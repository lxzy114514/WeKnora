package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestModelCallRepositoryFiltersAndAggregatesDatabaseRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:model-calls?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&modelCallRow{}))
	repo := NewModelCallRepository(db)
	ctx := context.Background()
	now := time.Now().UTC()

	require.NoError(t, repo.Record(ctx, &types.ModelCallRecord{
		ID: "call-1", TenantID: 7, ModelID: "chat-a", ModelType: types.ModelTypeKnowledgeQA,
		Purpose: "knowledge_qa", CorrelationID: "eval-1", PromptTokens: 1000,
		CompletionTokens: 200, CacheReadTokens: 600, CacheMissTokens: 400,
		DurationMS: 80, Success: true, CreatedAt: now.Add(-time.Minute),
		Pricing: types.ModelPricing{Currency: "USD", InputPerMillion: 1, OutputPerMillion: 2, CacheReadPerMillion: 0.1},
	}))
	require.NoError(t, repo.Record(ctx, &types.ModelCallRecord{
		ID: "call-2", TenantID: 7, ModelID: "embed-a", ModelType: types.ModelTypeEmbedding,
		Purpose: "knowledge_index", CorrelationID: "eval-1", InputUnits: 500,
		CacheHitItems: 2, CacheMissItems: 1, DurationMS: 20, Success: true, CreatedAt: now,
		Pricing: types.ModelPricing{Currency: "USD", EmbeddingPerMillion: 0.5},
	}))
	require.NoError(t, repo.Record(ctx, &types.ModelCallRecord{
		ID: "other-tenant", TenantID: 8, ModelID: "chat-a", ModelType: types.ModelTypeKnowledgeQA,
		Purpose: "knowledge_qa", PromptTokens: 999999, Success: true, CreatedAt: now,
	}))

	summary, err := repo.Summary(ctx, types.ModelUsageQuery{
		TenantID: 7, StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), summary.CallCount)
	assert.Equal(t, int64(600), summary.CacheReadTokens)
	assert.Equal(t, int64(2), summary.CacheHitItems)
	assert.Len(t, summary.ByModel, 2)
	assert.InDelta(t, 0.00111, summary.TotalCost, 0.0000001)

	chatOnly, err := repo.Summary(ctx, types.ModelUsageQuery{TenantID: 7, ModelID: "chat-a"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), chatOnly.CallCount)
	assert.Equal(t, "chat-a", chatOnly.ByModel[0].ModelID)

	linked, err := repo.ListByCorrelation(ctx, 7, "eval-1")
	require.NoError(t, err)
	assert.Len(t, linked, 2)
}

func TestEstimateModelCallCostUsesCacheAndTypeSpecificPrices(t *testing.T) {
	chat := &types.ModelCallRecord{
		ModelType: types.ModelTypeKnowledgeQA, PromptTokens: 1000, CompletionTokens: 100,
		CacheReadTokens: 800, CacheMissTokens: 200,
		Pricing: types.ModelPricing{InputPerMillion: 2, OutputPerMillion: 4, CacheReadPerMillion: 0.2},
	}
	embed := &types.ModelCallRecord{
		ModelType: types.ModelTypeEmbedding, InputUnits: 1000,
		Pricing: types.ModelPricing{EmbeddingPerMillion: 0.5},
	}
	rerank := &types.ModelCallRecord{
		ModelType: types.ModelTypeRerank, InputUnits: 25,
		Pricing: types.ModelPricing{RerankPerThousand: 1.2},
	}

	assert.InDelta(t, 0.00096, types.EstimateModelCallCost(chat), 0.0000001)
	assert.InDelta(t, 0.0005, types.EstimateModelCallCost(embed), 0.0000001)
	assert.InDelta(t, 0.03, types.EstimateModelCallCost(rerank), 0.0000001)
}
