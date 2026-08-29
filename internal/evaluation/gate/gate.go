// Package gate contains the deterministic evaluation regression policy.
// It deliberately has no database, network, or process-exit dependencies so
// the exact same comparison is used by unit tests, CI, and local commands.
package gate

import "fmt"

type Direction string

const (
	HigherIsBetter Direction = "higher_is_better"
	LowerIsBetter  Direction = "lower_is_better"
)

// Report is the portable input/output shape used by the evaluation CLI.
// Dot-separated metric names keep the gate independent from report nesting.
type Report struct {
	RunID      string             `json:"run_id,omitempty"`
	ConfigHash string             `json:"config_hash,omitempty"`
	Metrics    map[string]float64 `json:"metrics"`
}

type Threshold struct {
	Metric        string    `json:"metric" yaml:"metric"`
	Direction     Direction `json:"direction" yaml:"direction"`
	MaxRegression float64   `json:"max_regression" yaml:"max_regression"`
}

type Check struct {
	Metric        string    `json:"metric"`
	Direction     Direction `json:"direction"`
	Baseline      float64   `json:"baseline"`
	Current       float64   `json:"current"`
	Change        float64   `json:"change"`
	MaxRegression float64   `json:"max_regression"`
	Passed        bool      `json:"passed"`
}

type Result struct {
	Passed   bool    `json:"passed"`
	Checks   []Check `json:"checks"`
	Failures []Check `json:"failures,omitempty"`
}

func Compare(baseline, current Report, thresholds []Threshold) (Result, error) {
	if baseline.ConfigHash == "" || current.ConfigHash == "" {
		return Result{}, fmt.Errorf("evaluation gate: baseline and current config hashes are required")
	}
	if baseline.ConfigHash != current.ConfigHash {
		return Result{}, fmt.Errorf(
			"evaluation gate: config hash mismatch (baseline=%s current=%s)",
			baseline.ConfigHash,
			current.ConfigHash,
		)
	}
	if len(thresholds) == 0 {
		return Result{}, fmt.Errorf("evaluation gate: at least one threshold is required")
	}
	result := Result{Passed: true, Checks: make([]Check, 0, len(thresholds))}
	seenMetrics := make(map[string]struct{}, len(thresholds))
	for _, threshold := range thresholds {
		if threshold.Metric == "" {
			return Result{}, fmt.Errorf("evaluation gate: metric name is required")
		}
		if threshold.MaxRegression < 0 {
			return Result{}, fmt.Errorf("evaluation gate: max regression for %q cannot be negative", threshold.Metric)
		}
		if _, exists := seenMetrics[threshold.Metric]; exists {
			return Result{}, fmt.Errorf("evaluation gate: duplicate threshold for metric %q", threshold.Metric)
		}
		seenMetrics[threshold.Metric] = struct{}{}
		base, ok := baseline.Metrics[threshold.Metric]
		if !ok {
			return Result{}, fmt.Errorf("evaluation gate: baseline metric %q is missing", threshold.Metric)
		}
		value, ok := current.Metrics[threshold.Metric]
		if !ok {
			return Result{}, fmt.Errorf("evaluation gate: current metric %q is missing", threshold.Metric)
		}

		change := value - base
		passed := false
		switch threshold.Direction {
		case HigherIsBetter:
			passed = change >= -threshold.MaxRegression
		case LowerIsBetter:
			passed = change <= threshold.MaxRegression
		default:
			return Result{}, fmt.Errorf(
				"evaluation gate: unsupported direction %q for metric %q",
				threshold.Direction,
				threshold.Metric,
			)
		}

		check := Check{
			Metric:        threshold.Metric,
			Direction:     threshold.Direction,
			Baseline:      base,
			Current:       value,
			Change:        change,
			MaxRegression: threshold.MaxRegression,
			Passed:        passed,
		}
		result.Checks = append(result.Checks, check)
		if !passed {
			result.Passed = false
			result.Failures = append(result.Failures, check)
		}
	}
	return result, nil
}
