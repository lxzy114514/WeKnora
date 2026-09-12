package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchEvaluationCasesUsesBoundedPages(t *testing.T) {
	const total = 205
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		require.NoError(t, err)
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		require.NoError(t, err)
		assert.LessOrEqual(t, limit, 100)
		end := min(offset+limit, total)
		page := make([]*types.EvaluationCaseResult, 0, max(0, end-offset))
		for qid := offset; qid < end; qid++ {
			page = append(page, &types.EvaluationCaseResult{QID: qid})
		}
		require.NoError(t, json.NewEncoder(w).Encode(casesResponse{Success: true, Data: page}))
	}))
	defer server.Close()

	cases, err := fetchEvaluationCases(
		context.Background(), server.Client(), server.URL+"/api/v1/evaluation", "run-1", "key", "", "7",
	)
	require.NoError(t, err)
	require.Len(t, cases, total)
	assert.Equal(t, 0, cases[0].QID)
	assert.Equal(t, total-1, cases[total-1].QID)
	assert.Equal(t, 3, requests)
}

func TestPortableReportIncludesQualityCostAndLatency(t *testing.T) {
	detail := &types.EvaluationDetail{
		Task:   &types.EvaluationTask{ID: "run-1"},
		Config: &types.EvaluationConfigSnapshot{ConfigHash: "config-hash", DatasetHash: "dataset-hash"},
		Metric: &types.MetricResult{
			RetrievalMetrics:  types.RetrievalMetrics{Recall: 0.8, MRR: 0.7},
			GenerationMetrics: types.GenerationMetrics{ROUGEL: 0.6},
		},
		Cost:     &types.EvaluationCostReport{Total: 0.12, CallCount: 1, PricedCallCount: 1, CostCoverageRatio: 1, CostGateEligible: true},
		Duration: &types.EvaluationDurationReport{TotalMS: 1234, AverageCaseMS: 321, RetrievalMS: 222},
	}

	report := portableReport(detail)

	assert.Equal(t, "run-1", report.RunID)
	assert.Equal(t, "config-hash", report.ConfigHash)
	assert.Equal(t, 0.8, report.Metrics["retrieval.recall"])
	assert.Equal(t, 0.6, report.Metrics["answer.rougel"])
	assert.Equal(t, 0.12, report.Metrics["cost.total_usd"])
	assert.Equal(t, float64(1234), report.Metrics["latency.total_ms"])
	assert.Equal(t, float64(222), report.Metrics["latency.retrieval_ms"])
}

func TestPortableReportMarksUnknownCostIneligibleForTheGate(t *testing.T) {
	report := portableReport(&types.EvaluationDetail{
		Task: &types.EvaluationTask{ID: "run-unknown-cost"},
		Cost: &types.EvaluationCostReport{Total: 0, CallCount: 2, PricedCallCount: 0, CostCoverageRatio: 0},
	})

	assert.Equal(t, 0.0, report.Metrics["cost.total_usd"])
	assert.False(t, report.MetricEligibility["cost.total_usd"].Eligible)
	assert.Contains(t, report.MetricEligibility["cost.total_usd"].Reason, "price coverage incomplete")
}

func TestWriteMarkdownRendersIneligibleCostAsUnknown(t *testing.T) {
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{ID: "run-unknown-cost", StartTime: time.Unix(100, 0)},
		Cost: &types.EvaluationCostReport{Total: 0, CallCount: 1, CostCoverageRatio: 0},
	}
	path := filepath.Join(t.TempDir(), "report.md")
	require.NoError(t, writeMarkdown(path, detail))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "| cost.total_usd | unknown (price coverage incomplete (0.000000)) |")
}

func TestWriteMarkdownIncludesConfigurationMetricsAndCaseEvidence(t *testing.T) {
	end := time.Unix(200, 0)
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID: "run-1", DatasetID: "baseline-v1", StartTime: time.Unix(100, 0), EndTime: &end,
		},
		Config: &types.EvaluationConfigSnapshot{
			DatasetVersion: "1", DatasetHash: "dataset-hash", ConfigHash: "config-hash", CodeVersion: "sha",
		},
		Metric: &types.MetricResult{RetrievalMetrics: types.RetrievalMetrics{Recall: 0.8}},
		Cases: []*types.EvaluationCaseResult{{
			QID: 3, RetrievedPIDs: []int{1, 2}, RerankedPIDs: []int{1},
			DurationMS: 42, FinalAnswer: "answer | evidence",
		}},
	}
	path := filepath.Join(t.TempDir(), "report.md")
	require.NoError(t, writeMarkdown(path, detail))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.True(t, strings.Contains(text, "Configuration hash: `config-hash`"))
	assert.True(t, strings.Contains(text, "retrieval.recall"))
	assert.True(t, strings.Contains(text, "answer \\| evidence"))
}
