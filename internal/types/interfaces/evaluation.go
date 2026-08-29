package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// EvaluationService defines operations for evaluation tasks
type EvaluationService interface {
	// Evaluation starts a new evaluation task
	Evaluation(ctx context.Context, datasetID string, knowledgeBaseID string,
		chatModelID string, embeddingModelID string, rerankModelID string, summaryModelID string,
	) (*types.EvaluationDetail, error)
	// EvaluationResult retrieves evaluation result by task ID
	EvaluationResult(ctx context.Context, taskID string) (*types.EvaluationDetail, error)
	// ListEvaluations lists persisted runs for the current tenant.
	ListEvaluations(ctx context.Context, limit, offset int) ([]*types.EvaluationDetail, error)
	// ListEvaluationCases pages through per-question evidence separately from status polling.
	ListEvaluationCases(ctx context.Context, taskID string, limit, offset int) ([]*types.EvaluationCaseResult, error)
}

// EvaluationRepository is the durable boundary for evaluation lifecycle data.
type EvaluationRepository interface {
	Create(ctx context.Context, detail *types.EvaluationDetail) error
	Update(ctx context.Context, detail *types.EvaluationDetail) error
	Get(ctx context.Context, tenantID uint64, taskID string) (*types.EvaluationDetail, error)
	List(ctx context.Context, tenantID uint64, limit, offset int) ([]*types.EvaluationDetail, error)
	UpsertCase(ctx context.Context, result *types.EvaluationCaseResult) error
	ListCases(ctx context.Context, tenantID uint64, taskID string, limit, offset int) ([]*types.EvaluationCaseResult, error)
	RecoverInterrupted(ctx context.Context, reason string) error
}

// Metrics defines interface for computing evaluation metrics
type Metrics interface {
	// Compute calculates metric score based on input data
	Compute(metricInput *types.MetricInput) float64
}

// EvalHook defines interface for evaluation process hooks
type EvalHook interface {
	// Handle processes evaluation state change
	Handle(ctx context.Context, state types.EvalState, index int, data interface{}) error
}

// DatasetService defines operations for dataset management
type DatasetService interface {
	// GetDatasetByID retrieves QA pairs from dataset by ID
	GetDatasetByID(ctx context.Context, datasetID string) ([]*types.QAPair, error)
	// GetDatasetSnapshot verifies the versioned manifest and returns its data.
	GetDatasetSnapshot(ctx context.Context, datasetID string) (*types.DatasetSnapshot, error)
}
