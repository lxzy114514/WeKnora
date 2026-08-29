package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Tencent/WeKnora/internal/evaluation/gate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGeneratesReportUsingCurrentMetricImplementations(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "cases.json")
	output := filepath.Join(dir, "nested", "report.json")
	require.NoError(t, os.WriteFile(input, []byte(`{
  "run_id":"contract", "config_hash":"cases-v1",
  "cases":[{"RetrievalGT":[[1]],"RetrievalIDs":[1,2],"GeneratedTexts":"same answer","GeneratedGT":"same answer"}]
}`), 0o600))

	require.NoError(t, run(input, output))
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	var report gate.Report
	require.NoError(t, json.Unmarshal(data, &report))
	assert.Equal(t, "cases-v1", report.ConfigHash)
	assert.Equal(t, 0.5, report.Metrics["retrieval.precision"])
	assert.Equal(t, 1.0, report.Metrics["retrieval.recall"])
	assert.InDelta(t, 1.0, report.Metrics["answer.rougel"], 0.000001)
}
