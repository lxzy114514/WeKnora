package rerank

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

type CallRecorder interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
}

type observedReranker struct {
	inner    Reranker
	tenantID uint64
	recorder CallRecorder
	pricing  types.ModelPricing
}

func NewObservedReranker(
	inner Reranker, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing,
) Reranker {
	if inner == nil || recorder == nil {
		return inner
	}
	return &observedReranker{inner: inner, tenantID: tenantID, recorder: recorder, pricing: pricing}
}

func (w *observedReranker) Rerank(
	ctx context.Context, query string, documents []string,
) ([]RankResult, error) {
	started := time.Now()
	result, err := w.inner.Rerank(ctx, query, documents)
	purpose, prefix := types.LLMCallMetadataFromContext(ctx)
	if purpose == "" {
		purpose = "rerank"
	}
	record := &types.ModelCallRecord{
		TenantID: w.tenantID, ModelID: w.inner.GetModelID(), ModelType: types.ModelTypeRerank,
		Purpose: purpose, CorrelationID: types.ModelCallCorrelationFromContext(ctx),
		PromptPrefixFingerprint: prefix, InputUnits: len(documents), ItemCount: len(documents),
		DurationMS: time.Since(started).Milliseconds(), Success: err == nil, Pricing: w.pricing,
	}
	if err != nil {
		record.ErrorCode = fmt.Sprintf("%T", err)
	}
	recordCtx, cancel := types.ModelCallPersistenceContext(ctx)
	defer cancel()
	if recordErr := w.recorder.Record(recordCtx, record); recordErr != nil {
		logger.Warnf(ctx, "record rerank model call failed: %v", recordErr)
	}
	return result, err
}

func (w *observedReranker) GetModelName() string { return w.inner.GetModelName() }
func (w *observedReranker) GetModelID() string   { return w.inner.GetModelID() }
