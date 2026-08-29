package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelUsageParsesTenantModelAndTimeFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &capturingModelCallRepo{summary: &types.ModelUsageSummary{CallCount: 3}}
	handler := &ModelHandler{usageRepo: repo}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/models/usage?model_id=chat-a&purpose=knowledge_qa&start_time=2026-08-01T00:00:00Z&end_time=2026-08-02T00:00:00Z", nil)
	request = request.WithContext(context.WithValue(request.Context(), types.TenantIDContextKey, uint64(7)))
	ctx.Request = request

	handler.GetModelUsage(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, uint64(7), repo.query.TenantID)
	assert.Equal(t, "chat-a", repo.query.ModelID)
	assert.Equal(t, "knowledge_qa", repo.query.Purpose)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), repo.query.StartTime)
	assert.Contains(t, recorder.Body.String(), `"call_count":3`)
}

type capturingModelCallRepo struct {
	query   types.ModelUsageQuery
	summary *types.ModelUsageSummary
}

func (r *capturingModelCallRepo) Record(context.Context, *types.ModelCallRecord) error { return nil }
func (r *capturingModelCallRepo) Summary(_ context.Context, query types.ModelUsageQuery) (*types.ModelUsageSummary, error) {
	r.query = query
	return r.summary, nil
}
func (r *capturingModelCallRepo) ListByCorrelation(context.Context, uint64, string) ([]*types.ModelCallRecord, error) {
	return nil, nil
}

func TestGetModelUsageRejectsInvalidTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &ModelHandler{usageRepo: &capturingModelCallRepo{}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/models/usage?start_time=not-a-time", nil)
	request = request.WithContext(context.WithValue(request.Context(), types.TenantIDContextKey, uint64(7)))
	ctx.Request = request

	handler.GetModelUsage(ctx)

	require.Len(t, ctx.Errors, 1)
	assert.Contains(t, ctx.Errors[0].Error(), "start_time")
}
