package types

import "testing"

func TestEvaluationConfigFingerprintCoversEffectiveConfiguration(t *testing.T) {
	base := EvaluationConfigSnapshot{
		DatasetID: "baseline-v1", DatasetVersion: "1", DatasetHash: "dataset-sha",
		ChatModelID: "chat-1", EmbeddingModelID: "embedding-1", RerankModelID: "rerank-1",
		ChunkSize: 512, ChunkOverlap: 64, CodeVersion: "commit-sha",
	}
	hash, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	base.ConfigHash = hash
	if got, err := base.Fingerprint(); err != nil || got != hash {
		t.Fatalf("fingerprint is not idempotent: got %s want %s", got, hash)
	}
	changed := base
	changed.ChunkSize++
	if got, err := changed.Fingerprint(); err != nil || got == hash {
		t.Fatal("chunk configuration change did not change fingerprint")
	}
	changedPipeline := base
	changedPipeline.PipelineHash = "different-pipeline"
	if got, err := changedPipeline.Fingerprint(); err != nil || got == hash {
		t.Fatal("pipeline configuration change did not change fingerprint")
	}
	codeOnly := base
	codeOnly.CodeVersion = "different-implementation"
	if got, err := codeOnly.Fingerprint(); err != nil || got != hash {
		t.Fatalf("code version must not change condition hash: got %s want %s", got, hash)
	}
}
