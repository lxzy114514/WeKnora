package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservedChatRecordsNonStreamingAndStreamingUsageOnce(t *testing.T) {
	recorder := &chatCallRecorder{}
	inner := &usageChat{}
	observed := NewObservedChat(inner, 7, recorder, types.ModelPricing{InputPerMillion: 1})
	ctx := types.WithModelCallCorrelation(
		types.WithLLMCallMetadata(context.Background(), "knowledge_qa", "opaque-prefix"),
		"eval-1",
	)

	_, err := observed.Chat(ctx, nil, nil)
	require.NoError(t, err)
	stream, err := observed.ChatStream(ctx, nil, nil)
	require.NoError(t, err)
	for range stream {
	}

	require.Len(t, recorder.records, 2)
	for _, record := range recorder.records {
		assert.Equal(t, "knowledge_qa", record.Purpose)
		assert.Equal(t, "eval-1", record.CorrelationID)
		assert.Equal(t, "opaque-prefix", record.PromptPrefixFingerprint)
		assert.Equal(t, 10, record.PromptTokens)
		assert.Equal(t, 6, record.CacheReadTokens)
		assert.True(t, record.Success)
	}
}

func TestObservedChatRecordsStreamErrorEventAsFailure(t *testing.T) {
	recorder := &chatCallRecorder{}
	observed := NewObservedChat(errorStreamChat{}, 7, recorder, types.ModelPricing{})

	stream, err := observed.ChatStream(context.Background(), nil, nil)
	require.NoError(t, err)
	for range stream {
	}

	require.Len(t, recorder.records, 1)
	assert.False(t, recorder.records[0].Success)
	assert.Equal(t, "*errors.errorString", recorder.records[0].ErrorCode)
	assert.Equal(t, 10, recorder.records[0].PromptTokens)
}

type usageChat struct{}

func (usageChat) Chat(context.Context, []Message, *ChatOptions) (*types.ChatResponse, error) {
	return &types.ChatResponse{Usage: types.TokenUsage{
		PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, CacheReadTokens: 6, CacheMissTokens: 4,
	}}, nil
}

func (usageChat) ChatStream(context.Context, []Message, *ChatOptions) (<-chan types.StreamResponse, error) {
	ch := make(chan types.StreamResponse, 1)
	usage := &types.TokenUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, CacheReadTokens: 6, CacheMissTokens: 4}
	ch <- types.StreamResponse{Done: true, Usage: usage}
	close(ch)
	return ch, nil
}

func (usageChat) GetModelName() string { return "chat-a" }
func (usageChat) GetModelID() string   { return "chat-a" }

type errorStreamChat struct{ usageChat }

func (errorStreamChat) ChatStream(context.Context, []Message, *ChatOptions) (<-chan types.StreamResponse, error) {
	ch := make(chan types.StreamResponse, 1)
	ch <- types.StreamResponse{
		ResponseType: types.ResponseTypeError,
		Content:      "provider detail must not be persisted",
		Usage:        &types.TokenUsage{PromptTokens: 10},
	}
	close(ch)
	return ch, nil
}

type chatCallRecorder struct{ records []*types.ModelCallRecord }

func (r *chatCallRecorder) Record(_ context.Context, record *types.ModelCallRecord) error {
	if record.ErrorCode == "force" {
		return errors.New("forced")
	}
	copy := *record
	r.records = append(r.records, &copy)
	return nil
}
