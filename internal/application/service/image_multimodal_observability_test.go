package service

import (
	"context"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type legacyObservedVLMFake struct{}

func (legacyObservedVLMFake) Predict(_ context.Context, _ [][]byte, prompt string) (string, error) {
	return "response to " + prompt, nil
}
func (legacyObservedVLMFake) GetModelName() string { return "legacy-vision" }
func (legacyObservedVLMFake) GetModelID() string   { return "legacy-vlm-stable" }

type legacyVLMCallRecorder struct {
	mu      sync.Mutex
	records []*types.ModelCallRecord
}

func (r *legacyVLMCallRecorder) Record(_ context.Context, record *types.ModelCallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *record
	r.records = append(r.records, &copy)
	return nil
}

func TestLegacyInlineVLMRecordsOCRAndCaptionCalls(t *testing.T) {
	recorder := &legacyVLMCallRecorder{}
	service := &ImageMultimodalService{modelCallRecorder: recorder}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))
	model := service.observeLegacyVLM(ctx, legacyObservedVLMFake{})

	_, err := model.Predict(types.WithLLMCallMetadata(ctx, "vlm_ocr", ""), [][]byte{{1}}, "ocr")
	require.NoError(t, err)
	_, err = model.Predict(types.WithLLMCallMetadata(ctx, "vlm_caption", ""), [][]byte{{2}}, "caption")
	require.NoError(t, err)

	require.Len(t, recorder.records, 2)
	assert.Equal(t, "vlm_ocr", recorder.records[0].Purpose)
	assert.Equal(t, "vlm_caption", recorder.records[1].Purpose)
	assert.Equal(t, uint64(7), recorder.records[0].TenantID)
	assert.Equal(t, "legacy-vlm-stable", recorder.records[0].ModelID)
}
