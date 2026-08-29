package service

import (
	"runtime/debug"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFormatEvaluationCodeVersionMarksDirtyBuilds(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.modified", Value: "true"},
	}
	if got := formatEvaluationCodeVersion(settings, "(devel)"); got != "abc123+dirty" {
		t.Fatalf("code version = %q, want abc123+dirty", got)
	}
}

func TestEvaluationModelSnapshotExcludesCredentialsAndTracksParameters(t *testing.T) {
	model := &types.Model{
		ID: "embed-1", Name: "embedding-model", Type: types.ModelTypeEmbedding,
		Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			BaseURL: "https://models.example/v1", APIKey: "secret-a", AppSecret: "secret-b",
			Provider: "generic", EmbeddingParameters: types.EmbeddingParameters{Dimension: 1024},
		},
	}
	first, err := evaluationModelSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	model.Parameters.APIKey = "rotated-secret"
	second, err := evaluationModelSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	if first.ParameterHash != second.ParameterHash {
		t.Fatal("credential rotation changed non-secret parameter hash")
	}
	model.Parameters.BaseURL = "https://new-models.example/v1"
	third, err := evaluationModelSnapshot(model)
	if err != nil {
		t.Fatal(err)
	}
	if first.ParameterHash == third.ParameterHash {
		t.Fatal("provider configuration change did not change parameter hash")
	}
}

func TestEvaluationStageDurationsGroupsPipelineEvents(t *testing.T) {
	got := evaluationStageDurations(map[types.EventType]int64{
		types.QUERY_UNDERSTAND:  4,
		types.CHUNK_SEARCH:      10,
		types.CHUNK_MERGE:       3,
		types.CHUNK_RERANK:      7,
		types.INTO_CHAT_MESSAGE: 2,
		types.CHAT_COMPLETION:   20,
	})
	if got.RetrievalMS != 17 || got.RerankMS != 7 || got.GenerationMS != 22 {
		t.Fatalf("stage durations = %+v", got)
	}
}
