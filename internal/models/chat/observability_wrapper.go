package chat

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

type CallRecorder interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
}

type observedChat struct {
	inner    Chat
	tenantID uint64
	recorder CallRecorder
	pricing  types.ModelPricing
}

func NewObservedChat(inner Chat, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing) Chat {
	if inner == nil || recorder == nil {
		return inner
	}
	return &observedChat{inner: inner, tenantID: tenantID, recorder: recorder, pricing: pricing}
}

func (w *observedChat) Chat(
	ctx context.Context, messages []Message, opts *ChatOptions,
) (*types.ChatResponse, error) {
	started := time.Now()
	response, err := w.inner.Chat(ctx, messages, opts)
	var usage *types.TokenUsage
	if response != nil {
		usage = &response.Usage
	}
	w.record(ctx, started, usage, err)
	return response, err
}

func (w *observedChat) ChatStream(
	ctx context.Context, messages []Message, opts *ChatOptions,
) (<-chan types.StreamResponse, error) {
	started := time.Now()
	inner, err := w.inner.ChatStream(ctx, messages, opts)
	if err != nil {
		w.record(ctx, started, nil, err)
		return nil, err
	}
	output := make(chan types.StreamResponse)
	go func() {
		defer close(output)
		var usage *types.TokenUsage
		var streamErr error
		for response := range inner {
			if response.Usage != nil {
				copy := *response.Usage
				usage = &copy
			}
			if response.ResponseType == types.ResponseTypeError {
				// Provider error bodies can contain sensitive material. A typed
				// failure marker is sufficient for telemetry and aggregation.
				streamErr = errors.New("chat stream returned an error event")
			}
			output <- response
		}
		w.record(ctx, started, usage, streamErr)
	}()
	return output, nil
}

func (w *observedChat) record(
	ctx context.Context, started time.Time, usage *types.TokenUsage, callErr error,
) {
	purpose, prefix := types.LLMCallMetadataFromContext(ctx)
	if purpose == "" {
		purpose = "chat"
	}
	record := &types.ModelCallRecord{
		TenantID: w.tenantID, ModelID: w.inner.GetModelID(), ModelType: types.ModelTypeKnowledgeQA,
		Purpose: purpose, CorrelationID: types.ModelCallCorrelationFromContext(ctx),
		PromptPrefixFingerprint: prefix, DurationMS: time.Since(started).Milliseconds(),
		Success: callErr == nil, Pricing: w.pricing,
	}
	if usage != nil {
		record.PromptTokens = usage.PromptTokens
		record.CompletionTokens = usage.CompletionTokens
		record.TotalTokens = usage.TotalTokens
		record.CacheReadTokens = usage.CacheReadTokens
		record.CacheWriteTokens = usage.CacheWriteTokens
		record.CacheMissTokens = usage.CacheMissTokens
	}
	if callErr != nil {
		record.ErrorCode = fmt.Sprintf("%T", callErr)
	}
	recordCtx, cancel := types.ModelCallPersistenceContext(ctx)
	defer cancel()
	if err := w.recorder.Record(recordCtx, record); err != nil {
		logger.Warnf(ctx, "record chat model call failed: %v", err)
	}
}

func (w *observedChat) GetModelName() string { return w.inner.GetModelName() }
func (w *observedChat) GetModelID() string   { return w.inner.GetModelID() }
