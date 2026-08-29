package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDatasetServiceLoadsVersionedVerifiedSnapshot(t *testing.T) {
	service := NewDatasetServiceWithRoot(filepath.Join("..", "..", "..", "dataset"))

	snapshot, err := service.GetDatasetSnapshot(context.Background(), "default")

	require.NoError(t, err)
	assert.Equal(t, "baseline-v1", snapshot.ID)
	assert.Equal(t, "1", snapshot.Version)
	assert.Len(t, snapshot.Files, 5)
	assert.Len(t, snapshot.Hash, 64)
	require.NotEmpty(t, snapshot.QAPairs)
	for i := 1; i < len(snapshot.QAPairs); i++ {
		assert.Less(t, snapshot.QAPairs[i-1].QID, snapshot.QAPairs[i].QID)
	}
}

func TestDatasetServiceRejectsUnsafeDatasetID(t *testing.T) {
	service := NewDatasetServiceWithRoot(filepath.Join("..", "..", "..", "dataset"))

	_, err := service.GetDatasetSnapshot(context.Background(), "../samples")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid dataset ID")
}

func TestDatasetServiceRejectsChangedDatasetFile(t *testing.T) {
	root := t.TempDir()
	writeDatasetFixture(t, root, "baseline-v1", "samples/queries.parquet", "wrong-hash")
	service := NewDatasetServiceWithRoot(root)

	_, err := service.GetDatasetSnapshot(context.Background(), "baseline-v1")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hash mismatch")
}

func writeDatasetFixture(t *testing.T, root, id, filePath, hash string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "manifests"), 0o755))
	fullPath := filepath.Join(root, filepath.FromSlash(filePath))
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte("changed"), 0o600))
	manifest := fmt.Sprintf(`{
  "id": %q,
  "version": "1",
  "files": {
    "queries": {"path": %q, "size": 7, "sha256": %q},
    "corpus": {"path": %q, "size": 7, "sha256": %q},
    "answers": {"path": %q, "size": 7, "sha256": %q},
    "qrels": {"path": %q, "size": 7, "sha256": %q},
    "qas": {"path": %q, "size": 7, "sha256": %q}
  }
}`, id, filePath, hash, filePath, hash, filePath, hash, filePath, hash, filePath, hash)
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifests", id+".json"), []byte(manifest), 0o600))
}
