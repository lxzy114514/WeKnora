package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/utils"
	"golang.org/x/sync/errgroup"
)

const evaluationMaxWorkers = 4

// EvaluationCommitID is injected by production builds. Container build
// contexts intentionally omit .git, so runtime/debug cannot always recover a
// VCS revision from the compiled binary on its own.
var EvaluationCommitID = "unknown"

/*
corpus: pid -> content
queries: qid -> content
answers: aid -> content
qrels: qid -> pid
arels: qid -> aid
*/

// EvaluationService handles evaluation tasks for knowledge base and chat models
type EvaluationService struct {
	config               *config.Config                  // Application configuration
	dataset              interfaces.DatasetService       // Service for dataset operations
	knowledgeBaseService interfaces.KnowledgeBaseService // Service for knowledge base operations
	knowledgeService     interfaces.KnowledgeService     // Service for knowledge operations
	sessionService       interfaces.SessionService       // Service for chat sessions
	modelService         interfaces.ModelService         // Service for model operations
	evaluationRepo       interfaces.EvaluationRepository
	modelCallRepo        interfaces.ModelCallRepository

	evaluationMemoryStorage *evaluationMemoryStorage // In-memory storage for evaluation tasks
}

func NewEvaluationService(
	config *config.Config,
	dataset interfaces.DatasetService,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	sessionService interfaces.SessionService,
	modelService interfaces.ModelService,
	evaluationRepo interfaces.EvaluationRepository,
	modelCallRepo interfaces.ModelCallRepository,
) interfaces.EvaluationService {
	if evaluationRepo != nil {
		if err := evaluationRepo.RecoverInterrupted(
			context.Background(), "service restarted before evaluation completed",
		); err != nil {
			logger.Warnf(context.Background(), "recover interrupted evaluations failed: %v", err)
		}
	}
	evaluationMemoryStorage := newEvaluationMemoryStorage()
	return &EvaluationService{
		config:                  config,
		dataset:                 dataset,
		knowledgeBaseService:    knowledgeBaseService,
		knowledgeService:        knowledgeService,
		sessionService:          sessionService,
		modelService:            modelService,
		evaluationRepo:          evaluationRepo,
		modelCallRepo:           modelCallRepo,
		evaluationMemoryStorage: evaluationMemoryStorage,
	}
}

// evaluationMemoryStorage stores evaluation tasks in memory with thread-safe access
type evaluationMemoryStorage struct {
	store map[string]*types.EvaluationDetail // Map of taskID to evaluation details
	mu    *sync.RWMutex                      // Read-write lock for concurrent access
}

func newEvaluationMemoryStorage() *evaluationMemoryStorage {
	res := &evaluationMemoryStorage{
		store: make(map[string]*types.EvaluationDetail),
		mu:    &sync.RWMutex{},
	}
	return res
}

func (e *evaluationMemoryStorage) register(params *types.EvaluationDetail) {
	e.mu.Lock()
	defer e.mu.Unlock()
	logger.Infof(context.Background(), "Registering evaluation task: %s", params.Task.ID)
	e.store[params.Task.ID] = params
}

func (e *evaluationMemoryStorage) get(taskID string) (*types.EvaluationDetail, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	logger.Infof(context.Background(), "Getting evaluation task: %s", taskID)
	res, ok := e.store[taskID]
	if !ok {
		return nil, errors.New("task not found")
	}
	return res, nil
}

func (e *evaluationMemoryStorage) update(taskID string, fn func(params *types.EvaluationDetail)) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	params, ok := e.store[taskID]
	if !ok {
		return errors.New("task not found")
	}
	fn(params)
	return nil
}

func (e *evaluationMemoryStorage) updateSnapshot(
	taskID string, fn func(params *types.EvaluationDetail),
) (*types.EvaluationDetail, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	params, ok := e.store[taskID]
	if !ok {
		return nil, errors.New("task not found")
	}
	fn(params)
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var snapshot types.EvaluationDetail
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (e *EvaluationService) EvaluationResult(ctx context.Context, taskID string) (*types.EvaluationDetail, error) {
	logger.Info(ctx, "Start getting evaluation result")
	logger.Infof(ctx, "Task ID: %s", taskID)

	tenantID := types.MustTenantIDFromContext(ctx)
	detail, err := e.evaluationRepo.Get(ctx, tenantID, taskID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get evaluation task: %v", err)
		return nil, err
	}

	if detail == nil {
		return nil, errors.New("task not found")
	}
	logger.Info(ctx, "Evaluation result retrieved successfully")
	return detail, nil
}

func (e *EvaluationService) ListEvaluations(
	ctx context.Context, limit, offset int,
) ([]*types.EvaluationDetail, error) {
	return e.evaluationRepo.List(ctx, types.MustTenantIDFromContext(ctx), limit, offset)
}

func (e *EvaluationService) ListEvaluationCases(
	ctx context.Context, taskID string, limit, offset int,
) ([]*types.EvaluationCaseResult, error) {
	return e.evaluationRepo.ListCases(ctx, types.MustTenantIDFromContext(ctx), taskID, limit, offset)
}

func (e *EvaluationService) persistUpdate(
	ctx context.Context, taskID string, fn func(*types.EvaluationDetail),
) error {
	snapshot, err := e.evaluationMemoryStorage.updateSnapshot(taskID, fn)
	if err != nil {
		return err
	}
	return e.evaluationRepo.Update(ctx, snapshot)
}

// Evaluation starts a new evaluation task with given parameters
// datasetID: ID of the dataset to evaluate against
// knowledgeBaseID: ID of the knowledge base to use (empty to create new)
// chatModelID: ID of the chat model to evaluate
// rerankModelID: ID of the rerank model to evaluate
func (e *EvaluationService) Evaluation(ctx context.Context,
	datasetID string, knowledgeBaseID string, chatModelID string, embeddingModelID string,
	rerankModelID string, summaryModelID string,
) (*types.EvaluationDetail, error) {
	logger.Info(ctx, "Start evaluation")
	logger.Infof(ctx, "Dataset ID: %s, Knowledge Base ID: %s, Chat Model ID: %s, Rerank Model ID: %s",
		datasetID, knowledgeBaseID, chatModelID, rerankModelID)

	// Get tenant ID from context for multi-tenancy support
	tenantID := types.MustTenantIDFromContext(ctx)
	sourceKnowledgeBaseID := knowledgeBaseID
	logger.Infof(ctx, "Tenant ID: %d", tenantID)
	if datasetID == "" || datasetID == "default" {
		datasetID = "baseline-v1"
	}
	datasetSnapshot, err := e.dataset.GetDatasetSnapshot(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	datasetID = datasetSnapshot.ID
	temporaryKnowledgeBaseID := ""
	handedToBackground := false
	defer func() {
		if temporaryKnowledgeBaseID != "" && !handedToBackground {
			if cleanupErr := e.knowledgeBaseService.DeleteKnowledgeBase(ctx, temporaryKnowledgeBaseID); cleanupErr != nil {
				logger.Warnf(ctx, "clean up evaluation knowledge base after setup failure: %v", cleanupErr)
			}
		}
	}()

	// Handle knowledge base creation if not provided
	if knowledgeBaseID == "" {
		logger.Info(ctx, "No knowledge base ID provided, creating new knowledge base")
		// Create new knowledge base with default evaluation settings
		// 获取默认的嵌入模型和LLM模型
		models, err := e.modelService.ListModels(ctx)
		if err != nil {
			logger.Errorf(ctx, "Failed to list models: %v", err)
			return nil, err
		}

		llmModelID := summaryModelID
		if llmModelID == "" {
			llmModelID = chatModelID
		}
		for _, model := range models {
			if model == nil {
				continue
			}
			if embeddingModelID == "" && model.Type == types.ModelTypeEmbedding {
				embeddingModelID = model.ID
			}
			if llmModelID == "" && model.Type == types.ModelTypeKnowledgeQA {
				llmModelID = model.ID
			}
		}

		if embeddingModelID == "" || llmModelID == "" {
			return nil, fmt.Errorf("no default models found for evaluation")
		}
		summaryModelID = llmModelID

		kb, err := e.knowledgeBaseService.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
			Name:             "evaluation",
			Description:      "evaluation",
			EmbeddingModelID: embeddingModelID,
			SummaryModelID:   llmModelID,
		})
		if err != nil {
			logger.Errorf(ctx, "Failed to create knowledge base: %v", err)
			return nil, err
		}
		knowledgeBaseID = kb.ID
		temporaryKnowledgeBaseID = kb.ID
		logger.Infof(ctx, "Created new knowledge base with ID: %s", knowledgeBaseID)
	} else {
		logger.Infof(ctx, "Using existing knowledge base ID: %s", knowledgeBaseID)
		// Create evaluation-specific knowledge base based on existing one
		kb, err := e.knowledgeBaseService.GetKnowledgeBaseByID(ctx, knowledgeBaseID)
		if err != nil {
			logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
			return nil, err
		}

		if embeddingModelID == "" {
			embeddingModelID = kb.EmbeddingModelID
		}
		if summaryModelID == "" {
			summaryModelID = kb.SummaryModelID
		}
		if summaryModelID == "" {
			summaryModelID = chatModelID
		}
		kb, err = e.knowledgeBaseService.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
			Name:             "evaluation",
			Description:      "evaluation",
			EmbeddingModelID: embeddingModelID,
			SummaryModelID:   summaryModelID,
		})
		if err != nil {
			logger.Errorf(ctx, "Failed to create knowledge base: %v", err)
			return nil, err
		}
		knowledgeBaseID = kb.ID
		temporaryKnowledgeBaseID = kb.ID
		logger.Infof(ctx, "Created new knowledge base with ID: %s based on existing one", knowledgeBaseID)
	}

	if rerankModelID == "" {
		// 获取默认的重排模型
		models, err := e.modelService.ListModels(ctx)
		if err == nil {
			for _, model := range models {
				if model == nil {
					continue
				}
				if model.Type == types.ModelTypeRerank {
					rerankModelID = model.ID
					break
				}
			}
		}
		if rerankModelID == "" {
			logger.Warnf(ctx, "No rerank model found, skipping rerank")
		} else {
			logger.Infof(ctx, "Using default rerank model: %s", rerankModelID)
		}
	}

	if chatModelID == "" {
		// 获取默认的LLM模型
		models, err := e.modelService.ListModels(ctx)
		if err == nil {
			for _, model := range models {
				if model == nil {
					continue
				}
				if model.Type == types.ModelTypeKnowledgeQA {
					chatModelID = model.ID
					break
				}
			}
		}
		if chatModelID == "" {
			return nil, fmt.Errorf("no default chat model found")
		}
		logger.Infof(ctx, "Using default chat model: %s", chatModelID)
	}

	// Create evaluation task with unique ID
	logger.Info(ctx, "Creating evaluation task")
	taskID := utils.GenerateTaskID("evaluation", tenantID, datasetID)
	logger.Infof(ctx, "Generated task ID: %s", taskID)

	chunkSize, chunkOverlap := 0, 0
	if e.config.KnowledgeBase != nil {
		chunkSize = e.config.KnowledgeBase.ChunkSize
		chunkOverlap = e.config.KnowledgeBase.ChunkOverlap
	}
	pipelineRequest := types.PipelineRequest{
		VectorThreshold:  e.config.Conversation.VectorThreshold,
		KeywordThreshold: e.config.Conversation.KeywordThreshold,
		EmbeddingTopK:    e.config.Conversation.EmbeddingTopK,
		MaxRounds:        e.config.Conversation.MaxRounds,
		RerankModelID:    rerankModelID,
		RerankTopK:       e.config.Conversation.RerankTopK,
		RerankThreshold:  e.config.Conversation.RerankThreshold,
		ChatModelID:      chatModelID,
		SummaryConfig: types.SummaryConfig{
			MaxTokens:           e.config.Conversation.Summary.MaxTokens,
			RepeatPenalty:       e.config.Conversation.Summary.RepeatPenalty,
			TopK:                e.config.Conversation.Summary.TopK,
			TopP:                e.config.Conversation.Summary.TopP,
			Prompt:              e.config.Conversation.Summary.Prompt,
			ContextTemplate:     e.config.Conversation.Summary.ContextTemplate,
			FrequencyPenalty:    e.config.Conversation.Summary.FrequencyPenalty,
			PresencePenalty:     e.config.Conversation.Summary.PresencePenalty,
			NoMatchPrefix:       e.config.Conversation.Summary.NoMatchPrefix,
			Temperature:         e.config.Conversation.Summary.Temperature,
			Seed:                e.config.Conversation.Summary.Seed,
			MaxCompletionTokens: e.config.Conversation.Summary.MaxCompletionTokens,
		},
		FallbackResponse:    e.config.Conversation.FallbackResponse,
		RewritePromptSystem: e.config.Conversation.RewritePromptSystem,
		RewritePromptUser:   e.config.Conversation.RewritePromptUser,
	}
	pipelineData, err := json.Marshal(pipelineRequest)
	if err != nil {
		return nil, fmt.Errorf("fingerprint evaluation pipeline: %w", err)
	}
	pipelineDigest := sha256.Sum256(pipelineData)
	modelSnapshots, err := e.snapshotEvaluationModels(ctx, map[string]string{
		"chat": chatModelID, "embedding": embeddingModelID, "rerank": rerankModelID,
		"summary": summaryModelID,
	})
	if err != nil {
		return nil, err
	}
	configSnapshot := &types.EvaluationConfigSnapshot{
		DatasetID: datasetSnapshot.ID, DatasetVersion: datasetSnapshot.Version,
		DatasetHash: datasetSnapshot.Hash, DatasetFiles: datasetSnapshot.Files,
		ChatModelID: chatModelID, EmbeddingModelID: embeddingModelID,
		RerankModelID: rerankModelID, SummaryModelID: summaryModelID,
		Models: modelSnapshots, KnowledgeBaseID: sourceKnowledgeBaseID,
		ChunkSize: chunkSize, ChunkOverlap: chunkOverlap,
		PipelineHash: fmt.Sprintf("%x", pipelineDigest[:]),
		CodeVersion:  evaluationCodeVersion(),
	}
	configHash, err := configSnapshot.Fingerprint()
	if err != nil {
		return nil, fmt.Errorf("fingerprint evaluation configuration: %w", err)
	}
	configSnapshot.ConfigHash = configHash

	// Prepare evaluation detail with all parameters
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID:        taskID,
			TenantID:  tenantID,
			DatasetID: datasetID,
			Status:    types.EvaluationStatuePending,
			StartTime: time.Now(),
		},
		Params: &types.ChatManage{PipelineRequest: pipelineRequest},
		Config: configSnapshot,
	}

	// Store evaluation task in memory storage
	logger.Info(ctx, "Registering evaluation task")
	e.evaluationMemoryStorage.register(detail)
	if err := e.evaluationRepo.Create(ctx, detail); err != nil {
		return nil, fmt.Errorf("persist evaluation run: %w", err)
	}

	// Start evaluation in background goroutine
	logger.Info(ctx, "Starting evaluation in background")
	handedToBackground = true
	go func() {
		// Create new context with logger for background task
		newCtx := logger.CloneContext(ctx)
		defer func() {
			logger.Infof(newCtx, "Cleaning up evaluation knowledge base: %s", knowledgeBaseID)
			if cleanupErr := e.knowledgeBaseService.DeleteKnowledgeBase(newCtx, knowledgeBaseID); cleanupErr != nil {
				logger.Errorf(newCtx, "Failed to delete evaluation knowledge base %s: %v", knowledgeBaseID, cleanupErr)
			}
		}()
		logger.Infof(newCtx, "Background evaluation started for task ID: %s", taskID)

		// Update task status to running
		if err := e.persistUpdate(newCtx, taskID, func(current *types.EvaluationDetail) {
			current.Task.Status = types.EvaluationStatueRunning
		}); err != nil {
			logger.Errorf(newCtx, "Failed to persist running evaluation: %v", err)
			return
		}
		logger.Info(newCtx, "Evaluation task status set to running")

		// Execute actual evaluation
		runCtx := types.WithBackgroundTask(types.WithModelCallCorrelation(newCtx, taskID))
		if err := e.EvalDataset(runCtx, detail, knowledgeBaseID); err != nil {
			now := time.Now()
			_ = e.persistUpdate(newCtx, taskID, func(current *types.EvaluationDetail) {
				current.Task.Status = types.EvaluationStatueFailed
				current.Task.ErrMsg = err.Error()
				current.Task.EndTime = &now
				e.populateEvaluationReports(newCtx, current)
			})
			logger.Errorf(newCtx, "Evaluation task failed: %v, task ID: %s", err, taskID)
			return
		}

		// Mark task as completed successfully
		logger.Infof(newCtx, "Evaluation task completed successfully, task ID: %s", taskID)
		now := time.Now()
		if err := e.persistUpdate(newCtx, taskID, func(current *types.EvaluationDetail) {
			current.Task.Status = types.EvaluationStatueSuccess
			current.Task.EndTime = &now
			e.populateEvaluationReports(newCtx, current)
		}); err != nil {
			logger.Errorf(newCtx, "Failed to persist completed evaluation: %v", err)
		}
	}()

	logger.Infof(ctx, "Evaluation task created successfully, task ID: %s", taskID)
	return detail, nil
}

func (e *EvaluationService) snapshotEvaluationModels(
	ctx context.Context, modelIDs map[string]string,
) (map[string]types.EvaluationModelSnapshot, error) {
	result := make(map[string]types.EvaluationModelSnapshot)
	for role, modelID := range modelIDs {
		if modelID == "" {
			continue
		}
		model, err := e.modelService.GetModelByID(ctx, modelID)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s model %s: %w", role, modelID, err)
		}
		snapshot, err := evaluationModelSnapshot(model)
		if err != nil {
			return nil, fmt.Errorf("snapshot %s model parameters: %w", role, err)
		}
		result[role] = snapshot
	}
	return result, nil
}

func evaluationModelSnapshot(model *types.Model) (types.EvaluationModelSnapshot, error) {
	if model == nil {
		return types.EvaluationModelSnapshot{}, errors.New("model is required")
	}
	parameters := model.Parameters
	parameters.APIKey = ""
	parameters.AppSecret = ""
	data, err := json.Marshal(parameters)
	if err != nil {
		return types.EvaluationModelSnapshot{}, err
	}
	hash := sha256.Sum256(data)
	return types.EvaluationModelSnapshot{
		ID: model.ID, Name: model.Name, Type: model.Type, Source: model.Source,
		Provider: parameters.Provider, InterfaceType: parameters.InterfaceType,
		Dimensions:    parameters.EmbeddingParameters.Dimension,
		ParameterHash: fmt.Sprintf("%x", hash[:]), Pricing: parameters.Pricing,
	}, nil
}

// EvalDataset performs the actual evaluation of a dataset
// Processes each QA pair in parallel and records metrics
func (e *EvaluationService) EvalDataset(ctx context.Context, detail *types.EvaluationDetail, knowledgeBaseID string) error {
	logger.Info(ctx, "Start evaluating dataset")
	logger.Infof(ctx, "Task ID: %s, Dataset ID: %s", detail.Task.ID, detail.Task.DatasetID)

	// Retrieve dataset from storage
	snapshot, err := e.dataset.GetDatasetSnapshot(ctx, detail.Task.DatasetID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get dataset: %v", err)
		return err
	}
	dataset := snapshot.QAPairs
	logger.Infof(ctx, "Dataset retrieved successfully with %d QA pairs", len(dataset))

	// Update total QA pairs count in task details
	if err := e.persistUpdate(ctx, detail.Task.ID, func(params *types.EvaluationDetail) {
		params.Task.Total = len(dataset)
		logger.Infof(ctx, "Updated task total to %d QA pairs", params.Task.Total)
	}); err != nil {
		return err
	}

	// Extract and organize passages from dataset
	passages := getPassageList(dataset)
	logger.Infof(ctx, "Creating knowledge from %d passages", len(passages))

	// Create knowledge base from passages (sync: wait for indexing to complete before querying)
	indexStarted := time.Now()
	knowledge, err := e.knowledgeService.CreateKnowledgeFromPassageSync(ctx, knowledgeBaseID, passages, "")
	if err != nil {
		logger.Errorf(ctx, "Failed to create knowledge from passages: %v", err)
		return err
	}
	logger.Infof(ctx, "Knowledge created and indexed successfully, ID: %s", knowledge.ID)

	// Setup cleanup of temporary resources
	defer func() {
		logger.Infof(ctx, "Cleaning up resources - deleting knowledge: %s", knowledge.ID)
		if err := e.knowledgeService.DeleteKnowledge(ctx, knowledge.ID); err != nil {
			logger.Errorf(ctx, "Failed to delete knowledge: %v, knowledge ID: %s", err, knowledge.ID)
		}

	}()
	if err := e.persistUpdate(ctx, detail.Task.ID, func(params *types.EvaluationDetail) {
		if params.Duration == nil {
			params.Duration = &types.EvaluationDurationReport{}
		}
		params.Duration.IndexMS = time.Since(indexStarted).Milliseconds()
	}); err != nil {
		return err
	}

	// Initialize parallel evaluation metrics
	var finished int
	var mu sync.Mutex
	var g errgroup.Group
	metricHook := NewHookMetric(len(dataset), passages)

	// Keep offline evaluation below a small process-level ceiling. The context
	// is also marked as background so each model's governor can apply its own
	// configured limit without competing with interactive traffic.
	workerCount := min(max(runtime.GOMAXPROCS(0)-1, 1), evaluationMaxWorkers)
	g.SetLimit(workerCount)
	logger.Infof(ctx, "Starting evaluation with %d parallel workers", workerCount)

	// Process each QA pair in parallel
	for i, qaPair := range dataset {
		qaPair := qaPair
		i := i
		g.Go(func() error {
			caseStarted := time.Now()
			logger.Infof(ctx, "Processing QA pair %d, question: %s", i, qaPair.Question)

			// Prepare chat management parameters for this QA pair
			chatManage := detail.Params.Clone()
			chatManage.Query = qaPair.Question
			chatManage.RewriteQuery = qaPair.Question
			// Set knowledge base ID and search targets for this evaluation
			chatManage.KnowledgeBaseIDs = []string{knowledgeBaseID}
			chatManage.SearchTargets = types.SearchTargets{
				&types.SearchTarget{
					Type:            types.SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: knowledgeBaseID,
				},
			}

			// Execute knowledge QA pipeline
			logger.Infof(ctx, "Running knowledge QA for question: %s", qaPair.Question)
			qaErr := e.sessionService.KnowledgeQAByEvent(ctx, chatManage, types.Pipline["rag"])
			stages := evaluationStageDurations(chatManage.StageDurations)
			retrievedPIDs := mapSearchResultsToPassageIDs(passages, chatManage.SearchResult)
			rerankedPIDs := mapSearchResultsToPassageIDs(passages, chatManage.RerankResult)
			finalAnswer := ""
			if chatManage.ChatResponse != nil {
				finalAnswer = chatManage.ChatResponse.Content
			}
			if qaErr != nil {
				_ = e.evaluationRepo.UpsertCase(ctx, &types.EvaluationCaseResult{
					TaskID: detail.Task.ID, TenantID: detail.Task.TenantID, QID: qaPair.QID,
					DurationMS: time.Since(caseStarted).Milliseconds(), Stages: stages,
					RetrievedPIDs: retrievedPIDs, RerankedPIDs: rerankedPIDs,
					FinalAnswer: finalAnswer, Error: qaErr.Error(),
				})
				logger.Errorf(ctx, "Failed to process question %d: %v", i, qaErr)
				return qaErr
			}

			// Record evaluation metrics
			logger.Infof(ctx, "Recording metrics for QA pair %d", i)
			metricHook.recordInit(i)
			metricHook.recordQaPair(i, qaPair)
			metricHook.recordSearchResult(i, chatManage.SearchResult)
			metricHook.recordRerankResult(i, chatManage.RerankResult)
			metricHook.recordChatResponse(i, chatManage.ChatResponse)
			caseMetric := metricHook.recordFinish(i)
			if persistErr := e.evaluationRepo.UpsertCase(ctx, &types.EvaluationCaseResult{
				TaskID: detail.Task.ID, TenantID: detail.Task.TenantID, QID: qaPair.QID,
				DurationMS: time.Since(caseStarted).Milliseconds(), Stages: stages,
				RetrievedPIDs: retrievedPIDs, RerankedPIDs: rerankedPIDs,
				FinalAnswer: finalAnswer, Metric: caseMetric,
			}); persistErr != nil {
				return persistErr
			}

			// Update progress metrics
			mu.Lock()
			finished += 1
			currentFinished := finished
			metricResult := metricHook.MetricResult()
			mu.Unlock()
			if persistErr := e.persistUpdate(ctx, detail.Task.ID, func(params *types.EvaluationDetail) {
				params.Metric = metricResult
				params.Task.Finished = currentFinished
				logger.Infof(ctx, "Updated task progress: %d/%d completed", currentFinished, params.Task.Total)
			}); persistErr != nil {
				return persistErr
			}
			return nil
		})
	}

	// Wait for all parallel evaluations to complete
	logger.Info(ctx, "Waiting for all evaluation tasks to complete")
	if err := g.Wait(); err != nil {
		logger.Errorf(ctx, "Evaluation error: %v", err)
		return err
	}

	// Final update of evaluation metrics
	if err := e.persistUpdate(ctx, detail.Task.ID, func(params *types.EvaluationDetail) {
		params.Metric = metricHook.MetricResult()
		params.Task.Finished = finished
	}); err != nil {
		return err
	}

	logger.Infof(ctx, "Dataset evaluation completed successfully, task ID: %s", detail.Task.ID)
	return nil
}

func evaluationCodeVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		return selectEvaluationCodeVersion(EvaluationCommitID, info.Settings, info.Main.Version)
	}
	return selectEvaluationCodeVersion(EvaluationCommitID, nil, "")
}

func selectEvaluationCodeVersion(
	injectedCommit string, settings []debug.BuildSetting, mainVersion string,
) string {
	if injectedCommit = strings.TrimSpace(injectedCommit); injectedCommit != "" && injectedCommit != "unknown" {
		return injectedCommit
	}
	return formatEvaluationCodeVersion(settings, mainVersion)
}

func formatEvaluationCodeVersion(settings []debug.BuildSetting, mainVersion string) string {
	revision, modified := "", false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" {
		if modified {
			return revision + "+dirty"
		}
		return revision
	}
	if mainVersion != "" && mainVersion != "(devel)" {
		return mainVersion
	}
	return "unknown"
}

func (e *EvaluationService) populateEvaluationReports(ctx context.Context, detail *types.EvaluationDetail) {
	end := time.Now()
	if detail.Task.EndTime != nil {
		end = *detail.Task.EndTime
	}
	duration := &types.EvaluationDurationReport{TotalMS: end.Sub(detail.Task.StartTime).Milliseconds()}
	if detail.Duration != nil {
		duration.IndexMS = detail.Duration.IndexMS
	}
	cost := &types.EvaluationCostReport{Currency: "USD", Estimated: true}
	if e.modelCallRepo != nil {
		calls, err := e.modelCallRepo.ListByCorrelation(ctx, detail.Task.TenantID, detail.Task.ID)
		if err != nil {
			logger.Warnf(ctx, "load evaluation model calls failed: %v", err)
		} else {
			for _, call := range calls {
				cost.CallCount++
				if types.ModelCallCostHasPriceCoverage(call) {
					cost.PricedCallCount++
				}
				cost.Total += call.Cost
				duration.ModelCallMS += call.DurationMS
				switch call.ModelType {
				case types.ModelTypeEmbedding:
					cost.Embedding += call.Cost
				case types.ModelTypeRerank:
					cost.Rerank += call.Cost
				default:
					cost.Chat += call.Cost
				}
			}
			if cost.CallCount > 0 {
				cost.CostCoverageRatio = float64(cost.PricedCallCount) / float64(cost.CallCount)
				cost.CostGateEligible = cost.PricedCallCount == cost.CallCount
			}
		}
	}
	if cases, err := e.evaluationRepo.ListCases(ctx, detail.Task.TenantID, detail.Task.ID, 0, 0); err == nil && len(cases) > 0 {
		var total int64
		for _, result := range cases {
			total += result.DurationMS
			if result.Stages != nil {
				duration.RetrievalMS += result.Stages.RetrievalMS
				duration.RerankMS += result.Stages.RerankMS
				duration.GenerationMS += result.Stages.GenerationMS
			}
		}
		duration.AverageCaseMS = float64(total) / float64(len(cases))
	}
	detail.Cost = cost
	detail.Duration = duration
}

func evaluationStageDurations(values map[types.EventType]int64) *types.EvaluationStageDuration {
	stages := &types.EvaluationStageDuration{}
	for event, duration := range values {
		switch event {
		case types.QUERY_UNDERSTAND, types.CHUNK_SEARCH, types.CHUNK_SEARCH_PARALLEL,
			types.ENTITY_SEARCH, types.CHUNK_MERGE, types.FILTER_TOP_K:
			stages.RetrievalMS += duration
		case types.CHUNK_RERANK:
			stages.RerankMS += duration
		case types.DATA_ANALYSIS, types.INTO_CHAT_MESSAGE,
			types.CHAT_COMPLETION, types.CHAT_COMPLETION_STREAM:
			stages.GenerationMS += duration
		}
	}
	return stages
}

// getPassageList extracts and organizes passages from QA pairs
// Returns a slice of passages indexed by their passage IDs
func getPassageList(dataset []*types.QAPair) []string {
	pIDMap := make(map[int]string)
	maxPID := 0
	for _, qaPair := range dataset {
		for i := 0; i < len(qaPair.PIDs); i++ {
			pIDMap[qaPair.PIDs[i]] = qaPair.Passages[i]
			maxPID = max(maxPID, qaPair.PIDs[i])
		}
	}
	passages := make([]string, maxPID+1)
	for i := 0; i <= maxPID; i++ {
		if _, ok := pIDMap[i]; ok {
			passages[i] = pIDMap[i]
		}
	}
	return passages
}
