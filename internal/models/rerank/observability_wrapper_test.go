package rerank

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObservedRerankerRecordsDocumentUnits(t *testing.T) {
	recorder := &rerankCallRecorder{}
	observed := NewObservedReranker(fakeObservedReranker{}, 7, recorder, types.ModelPricing{RerankPerThousand: 1})
	ctx := types.WithModelCallCorrelation(context.Background(), "eval-1")

	result, err := observed.Rerank(ctx, "question", []string{"a", "b", "c"})

	require.NoError(t, err)
	assert.Len(t, result, 1)
	require.Len(t, recorder.records, 1)
	assert.Equal(t, 3, recorder.records[0].InputUnits)
	assert.Equal(t, "eval-1", recorder.records[0].CorrelationID)
}

type fakeObservedReranker struct{}

func (fakeObservedReranker) Rerank(context.Context, string, []string) ([]RankResult, error) {
	return []RankResult{{Index: 0}}, nil
}
func (fakeObservedReranker) GetModelName() string { return "rerank-a" }
func (fakeObservedReranker) GetModelID() string   { return "rerank-a" }

type rerankCallRecorder struct{ records []*types.ModelCallRecord }

func (r *rerankCallRecorder) Record(_ context.Context, record *types.ModelCallRecord) error {
	copy := *record
	r.records = append(r.records, &copy)
	return nil
}
