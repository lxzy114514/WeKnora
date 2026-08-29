package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEvaluationRepositoryPersistsTenantScopedRunAndCases(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:evaluation-repo?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&evaluationRunRow{}, &evaluationCaseRow{}))
	repo := NewEvaluationRepository(db)
	ctx := context.Background()
	detail := &types.EvaluationDetail{
		Task: &types.EvaluationTask{
			ID: "run-1", TenantID: 7, DatasetID: "baseline-v1",
			StartTime: time.Unix(100, 0), Status: types.EvaluationStatueRunning, Total: 2,
		},
		Config: &types.EvaluationConfigSnapshot{DatasetID: "baseline-v1", DatasetHash: "abc", CodeVersion: "sha"},
		Metric: &types.MetricResult{RetrievalMetrics: types.RetrievalMetrics{Recall: 0.75}},
	}

	require.NoError(t, repo.Create(ctx, detail))
	got, err := repo.Get(ctx, 7, "run-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "abc", got.Config.DatasetHash)
	assert.InDelta(t, 0.75, got.Metric.RetrievalMetrics.Recall, 0.000001)

	notVisible, err := repo.Get(ctx, 8, "run-1")
	require.NoError(t, err)
	assert.Nil(t, notVisible)

	caseResult := &types.EvaluationCaseResult{
		TaskID: "run-1", TenantID: 7, QID: 42, DurationMS: 123,
		Stages:        &types.EvaluationStageDuration{RetrievalMS: 30, RerankMS: 12, GenerationMS: 70},
		RetrievedPIDs: []int{2, 9}, RerankedPIDs: []int{9}, FinalAnswer: "answer evidence",
		Metric: &types.MetricResult{RetrievalMetrics: types.RetrievalMetrics{Recall: 1}},
	}
	require.NoError(t, repo.UpsertCase(ctx, caseResult))
	cases, err := repo.ListCases(ctx, 7, "run-1", 100, 0)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, 42, cases[0].QID)
	assert.Equal(t, []int{2, 9}, cases[0].RetrievedPIDs)
	assert.Equal(t, []int{9}, cases[0].RerankedPIDs)
	assert.Equal(t, "answer evidence", cases[0].FinalAnswer)
	assert.Equal(t, int64(30), cases[0].Stages.RetrievalMS)

	page, err := repo.ListCases(ctx, 7, "run-1", 1, 1)
	require.NoError(t, err)
	assert.Empty(t, page)
}

func TestEvaluationRepositoryRecoversInterruptedRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:evaluation-recover?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&evaluationRunRow{}, &evaluationCaseRow{}))
	repo := NewEvaluationRepository(db)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, &types.EvaluationDetail{Task: &types.EvaluationTask{
		ID: "run-interrupted", TenantID: 1, DatasetID: "baseline-v1",
		StartTime: time.Now(), Status: types.EvaluationStatueRunning,
	}}))

	require.NoError(t, repo.RecoverInterrupted(ctx, "service restarted before evaluation completed"))
	got, err := repo.Get(ctx, 1, "run-interrupted")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, types.EvaluationStatueFailed, got.Task.Status)
	assert.Contains(t, got.Task.ErrMsg, "service restarted")
	assert.NotNil(t, got.Task.EndTime)
}
