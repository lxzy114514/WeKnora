package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

type ModelCallRepository interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
	Summary(ctx context.Context, query types.ModelUsageQuery) (*types.ModelUsageSummary, error)
	ListByCorrelation(ctx context.Context, tenantID uint64, correlationID string) ([]*types.ModelCallRecord, error)
}

type EmbeddingCacheRepository interface {
	Get(ctx context.Context, key types.EmbeddingCacheKey) ([]float32, bool, error)
	Put(ctx context.Context, key types.EmbeddingCacheKey, vector []float32) error
	// InvalidateModel removes derived vectors after a model configuration
	// changes or the model is deleted. Model IDs are globally unique.
	InvalidateModel(ctx context.Context, modelID string) error
}
