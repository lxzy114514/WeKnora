package vlm

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type observedVLMFake struct{ err error }

func (f observedVLMFake) Predict(context.Context, [][]byte, string) (string, error) {
	return "visual answer", f.err
}
func (observedVLMFake) GetModelName() string { return "vision" }
func (observedVLMFake) GetModelID() string   { return "vlm-1" }

type vlmRecordSink struct{ records []*types.ModelCallRecord }

func (s *vlmRecordSink) Record(_ context.Context, record *types.ModelCallRecord) error {
	s.records = append(s.records, record)
	return nil
}

func TestObservedVLMRecordsEstimatedUsageWithoutContent(t *testing.T) {
	sink := &vlmRecordSink{}
	wrapped := NewObservedVLM(observedVLMFake{}, 7, sink, types.ModelPricing{Currency: "USD", InputPerMillion: 1})
	ctx := types.WithModelCallCorrelation(context.Background(), "eval-1")
	if _, err := wrapped.Predict(ctx, [][]byte{{1, 2, 3}}, "describe image"); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("records = %d, want 1", len(sink.records))
	}
	record := sink.records[0]
	if record.ModelType != types.ModelTypeVLLM || record.CorrelationID != "eval-1" || !record.CostEstimated {
		t.Fatalf("unexpected record: %#v", record)
	}
	if record.PromptTokens == 0 || record.CompletionTokens == 0 || record.ItemCount != 1 {
		t.Fatalf("usage was not estimated: %#v", record)
	}
}

func TestObservedVLMRecordsFailureOnce(t *testing.T) {
	sink := &vlmRecordSink{}
	wantErr := errors.New("provider unavailable")
	wrapped := NewObservedVLM(observedVLMFake{err: wantErr}, 7, sink, types.ModelPricing{})
	_, err := wrapped.Predict(context.Background(), nil, "prompt")
	if !errors.Is(err, wantErr) || len(sink.records) != 1 || sink.records[0].Success {
		t.Fatalf("err=%v records=%#v", err, sink.records)
	}
}
