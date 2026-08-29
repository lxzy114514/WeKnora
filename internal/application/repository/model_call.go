package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type modelCallRow struct {
	ID                      string          `gorm:"type:varchar(36);primaryKey"`
	TenantID                uint64          `gorm:"not null;index:idx_model_calls_tenant_created,priority:1"`
	ModelID                 string          `gorm:"type:varchar(64);not null;index"`
	ModelType               types.ModelType `gorm:"type:varchar(32);not null"`
	Purpose                 string          `gorm:"type:varchar(96);index"`
	CorrelationID           string          `gorm:"type:varchar(128);index"`
	PromptPrefixFingerprint string          `gorm:"type:varchar(64)"`
	PromptTokens            int
	CompletionTokens        int
	TotalTokens             int
	CacheReadTokens         int
	CacheWriteTokens        int
	CacheMissTokens         int
	InputUnits              int
	ItemCount               int
	CacheHitItems           int
	CacheMissItems          int
	DurationMS              int64
	Success                 bool       `gorm:"not null"`
	ErrorCode               string     `gorm:"type:varchar(96)"`
	Pricing                 types.JSON `gorm:"type:json"`
	Cost                    float64    `gorm:"type:decimal(20,10);not null"`
	CostEstimated           bool       `gorm:"not null"`
	CreatedAt               time.Time  `gorm:"not null;index:idx_model_calls_tenant_created,priority:2,sort:desc"`
}

func (modelCallRow) TableName() string { return "model_calls" }

type modelCallRepository struct{ db *gorm.DB }

func NewModelCallRepository(db *gorm.DB) interfaces.ModelCallRepository {
	return &modelCallRepository{db: db}
}

func (r *modelCallRepository) Record(ctx context.Context, record *types.ModelCallRecord) error {
	if record == nil || record.TenantID == 0 || record.ModelID == "" || record.ModelType == "" {
		return errors.New("model call: tenant, model ID, and model type are required")
	}
	if record.ID == "" {
		record.ID = uuid.New().String()
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if record.Pricing.Currency == "" {
		record.Pricing.Currency = "USD"
	}
	if err := record.Pricing.NormalizeAndValidate(); err != nil {
		return err
	}
	record.Cost = types.EstimateModelCallCost(record)
	record.CostEstimated = true
	pricing, err := json.Marshal(record.Pricing)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&modelCallRow{
		ID: record.ID, TenantID: record.TenantID, ModelID: record.ModelID,
		ModelType: record.ModelType, Purpose: record.Purpose, CorrelationID: record.CorrelationID,
		PromptPrefixFingerprint: record.PromptPrefixFingerprint, PromptTokens: record.PromptTokens,
		CompletionTokens: record.CompletionTokens, TotalTokens: record.TotalTokens,
		CacheReadTokens: record.CacheReadTokens, CacheWriteTokens: record.CacheWriteTokens,
		CacheMissTokens: record.CacheMissTokens, InputUnits: record.InputUnits,
		ItemCount: record.ItemCount, CacheHitItems: record.CacheHitItems, CacheMissItems: record.CacheMissItems,
		DurationMS: record.DurationMS, Success: record.Success, ErrorCode: record.ErrorCode,
		Pricing: types.JSON(pricing), Cost: record.Cost, CostEstimated: record.CostEstimated,
		CreatedAt: record.CreatedAt,
	}).Error
}

func (r *modelCallRepository) Summary(
	ctx context.Context, query types.ModelUsageQuery,
) (*types.ModelUsageSummary, error) {
	if query.TenantID == 0 {
		return nil, errors.New("model usage: tenant ID is required")
	}
	db := r.scoped(ctx, query)
	var rows []modelUsageAggregate
	if err := db.Select(`
		model_id,
		model_type,
		COUNT(*) AS call_count,
		COALESCE(SUM(CASE WHEN success THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens,
		COALESCE(SUM(completion_tokens), 0) AS completion_tokens,
		COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
		COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
		COALESCE(SUM(cache_miss_tokens), 0) AS cache_miss_tokens,
		COALESCE(SUM(input_units), 0) AS input_units,
		COALESCE(SUM(cache_hit_items), 0) AS cache_hit_items,
		COALESCE(SUM(cache_miss_items), 0) AS cache_miss_items,
		COALESCE(SUM(duration_ms), 0) AS duration_ms,
		COALESCE(SUM(cost), 0) AS total_cost
	`).Group("model_id, model_type").Order("model_id ASC, model_type ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := &types.ModelUsageSummary{Currency: "USD"}
	for _, row := range rows {
		item := types.ModelUsageBreakdown{
			ModelID: row.ModelID, ModelType: row.ModelType, CallCount: row.CallCount,
			SuccessCount: row.SuccessCount, PromptTokens: row.PromptTokens,
			CompletionTokens: row.CompletionTokens, CacheReadTokens: row.CacheReadTokens,
			CacheWriteTokens: row.CacheWriteTokens, CacheMissTokens: row.CacheMissTokens,
			InputUnits: row.InputUnits, CacheHitItems: row.CacheHitItems,
			CacheMissItems: row.CacheMissItems, DurationMS: row.DurationMS, TotalCost: row.TotalCost,
		}
		result.ByModel = append(result.ByModel, item)
		accumulateBreakdown(result, item)
	}
	return result, nil
}

type modelUsageAggregate struct {
	ModelID          string
	ModelType        types.ModelType
	CallCount        int64
	SuccessCount     int64
	PromptTokens     int64
	CompletionTokens int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CacheMissTokens  int64
	InputUnits       int64
	CacheHitItems    int64
	CacheMissItems   int64
	DurationMS       int64
	TotalCost        float64
}

func (r *modelCallRepository) ListByCorrelation(
	ctx context.Context, tenantID uint64, correlationID string,
) ([]*types.ModelCallRecord, error) {
	if tenantID == 0 || correlationID == "" {
		return nil, errors.New("model calls: tenant and correlation ID are required")
	}
	var rows []modelCallRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND correlation_id = ?", tenantID, correlationID).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*types.ModelCallRecord, 0, len(rows))
	for _, row := range rows {
		record := row.record()
		result = append(result, record)
	}
	return result, nil
}

func (r *modelCallRepository) scoped(ctx context.Context, query types.ModelUsageQuery) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&modelCallRow{}).Where("tenant_id = ?", query.TenantID)
	if query.ModelID != "" {
		db = db.Where("model_id = ?", query.ModelID)
	}
	if query.Purpose != "" {
		db = db.Where("purpose = ?", query.Purpose)
	}
	if !query.StartTime.IsZero() {
		db = db.Where("created_at >= ?", query.StartTime)
	}
	if !query.EndTime.IsZero() {
		db = db.Where("created_at <= ?", query.EndTime)
	}
	return db
}

func accumulateBreakdown(summary *types.ModelUsageSummary, item types.ModelUsageBreakdown) {
	summary.CallCount += item.CallCount
	summary.SuccessCount += item.SuccessCount
	summary.PromptTokens += item.PromptTokens
	summary.CompletionTokens += item.CompletionTokens
	summary.CacheReadTokens += item.CacheReadTokens
	summary.CacheWriteTokens += item.CacheWriteTokens
	summary.CacheMissTokens += item.CacheMissTokens
	summary.InputUnits += item.InputUnits
	summary.CacheHitItems += item.CacheHitItems
	summary.CacheMissItems += item.CacheMissItems
	summary.DurationMS += item.DurationMS
	summary.TotalCost += item.TotalCost
}

func (row modelCallRow) record() *types.ModelCallRecord {
	record := &types.ModelCallRecord{
		ID: row.ID, TenantID: row.TenantID, ModelID: row.ModelID, ModelType: row.ModelType,
		Purpose: row.Purpose, CorrelationID: row.CorrelationID,
		PromptPrefixFingerprint: row.PromptPrefixFingerprint, PromptTokens: row.PromptTokens,
		CompletionTokens: row.CompletionTokens, TotalTokens: row.TotalTokens,
		CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
		CacheMissTokens: row.CacheMissTokens, InputUnits: row.InputUnits, ItemCount: row.ItemCount,
		CacheHitItems: row.CacheHitItems, CacheMissItems: row.CacheMissItems,
		DurationMS: row.DurationMS, Success: row.Success, ErrorCode: row.ErrorCode,
		Cost: row.Cost, CostEstimated: row.CostEstimated, CreatedAt: row.CreatedAt,
	}
	_ = json.Unmarshal(row.Pricing, &record.Pricing)
	return record
}
