package asr

import (
	"context"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// CallRecorder is the persistence seam used by the ASR observability decorator.
type CallRecorder interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
}

type observedASR struct {
	inner    ASR
	tenantID uint64
	recorder CallRecorder
	pricing  types.ModelPricing
}

// NewObservedASR records call count, audio bytes, latency and outcome without
// persisting the audio, file name, or transcript.
func NewObservedASR(inner ASR, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing) ASR {
	if inner == nil || recorder == nil {
		return inner
	}
	return &observedASR{inner: inner, tenantID: tenantID, recorder: recorder, pricing: pricing}
}

func (w *observedASR) Transcribe(
	ctx context.Context, audioBytes []byte, fileName string,
) (*TranscriptionResult, error) {
	started := time.Now()
	result, err := w.inner.Transcribe(ctx, audioBytes, fileName)
	purpose, prefix := types.LLMCallMetadataFromContext(ctx)
	if purpose == "" {
		purpose = "asr"
	}
	record := &types.ModelCallRecord{
		TenantID: w.tenantID, ModelID: w.inner.GetModelID(), ModelType: types.ModelTypeASR,
		Purpose: purpose, CorrelationID: types.ModelCallCorrelationFromContext(ctx),
		PromptPrefixFingerprint: prefix, InputUnits: len(audioBytes), ItemCount: 1,
		DurationMS: time.Since(started).Milliseconds(), Success: err == nil, Pricing: w.pricing,
	}
	if err != nil {
		record.ErrorCode = fmt.Sprintf("%T", err)
	}
	recordCtx, cancel := types.ModelCallPersistenceContext(ctx)
	defer cancel()
	if recordErr := w.recorder.Record(recordCtx, record); recordErr != nil {
		logger.Warnf(ctx, "record ASR model call failed: %v", recordErr)
	}
	return result, err
}

func (w *observedASR) GetModelName() string { return w.inner.GetModelName() }
func (w *observedASR) GetModelID() string   { return w.inner.GetModelID() }
