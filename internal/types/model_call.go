package types

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const modelCallPersistenceTimeout = 5 * time.Second

// ModelCallPersistenceContext preserves request metadata but outlives request
// cancellation long enough to persist the failure that caused it. The short
// deadline prevents observability from leaking goroutines or hanging callers.
func ModelCallPersistenceContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), modelCallPersistenceTimeout)
}

type EmbeddingCacheKey struct {
	TenantID         uint64
	ModelID          string
	Dimensions       int
	ModelFingerprint string
	ContentHash      string
}

func HashEmbeddingText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// EmbeddingModelFingerprint identifies effective provider configuration while
// excluding first-class credentials. It prevents a process started from YAML
// with a reused model ID from reading vectors produced by older settings.
func EmbeddingModelFingerprint(model *Model) string {
	if model == nil {
		return ""
	}
	parameters := model.Parameters
	parameters.APIKey = ""
	parameters.AppSecret = ""
	parameters.Pricing = ModelPricing{}
	data, _ := json.Marshal(struct {
		Name       string          `json:"name"`
		Source     ModelSource     `json:"source"`
		Parameters ModelParameters `json:"parameters"`
	}{model.Name, model.Source, parameters})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ModelCallRecord is the privacy-safe fact emitted once for each logical model
// call. It stores counts and opaque hashes, never prompt or response content.
type ModelCallRecord struct {
	ID                      string       `json:"id"`
	TenantID                uint64       `json:"tenant_id"`
	ModelID                 string       `json:"model_id"`
	ModelType               ModelType    `json:"model_type"`
	Purpose                 string       `json:"purpose,omitempty"`
	CorrelationID           string       `json:"correlation_id,omitempty"`
	PromptPrefixFingerprint string       `json:"prompt_prefix_fingerprint,omitempty"`
	PromptTokens            int          `json:"prompt_tokens"`
	CompletionTokens        int          `json:"completion_tokens"`
	TotalTokens             int          `json:"total_tokens"`
	CacheReadTokens         int          `json:"cache_read_tokens"`
	CacheWriteTokens        int          `json:"cache_write_tokens"`
	CacheMissTokens         int          `json:"cache_miss_tokens"`
	InputUnits              int          `json:"input_units"`
	ItemCount               int          `json:"item_count"`
	CacheHitItems           int          `json:"cache_hit_items"`
	CacheMissItems          int          `json:"cache_miss_items"`
	DurationMS              int64        `json:"duration_ms"`
	Success                 bool         `json:"success"`
	ErrorCode               string       `json:"error_code,omitempty"`
	Pricing                 ModelPricing `json:"pricing"`
	Cost                    float64      `json:"cost"`
	CostEstimated           bool         `json:"cost_estimated"`
	CreatedAt               time.Time    `json:"created_at"`
}

type ModelUsageQuery struct {
	TenantID  uint64
	ModelID   string
	Purpose   string
	StartTime time.Time
	EndTime   time.Time
}

type ModelUsageBreakdown struct {
	ModelID          string    `json:"model_id"`
	ModelType        ModelType `json:"model_type"`
	CallCount        int64     `json:"call_count"`
	SuccessCount     int64     `json:"success_count"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	CacheMissTokens  int64     `json:"cache_miss_tokens"`
	InputUnits       int64     `json:"input_units"`
	CacheHitItems    int64     `json:"cache_hit_items"`
	CacheMissItems   int64     `json:"cache_miss_items"`
	DurationMS       int64     `json:"duration_ms"`
	TotalCost        float64   `json:"total_cost"`
}

type ModelUsageSummary struct {
	Currency         string                `json:"currency"`
	CallCount        int64                 `json:"call_count"`
	SuccessCount     int64                 `json:"success_count"`
	PromptTokens     int64                 `json:"prompt_tokens"`
	CompletionTokens int64                 `json:"completion_tokens"`
	CacheReadTokens  int64                 `json:"cache_read_tokens"`
	CacheWriteTokens int64                 `json:"cache_write_tokens"`
	CacheMissTokens  int64                 `json:"cache_miss_tokens"`
	InputUnits       int64                 `json:"input_units"`
	CacheHitItems    int64                 `json:"cache_hit_items"`
	CacheMissItems   int64                 `json:"cache_miss_items"`
	DurationMS       int64                 `json:"duration_ms"`
	TotalCost        float64               `json:"total_cost"`
	ByModel          []ModelUsageBreakdown `json:"by_model"`
}

func EstimateModelCallCost(call *ModelCallRecord) float64 {
	if call == nil {
		return 0
	}
	total := 0.0
	for _, component := range modelCallCostComponents(call) {
		total += component.units * component.rate
	}
	return total
}

// ModelCallCostHasPriceCoverage reports whether every billable component used
// by the call has a configured positive rate. A fully cached Embedding call is
// a known zero-cost provider call and needs no rate. The pricing schema cannot
// distinguish an intentional zero rate from an omitted one, so all other used
// zero rates are treated conservatively as unknown.
func ModelCallCostHasPriceCoverage(call *ModelCallRecord) bool {
	if call == nil {
		return false
	}
	if call.ModelType == ModelTypeEmbedding && call.ItemCount > 0 &&
		call.CacheHitItems >= call.ItemCount && call.CacheMissItems == 0 && call.InputUnits == 0 {
		return true
	}
	components := modelCallCostComponents(call)
	hasBillableUsage := false
	for _, component := range components {
		if component.units <= 0 {
			continue
		}
		hasBillableUsage = true
		if component.rate <= 0 {
			return false
		}
	}
	return hasBillableUsage
}

type modelCallCostComponent struct {
	units float64
	rate  float64
}

func modelCallCostComponents(call *ModelCallRecord) []modelCallCostComponent {
	switch call.ModelType {
	case ModelTypeEmbedding:
		return []modelCallCostComponent{{float64(call.InputUnits) / 1_000_000, call.Pricing.EmbeddingPerMillion}}
	case ModelTypeRerank:
		return []modelCallCostComponent{{float64(call.InputUnits) / 1_000, call.Pricing.RerankPerThousand}}
	default:
		missTokens := call.CacheMissTokens
		if missTokens == 0 {
			missTokens = max(0, call.PromptTokens-call.CacheReadTokens-call.CacheWriteTokens)
		}
		return []modelCallCostComponent{
			{float64(missTokens) / 1_000_000, call.Pricing.InputPerMillion},
			{float64(call.CompletionTokens) / 1_000_000, call.Pricing.OutputPerMillion},
			{float64(call.CacheReadTokens) / 1_000_000, call.Pricing.CacheReadPerMillion},
			{float64(call.CacheWriteTokens) / 1_000_000, call.Pricing.CacheWritePerMillion},
		}
	}
}
