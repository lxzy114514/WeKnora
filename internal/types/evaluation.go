package types

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/yanyiwu/gojieba"
)

// Jieba is a global instance of Chinese text segmentation tool
var Jieba *gojieba.Jieba = newJieba()

func newJieba() *gojieba.Jieba {
	dictDir := os.Getenv("JIEBA_DICT_DIR")
	if dictDir == "" {
		return gojieba.NewJieba()
	}

	return gojieba.NewJieba(
		filepath.Join(dictDir, "jieba.dict.utf8"),
		filepath.Join(dictDir, "hmm_model.utf8"),
		filepath.Join(dictDir, "user.dict.utf8"),
		filepath.Join(dictDir, "idf.utf8"),
		filepath.Join(dictDir, "stop_words.utf8"),
	)
}

// EvaluationStatue represents the status of an evaluation task
type EvaluationStatue int

const (
	EvaluationStatuePending EvaluationStatue = iota // Task is waiting to start
	EvaluationStatueRunning                         // Task is in progress
	EvaluationStatueSuccess                         // Task completed successfully
	EvaluationStatueFailed                          // Task failed
)

// EvaluationTask contains information about an evaluation task
type EvaluationTask struct {
	ID        string `json:"id"`         // Unique task ID
	TenantID  uint64 `json:"tenant_id"`  // Tenant/Organization ID
	DatasetID string `json:"dataset_id"` // Dataset ID for evaluation

	StartTime time.Time        `json:"start_time"`         // Task start time
	EndTime   *time.Time       `json:"end_time,omitempty"` // Task completion time
	Status    EvaluationStatue `json:"status"`             // Current task status
	ErrMsg    string           `json:"err_msg,omitempty"`  // Error message if failed

	Total    int `json:"total,omitempty"`    // Total items to evaluate
	Finished int `json:"finished,omitempty"` // Completed items count
}

// EvaluationDetail contains detailed evaluation information
type EvaluationDetail struct {
	Task     *EvaluationTask           `json:"task"`               // Evaluation task info
	Params   *ChatManage               `json:"params,omitempty"`   // Evaluation parameters
	Config   *EvaluationConfigSnapshot `json:"config,omitempty"`   // Immutable inputs
	Metric   *MetricResult             `json:"metric,omitempty"`   // Evaluation metrics
	Cost     *EvaluationCostReport     `json:"cost,omitempty"`     // Model cost report
	Duration *EvaluationDurationReport `json:"duration,omitempty"` // Latency report
	Cases    []*EvaluationCaseResult   `json:"cases,omitempty"`    // Per-question evidence
}

// EvaluationConfigSnapshot freezes inputs that could otherwise drift between
// runs. Model prices are captured by individual model-call records.
type EvaluationConfigSnapshot struct {
	ConfigHash       string                             `json:"config_hash"`
	DatasetID        string                             `json:"dataset_id"`
	DatasetVersion   string                             `json:"dataset_version"`
	DatasetHash      string                             `json:"dataset_hash"`
	DatasetFiles     map[string]DatasetManifestFile     `json:"dataset_files,omitempty"`
	ChatModelID      string                             `json:"chat_model_id,omitempty"`
	EmbeddingModelID string                             `json:"embedding_model_id,omitempty"`
	RerankModelID    string                             `json:"rerank_model_id,omitempty"`
	SummaryModelID   string                             `json:"summary_model_id,omitempty"`
	Models           map[string]EvaluationModelSnapshot `json:"models,omitempty"`
	KnowledgeBaseID  string                             `json:"knowledge_base_id,omitempty"`
	ChunkSize        int                                `json:"chunk_size"`
	ChunkOverlap     int                                `json:"chunk_overlap"`
	PipelineHash     string                             `json:"pipeline_hash"`
	CodeVersion      string                             `json:"code_version,omitempty"`
}

type EvaluationModelSnapshot struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Type          ModelType    `json:"type"`
	Source        ModelSource  `json:"source"`
	Provider      string       `json:"provider,omitempty"`
	InterfaceType string       `json:"interface_type,omitempty"`
	Dimensions    int          `json:"dimensions,omitempty"`
	ParameterHash string       `json:"parameter_hash"`
	Pricing       ModelPricing `json:"pricing"`
}

// Fingerprint returns a deterministic digest of the effective evaluation
// inputs. ConfigHash and CodeVersion are excluded: the latter identifies the
// implementation being measured and must be allowed to change across a
// regression comparison; dataset/model/chunk conditions must not.
func (c EvaluationConfigSnapshot) Fingerprint() (string, error) {
	c.ConfigHash = ""
	c.CodeVersion = ""
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type EvaluationCostReport struct {
	Currency  string  `json:"currency"`
	Total     float64 `json:"total"`
	Chat      float64 `json:"chat"`
	Embedding float64 `json:"embedding"`
	Rerank    float64 `json:"rerank"`
	CallCount int64   `json:"call_count"`
	Estimated bool    `json:"estimated"`
}

type EvaluationDurationReport struct {
	TotalMS       int64   `json:"total_ms"`
	AverageCaseMS float64 `json:"average_case_ms"`
	ModelCallMS   int64   `json:"model_call_ms"`
	IndexMS       int64   `json:"index_ms"`
	RetrievalMS   int64   `json:"retrieval_ms"`
	RerankMS      int64   `json:"rerank_ms"`
	GenerationMS  int64   `json:"generation_ms"`
}

type EvaluationStageDuration struct {
	RetrievalMS  int64 `json:"retrieval_ms"`
	RerankMS     int64 `json:"rerank_ms"`
	GenerationMS int64 `json:"generation_ms"`
}

type EvaluationCaseResult struct {
	TaskID        string                   `json:"task_id"`
	TenantID      uint64                   `json:"tenant_id"`
	QID           int                      `json:"qid"`
	DurationMS    int64                    `json:"duration_ms"`
	Stages        *EvaluationStageDuration `json:"stages,omitempty"`
	RetrievedPIDs []int                    `json:"retrieved_pids,omitempty"`
	RerankedPIDs  []int                    `json:"reranked_pids,omitempty"`
	FinalAnswer   string                   `json:"final_answer,omitempty"`
	Metric        *MetricResult            `json:"metric,omitempty"`
	Error         string                   `json:"error,omitempty"`
}

// String returns JSON representation of EvaluationTask
func (e *EvaluationTask) String() string {
	b, _ := json.Marshal(e)
	return string(b)
}

// MetricInput contains input data for metric calculation
type MetricInput struct {
	RetrievalGT  [][]int // Ground truth for retrieval
	RetrievalIDs []int   // Retrieved IDs

	GeneratedTexts string // Generated text for evaluation
	GeneratedGT    string // Ground truth text for comparison
}

// MetricResult contains evaluation metrics
type MetricResult struct {
	RetrievalMetrics  RetrievalMetrics  `json:"retrieval_metrics"`  // Retrieval performance metrics
	GenerationMetrics GenerationMetrics `json:"generation_metrics"` // Text generation quality metrics
}

// RetrievalMetrics contains metrics for retrieval evaluation
type RetrievalMetrics struct {
	Precision float64 `json:"precision"` // Precision score
	Recall    float64 `json:"recall"`    // Recall score

	NDCG3  float64 `json:"ndcg3"`  // Normalized Discounted Cumulative Gain at 3
	NDCG10 float64 `json:"ndcg10"` // Normalized Discounted Cumulative Gain at 10
	MRR    float64 `json:"mrr"`    // Mean Reciprocal Rank
	MAP    float64 `json:"map"`    // Mean Average Precision
}

// GenerationMetrics contains metrics for text generation evaluation
type GenerationMetrics struct {
	BLEU1 float64 `json:"bleu1"` // BLEU-1 score
	BLEU2 float64 `json:"bleu2"` // BLEU-2 score
	BLEU4 float64 `json:"bleu4"` // BLEU-4 score

	ROUGE1 float64 `json:"rouge1"` // ROUGE-1 score
	ROUGE2 float64 `json:"rouge2"` // ROUGE-2 score
	ROUGEL float64 `json:"rougel"` // ROUGE-L score
}

// EvalState represents different stages of evaluation process
type EvalState int

const (
	StateBegin             EvalState = iota // Evaluation started
	StateAfterQaPairs                       // After loading QA pairs
	StateAfterDataset                       // After processing dataset
	StateAfterEmbedding                     // After generating embeddings
	StateAfterVectorSearch                  // After vector search
	StateAfterRerank                        // After reranking
	StateAfterComplete                      // After completion
	StateEnd                                // Evaluation ended
)
