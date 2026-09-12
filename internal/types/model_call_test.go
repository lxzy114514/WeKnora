package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCallPersistenceContextDetachesCancellationAndKeepsMetadata(t *testing.T) {
	ctx, cancelRequest := context.WithCancel(WithModelCallCorrelation(context.Background(), "run-1"))
	cancelRequest()
	if ctx.Err() == nil {
		t.Fatal("request context was not canceled")
	}

	recordCtx, cancelRecord := ModelCallPersistenceContext(ctx)
	defer cancelRecord()
	if recordCtx.Err() != nil {
		t.Fatalf("record context inherited cancellation: %v", recordCtx.Err())
	}
	if got := ModelCallCorrelationFromContext(recordCtx); got != "run-1" {
		t.Fatalf("correlation ID = %q, want run-1", got)
	}
}

func TestEmbeddingModelFingerprintTracksProviderConfigButNotSecretsOrPricing(t *testing.T) {
	model := &Model{
		Name: "embed-v1", Source: ModelSourceRemote,
		Parameters: ModelParameters{
			BaseURL: "https://provider-a.example/v1", APIKey: "secret-a", AppSecret: "app-secret-a",
			EmbeddingParameters: EmbeddingParameters{Dimension: 768},
			Pricing:             ModelPricing{Currency: "USD", EmbeddingPerMillion: 1},
		},
	}
	original := EmbeddingModelFingerprint(model)
	require.Len(t, original, 64)

	model.Parameters.APIKey = "rotated-secret"
	model.Parameters.AppSecret = "rotated-app-secret"
	model.Parameters.Pricing.EmbeddingPerMillion = 2
	assert.Equal(t, original, EmbeddingModelFingerprint(model))

	model.Parameters.BaseURL = "https://provider-b.example/v1"
	assert.NotEqual(t, original, EmbeddingModelFingerprint(model))
}

func TestEstimateModelCallCostSeparatesCacheReadWriteAndMissTokens(t *testing.T) {
	call := &ModelCallRecord{
		ModelType:    ModelTypeKnowledgeQA,
		PromptTokens: 1000, CompletionTokens: 100,
		CacheReadTokens: 600, CacheWriteTokens: 250, CacheMissTokens: 150,
		Pricing: ModelPricing{
			InputPerMillion: 2, OutputPerMillion: 10,
			CacheReadPerMillion: 0.2, CacheWritePerMillion: 2.5,
		},
	}
	want := float64(150)*2/1_000_000 + float64(100)*10/1_000_000 +
		float64(600)*0.2/1_000_000 + float64(250)*2.5/1_000_000
	assert.InDelta(t, want, EstimateModelCallCost(call), 1e-12)

	call.CacheMissTokens = 0
	assert.InDelta(t, want, EstimateModelCallCost(call), 1e-12,
		"fallback must not charge cache-write tokens again as uncached input")
}

func TestModelCallCostHasPriceCoverageUsesTheSharedCostComponents(t *testing.T) {
	chat := &ModelCallRecord{
		ModelType: ModelTypeKnowledgeQA, CacheMissTokens: 100, CompletionTokens: 20,
		Pricing: ModelPricing{InputPerMillion: 1, OutputPerMillion: 2},
	}
	require.True(t, ModelCallCostHasPriceCoverage(chat))
	chat.Pricing.OutputPerMillion = 0
	require.False(t, ModelCallCostHasPriceCoverage(chat))

	chat = &ModelCallRecord{
		ModelType: ModelTypeKnowledgeQA, PromptTokens: 100, CompletionTokens: 20,
		CacheReadTokens: 60, CacheWriteTokens: 10,
		Pricing: ModelPricing{
			InputPerMillion: 1, OutputPerMillion: 2,
			CacheReadPerMillion: 0.1, CacheWritePerMillion: 0.2,
		},
	}
	require.True(t, ModelCallCostHasPriceCoverage(chat), "legacy cache-miss fallback and both cache rates must be covered")
	chat.Pricing.CacheWritePerMillion = 0
	require.False(t, ModelCallCostHasPriceCoverage(chat))

	embedding := &ModelCallRecord{
		ModelType: ModelTypeEmbedding, InputUnits: 50,
		Pricing: ModelPricing{EmbeddingPerMillion: 0.5},
	}
	require.True(t, ModelCallCostHasPriceCoverage(embedding))
	embedding.InputUnits = 0
	embedding.ItemCount = 5
	embedding.CacheHitItems = 5
	embedding.CacheMissItems = 0
	require.True(t, ModelCallCostHasPriceCoverage(embedding), "a full application-cache hit is a known provider zero")

	rerank := &ModelCallRecord{
		ModelType: ModelTypeRerank, InputUnits: 8,
		Pricing: ModelPricing{RerankPerThousand: 0.7},
	}
	require.True(t, ModelCallCostHasPriceCoverage(rerank))
	rerank.Pricing.RerankPerThousand = 0
	require.False(t, ModelCallCostHasPriceCoverage(rerank))
}
