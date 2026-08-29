package service

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestMetricHookCountsIrrelevantRetrievedPassagesAsFalsePositives(t *testing.T) {
	corpus := []string{"", "alpha is relevant", "beta is irrelevant"}
	hook := NewHookMetric(1, corpus)
	hook.recordInit(0)
	hook.recordQaPair(0, &types.QAPair{
		QID: 1, PIDs: []int{1}, Passages: []string{corpus[1]}, Answer: "answer",
	})
	hook.recordSearchResult(0, []*types.SearchResult{
		{Content: "alpha is relevant"},
		{Content: "beta is irrelevant"},
	})
	hook.recordChatResponse(0, &types.ChatResponse{Content: "answer"})

	result := hook.recordFinish(0)
	if result.RetrievalMetrics.Precision != 0.5 {
		t.Fatalf("precision = %v, want 0.5", result.RetrievalMetrics.Precision)
	}
	if result.RetrievalMetrics.Recall != 1 {
		t.Fatalf("recall = %v, want 1", result.RetrievalMetrics.Recall)
	}
}

func TestMapSearchResultsToPassageIDsProvidesReproducibleEvidence(t *testing.T) {
	corpus := []string{"", "alpha relevant passage", "beta irrelevant passage"}
	got := mapSearchResultsToPassageIDs(corpus, []*types.SearchResult{
		{Content: "alpha relevant"},
		{Content: "beta irrelevant passage"},
		{Content: "alpha relevant"},
	})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("mapped passage IDs = %v, want [1 2]", got)
	}
}

func TestMapSearchResultsPrefersStablePassageMetadataOverDuplicateText(t *testing.T) {
	metadata, err := json.Marshal(map[string]string{sourcePassageIndexMetadataKey: "2"})
	if err != nil {
		t.Fatal(err)
	}
	corpus := []string{"", "duplicate passage", "duplicate passage"}
	got := mapSearchResultsToPassageIDs(corpus, []*types.SearchResult{{
		Content: "duplicate passage", ChunkMetadata: types.JSON(metadata),
	}})
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("mapped passage IDs = %v, want [2]", got)
	}
}
