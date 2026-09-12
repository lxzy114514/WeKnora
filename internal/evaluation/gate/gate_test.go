package gate

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareReportsFailsWithNamedRegressions(t *testing.T) {
	baseline := Report{ConfigHash: "config-a", Metrics: map[string]float64{
		"retrieval.recall": 0.80,
		"answer.rougel":    0.62,
		"cost.total_usd":   0.10,
	}, MetricEligibility: map[string]MetricEligibility{"cost.total_usd": {Eligible: true, CoverageRatio: 1}}}
	current := Report{ConfigHash: "config-a", Metrics: map[string]float64{
		"retrieval.recall": 0.70,
		"answer.rougel":    0.61,
		"cost.total_usd":   0.14,
	}, MetricEligibility: map[string]MetricEligibility{"cost.total_usd": {Eligible: true, CoverageRatio: 1}}}

	result, err := Compare(baseline, current, []Threshold{
		{Metric: "retrieval.recall", Direction: HigherIsBetter, MaxRegression: 0.02},
		{Metric: "answer.rougel", Direction: HigherIsBetter, MaxRegression: 0.02},
		{Metric: "cost.total_usd", Direction: LowerIsBetter, MaxRegression: 0.01},
	})

	require.NoError(t, err)
	assert.False(t, result.Passed)
	require.Len(t, result.Failures, 2)
	assert.Equal(t, "retrieval.recall", result.Failures[0].Metric)
	assert.InDelta(t, -0.10, result.Failures[0].Change, 0.000001)
	assert.Equal(t, "cost.total_usd", result.Failures[1].Metric)
}

func TestCompareReportsReturnsAllPassingChecks(t *testing.T) {
	baseline := Report{ConfigHash: "config-a", Metrics: map[string]float64{"retrieval.recall": 0.80, "latency.total_ms": 1000}}
	current := Report{ConfigHash: "config-a", Metrics: map[string]float64{"retrieval.recall": 0.79, "latency.total_ms": 1005}}

	result, err := Compare(baseline, current, []Threshold{
		{Metric: "retrieval.recall", Direction: HigherIsBetter, MaxRegression: 0.02},
		{Metric: "latency.total_ms", Direction: LowerIsBetter, MaxRegression: 10},
	})

	require.NoError(t, err)
	assert.True(t, result.Passed)
	assert.Empty(t, result.Failures)
	assert.Len(t, result.Checks, 2)
}

func TestCompareReportsRejectsMissingMetrics(t *testing.T) {
	_, err := Compare(
		Report{ConfigHash: "config-a", Metrics: map[string]float64{"retrieval.recall": 0.8}},
		Report{ConfigHash: "config-a", Metrics: map[string]float64{}},
		[]Threshold{{Metric: "retrieval.recall", Direction: HigherIsBetter, MaxRegression: 0.01}},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "retrieval.recall")
}

func TestCompareReportsRejectsMissingHashesAndEmptyOrDuplicatePolicy(t *testing.T) {
	metric := map[string]float64{"retrieval.recall": 0.8}
	threshold := Threshold{Metric: "retrieval.recall", Direction: HigherIsBetter, MaxRegression: 0.01}

	_, err := Compare(Report{Metrics: metric}, Report{ConfigHash: "config-a", Metrics: metric}, []Threshold{threshold})
	require.ErrorContains(t, err, "config hashes are required")

	_, err = Compare(
		Report{ConfigHash: "config-a", Metrics: metric}, Report{ConfigHash: "config-a", Metrics: metric}, nil,
	)
	require.ErrorContains(t, err, "at least one threshold")

	_, err = Compare(
		Report{ConfigHash: "config-a", Metrics: metric}, Report{ConfigHash: "config-a", Metrics: metric},
		[]Threshold{threshold, threshold},
	)
	require.ErrorContains(t, err, "duplicate threshold")
}

func TestCompareReportsRejectsDifferentConfigurations(t *testing.T) {
	_, err := Compare(
		Report{ConfigHash: "config-a", Metrics: map[string]float64{"retrieval.recall": 0.8}},
		Report{ConfigHash: "config-b", Metrics: map[string]float64{"retrieval.recall": 0.8}},
		[]Threshold{{Metric: "retrieval.recall", Direction: HigherIsBetter, MaxRegression: 0.01}},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "config hash mismatch")
}

func TestCompareReportsRejectsIneligibleCostInsteadOfPassingEstimatedZero(t *testing.T) {
	threshold := []Threshold{{Metric: "cost.total_usd", Direction: LowerIsBetter, MaxRegression: 0.01}}
	baseline := Report{
		ConfigHash: "config-a", Metrics: map[string]float64{"cost.total_usd": 0},
		MetricEligibility: map[string]MetricEligibility{"cost.total_usd": {Eligible: true, CoverageRatio: 1}},
	}
	current := Report{
		ConfigHash: "config-a", Metrics: map[string]float64{"cost.total_usd": 0},
		MetricEligibility: map[string]MetricEligibility{
			"cost.total_usd": {Eligible: false, CoverageRatio: 0, Reason: "price coverage incomplete (0.000000)"},
		},
	}

	_, err := Compare(baseline, current, threshold)
	require.ErrorContains(t, err, `current metric "cost.total_usd" is ineligible`)
}

func TestCompareReportsRejectsLegacyCostWithoutEligibilityMetadata(t *testing.T) {
	report := Report{ConfigHash: "config-a", Metrics: map[string]float64{"cost.total_usd": 0}}
	_, err := Compare(report, report, []Threshold{{Metric: "cost.total_usd", Direction: LowerIsBetter, MaxRegression: 0.01}})
	require.ErrorContains(t, err, `baseline cost metric "cost.total_usd" eligibility metadata is required`)
}

func TestCompareReportsRejectsContradictoryOrInvalidCoverage(t *testing.T) {
	threshold := []Threshold{{Metric: "cost.total_usd", Direction: LowerIsBetter, MaxRegression: 0.01}}
	tests := []struct {
		name     string
		coverage float64
		want     string
	}{
		{name: "eligible with zero coverage", coverage: 0, want: "cannot be eligible with incomplete coverage ratio"},
		{name: "negative coverage", coverage: -0.1, want: "has invalid coverage ratio"},
		{name: "coverage above one", coverage: 1.1, want: "has invalid coverage ratio"},
		{name: "not a number", coverage: math.NaN(), want: "has invalid coverage ratio"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Report{
				ConfigHash: "config-a",
				Metrics:    map[string]float64{"cost.total_usd": 0},
				MetricEligibility: map[string]MetricEligibility{
					"cost.total_usd": {Eligible: true, CoverageRatio: tt.coverage},
				},
			}
			_, err := Compare(report, report, threshold)
			require.ErrorContains(t, err, tt.want)
		})
	}
}
