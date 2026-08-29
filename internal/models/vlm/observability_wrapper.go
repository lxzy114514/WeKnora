package vlm

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// CallRecorder is the narrow persistence seam needed by the observability
// decorator. Keeping it local avoids coupling model implementations to GORM.
type CallRecorder interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
}

type observedVLM struct {
	inner    VLM
	tenantID uint64
	recorder CallRecorder
	pricing  types.ModelPricing
}

// NewObservedVLM records one privacy-safe row for every logical VLM call.
// VLM providers currently do not expose token usage through the VLM interface,
// so token counts (and therefore cost) are explicitly marked as estimates.
func NewObservedVLM(inner VLM, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing) VLM {
	if inner == nil || recorder == nil {
		return inner
	}
	return &observedVLM{inner: inner, tenantID: tenantID, recorder: recorder, pricing: pricing}
}

func (w *observedVLM) Predict(ctx context.Context, images [][]byte, prompt string) (string, error) {
	started := time.Now()
	response, err := w.inner.Predict(ctx, images, prompt)
	purpose, prefix := types.LLMCallMetadataFromContext(ctx)
	if purpose == "" {
		purpose = "vlm"
	}
	promptTokens := estimateTokens(prompt)
	completionTokens := estimateTokens(response)
	record := &types.ModelCallRecord{
		TenantID: w.tenantID, ModelID: w.inner.GetModelID(), ModelType: types.ModelTypeVLLM,
		Purpose: purpose, CorrelationID: types.ModelCallCorrelationFromContext(ctx),
		PromptPrefixFingerprint: prefix, PromptTokens: promptTokens,
		CompletionTokens: completionTokens, TotalTokens: promptTokens + completionTokens,
		ItemCount: len(images), DurationMS: time.Since(started).Milliseconds(),
		Success: err == nil, Pricing: w.pricing, CostEstimated: true,
	}
	if err != nil {
		record.ErrorCode = fmt.Sprintf("%T", err)
	}
	recordCtx, cancel := types.ModelCallPersistenceContext(ctx)
	defer cancel()
	if recordErr := w.recorder.Record(recordCtx, record); recordErr != nil {
		logger.Warnf(ctx, "record VLM model call failed: %v", recordErr)
	}
	return response, err
}

func estimateTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func (w *observedVLM) GetModelName() string { return w.inner.GetModelName() }
func (w *observedVLM) GetModelID() string   { return w.inner.GetModelID() }
