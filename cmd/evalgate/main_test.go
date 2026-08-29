package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunFailsAndNamesRegressedMetric(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	current := filepath.Join(dir, "current.json")
	thresholds := filepath.Join(dir, "thresholds.json")
	require.NoError(t, os.WriteFile(baseline, []byte(`{"config_hash":"config-a","metrics":{"retrieval.recall":0.8}}`), 0o600))
	require.NoError(t, os.WriteFile(current, []byte(`{"config_hash":"config-a","metrics":{"retrieval.recall":0.6}}`), 0o600))
	require.NoError(t, os.WriteFile(thresholds, []byte(`[{"metric":"retrieval.recall","direction":"higher_is_better","max_regression":0.01}]`), 0o600))
	var stdout, stderr bytes.Buffer

	exitCode := run([]string{"-baseline", baseline, "-current", current, "-thresholds", thresholds}, &stdout, &stderr)

	assert.Equal(t, 1, exitCode)
	assert.Contains(t, stdout.String(), "retrieval.recall")
	assert.Empty(t, stderr.String())
}

func TestRunCreatesJSONOutputDirectory(t *testing.T) {
	dir := t.TempDir()
	baseline := filepath.Join(dir, "baseline.json")
	current := filepath.Join(dir, "current.json")
	thresholds := filepath.Join(dir, "thresholds.json")
	output := filepath.Join(dir, "nested", "gate.json")
	require.NoError(t, os.WriteFile(baseline, []byte(`{"config_hash":"config-a","metrics":{"retrieval.recall":0.8}}`), 0o600))
	require.NoError(t, os.WriteFile(current, []byte(`{"config_hash":"config-a","metrics":{"retrieval.recall":0.8}}`), 0o600))
	require.NoError(t, os.WriteFile(thresholds, []byte(`[{"metric":"retrieval.recall","direction":"higher_is_better","max_regression":0.01}]`), 0o600))

	exitCode := run([]string{
		"-baseline", baseline, "-current", current, "-thresholds", thresholds, "-json-out", output,
	}, &bytes.Buffer{}, &bytes.Buffer{})

	assert.Equal(t, 0, exitCode)
	_, err := os.Stat(output)
	require.NoError(t, err)
}
