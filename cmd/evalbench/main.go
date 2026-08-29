// Command evalbench generates a deterministic quality report by running the
// repository's current metric implementations over reviewed case evidence.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/internal/application/service/metric"
	"github.com/Tencent/WeKnora/internal/evaluation/gate"
	"github.com/Tencent/WeKnora/internal/types"
)

type benchmarkInput struct {
	RunID      string              `json:"run_id"`
	ConfigHash string              `json:"config_hash"`
	Cases      []types.MetricInput `json:"cases"`
}

func main() {
	input := flag.String("input", "testdata/evaluation/deterministic-cases.json", "reviewed case evidence")
	output := flag.String("output", "artifacts/deterministic-report.json", "generated report")
	flag.Parse()
	if err := run(*input, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(inputPath, outputPath string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("evalbench: read input: %w", err)
	}
	var input benchmarkInput
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("evalbench: decode input: %w", err)
	}
	if input.ConfigHash == "" || len(input.Cases) == 0 {
		return fmt.Errorf("evalbench: config hash and at least one case are required")
	}
	report := gate.Report{RunID: input.RunID, ConfigHash: input.ConfigHash, Metrics: map[string]float64{}}
	for _, definition := range metric.Definitions() {
		for i := range input.Cases {
			report.Metrics[definition.Name] += definition.Compute(&input.Cases[i])
		}
		report.Metrics[definition.Name] /= float64(len(input.Cases))
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("evalbench: encode report: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("evalbench: create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("evalbench: write report: %w", err)
	}
	return nil
}
