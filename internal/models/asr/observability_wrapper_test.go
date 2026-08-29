package asr

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type observedASRFake struct{ err error }

func (f observedASRFake) Transcribe(context.Context, []byte, string) (*TranscriptionResult, error) {
	return &TranscriptionResult{Text: "transcript"}, f.err
}
func (observedASRFake) GetModelName() string { return "speech" }
func (observedASRFake) GetModelID() string   { return "asr-1" }

type asrRecordSink struct{ records []*types.ModelCallRecord }

func (s *asrRecordSink) Record(_ context.Context, record *types.ModelCallRecord) error {
	s.records = append(s.records, record)
	return nil
}

func TestObservedASRRecordsBytesWithoutPayload(t *testing.T) {
	sink := &asrRecordSink{}
	wrapped := NewObservedASR(observedASRFake{}, 9, sink, types.ModelPricing{Currency: "USD"})
	ctx := types.WithModelCallCorrelation(context.Background(), "eval-2")
	if _, err := wrapped.Transcribe(ctx, []byte{1, 2, 3, 4}, "secret.wav"); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 1 {
		t.Fatalf("records = %d, want 1", len(sink.records))
	}
	record := sink.records[0]
	if record.ModelType != types.ModelTypeASR || record.CorrelationID != "eval-2" || record.InputUnits != 4 {
		t.Fatalf("unexpected record: %#v", record)
	}
}

func TestObservedASRRecordsFailureOnce(t *testing.T) {
	sink := &asrRecordSink{}
	wantErr := errors.New("provider unavailable")
	wrapped := NewObservedASR(observedASRFake{err: wantErr}, 9, sink, types.ModelPricing{})
	_, err := wrapped.Transcribe(context.Background(), nil, "audio.wav")
	if !errors.Is(err, wantErr) || len(sink.records) != 1 || sink.records[0].Success {
		t.Fatalf("err=%v records=%#v", err, sink.records)
	}
}
