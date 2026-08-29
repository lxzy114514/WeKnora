package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type evaluationRunRow struct {
	ID        string    `gorm:"type:varchar(128);primaryKey"`
	TenantID  uint64    `gorm:"not null;index:idx_evaluation_runs_tenant_started,priority:1"`
	DatasetID string    `gorm:"type:varchar(128);not null"`
	StartTime time.Time `gorm:"not null;index:idx_evaluation_runs_tenant_started,priority:2,sort:desc"`
	EndTime   *time.Time
	Status    types.EvaluationStatue `gorm:"not null"`
	ErrMsg    string                 `gorm:"type:text"`
	Total     int
	Finished  int
	Params    types.JSON `gorm:"type:json"`
	Config    types.JSON `gorm:"type:json"`
	Metric    types.JSON `gorm:"type:json"`
	Cost      types.JSON `gorm:"type:json"`
	Duration  types.JSON `gorm:"type:json"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (evaluationRunRow) TableName() string { return "evaluation_runs" }

type evaluationCaseRow struct {
	TaskID      string     `gorm:"type:varchar(128);primaryKey"`
	TenantID    uint64     `gorm:"not null;index"`
	QID         int        `gorm:"column:qid;primaryKey"`
	DurationMS  int64      `gorm:"not null"`
	Stages      types.JSON `gorm:"type:json"`
	Retrieved   types.JSON `gorm:"type:json"`
	Reranked    types.JSON `gorm:"type:json"`
	FinalAnswer string     `gorm:"type:text"`
	Metric      types.JSON `gorm:"type:json"`
	ErrMsg      string     `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (evaluationCaseRow) TableName() string { return "evaluation_cases" }

type evaluationRepository struct{ db *gorm.DB }

func NewEvaluationRepository(db *gorm.DB) interfaces.EvaluationRepository {
	return &evaluationRepository{db: db}
}

func (r *evaluationRepository) Create(ctx context.Context, detail *types.EvaluationDetail) error {
	row, err := evaluationRowFromDetail(detail)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *evaluationRepository) Update(ctx context.Context, detail *types.EvaluationDetail) error {
	row, err := evaluationRowFromDetail(detail)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Model(&evaluationRunRow{}).
		Where("id = ? AND tenant_id = ?", row.ID, row.TenantID).
		Updates(map[string]interface{}{
			"dataset_id": row.DatasetID, "start_time": row.StartTime, "end_time": row.EndTime,
			"status": row.Status, "err_msg": row.ErrMsg, "total": row.Total,
			"finished": row.Finished, "params": row.Params, "config": row.Config,
			"metric": row.Metric, "cost": row.Cost, "duration": row.Duration,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("evaluation run not found")
	}
	return nil
}

func (r *evaluationRepository) Get(ctx context.Context, tenantID uint64, taskID string) (*types.EvaluationDetail, error) {
	var row evaluationRunRow
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, taskID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.detail()
}

func (r *evaluationRepository) List(
	ctx context.Context, tenantID uint64, limit, offset int,
) ([]*types.EvaluationDetail, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var rows []evaluationRunRow
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).
		Order("start_time DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, err
	}
	results := make([]*types.EvaluationDetail, 0, len(rows))
	for i := range rows {
		detail, err := rows[i].detail()
		if err != nil {
			return nil, err
		}
		results = append(results, detail)
	}
	return results, nil
}

func (r *evaluationRepository) UpsertCase(ctx context.Context, result *types.EvaluationCaseResult) error {
	if result == nil || result.TaskID == "" || result.TenantID == 0 {
		return errors.New("evaluation case: task ID and tenant ID are required")
	}
	metric, err := marshalJSON(result.Metric)
	if err != nil {
		return err
	}
	stages, err := marshalJSON(result.Stages)
	if err != nil {
		return err
	}
	retrieved, err := marshalJSON(result.RetrievedPIDs)
	if err != nil {
		return err
	}
	reranked, err := marshalJSON(result.RerankedPIDs)
	if err != nil {
		return err
	}
	row := evaluationCaseRow{
		TaskID: result.TaskID, TenantID: result.TenantID, QID: result.QID,
		DurationMS: result.DurationMS, Stages: stages, Retrieved: retrieved,
		Reranked: reranked, FinalAnswer: result.FinalAnswer, Metric: metric, ErrMsg: result.Error,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "qid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"tenant_id", "duration_ms", "stages", "retrieved", "reranked",
			"final_answer", "metric", "err_msg", "updated_at",
		}),
	}).Create(&row).Error
}

func (r *evaluationRepository) ListCases(
	ctx context.Context, tenantID uint64, taskID string, limit, offset int,
) ([]*types.EvaluationCaseResult, error) {
	var rows []evaluationCaseRow
	query := r.db.WithContext(ctx).Where("tenant_id = ? AND task_id = ?", tenantID, taskID).
		Order("qid ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	results := make([]*types.EvaluationCaseResult, 0, len(rows))
	for _, row := range rows {
		result := &types.EvaluationCaseResult{
			TaskID: row.TaskID, TenantID: row.TenantID, QID: row.QID,
			DurationMS: row.DurationMS, FinalAnswer: row.FinalAnswer, Error: row.ErrMsg,
		}
		if err := unmarshalJSON(row.Stages, &result.Stages); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(row.Retrieved, &result.RetrievedPIDs); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(row.Reranked, &result.RerankedPIDs); err != nil {
			return nil, err
		}
		if err := unmarshalJSON(row.Metric, &result.Metric); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (r *evaluationRepository) RecoverInterrupted(ctx context.Context, reason string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&evaluationRunRow{}).
		Where("status IN ?", []types.EvaluationStatue{types.EvaluationStatuePending, types.EvaluationStatueRunning}).
		Updates(map[string]interface{}{
			"status": types.EvaluationStatueFailed, "err_msg": reason,
			"end_time": now, "updated_at": now,
		}).Error
}

func evaluationRowFromDetail(detail *types.EvaluationDetail) (*evaluationRunRow, error) {
	if detail == nil || detail.Task == nil || detail.Task.ID == "" || detail.Task.TenantID == 0 {
		return nil, errors.New("evaluation run: task ID and tenant ID are required")
	}
	params, err := marshalJSON(detail.Params)
	if err != nil {
		return nil, err
	}
	config, err := marshalJSON(detail.Config)
	if err != nil {
		return nil, err
	}
	metric, err := marshalJSON(detail.Metric)
	if err != nil {
		return nil, err
	}
	cost, err := marshalJSON(detail.Cost)
	if err != nil {
		return nil, err
	}
	duration, err := marshalJSON(detail.Duration)
	if err != nil {
		return nil, err
	}
	return &evaluationRunRow{
		ID: detail.Task.ID, TenantID: detail.Task.TenantID, DatasetID: detail.Task.DatasetID,
		StartTime: detail.Task.StartTime, EndTime: detail.Task.EndTime, Status: detail.Task.Status,
		ErrMsg: detail.Task.ErrMsg, Total: detail.Task.Total, Finished: detail.Task.Finished,
		Params: params, Config: config, Metric: metric, Cost: cost, Duration: duration,
	}, nil
}

func (row *evaluationRunRow) detail() (*types.EvaluationDetail, error) {
	detail := &types.EvaluationDetail{Task: &types.EvaluationTask{
		ID: row.ID, TenantID: row.TenantID, DatasetID: row.DatasetID,
		StartTime: row.StartTime, EndTime: row.EndTime, Status: row.Status,
		ErrMsg: row.ErrMsg, Total: row.Total, Finished: row.Finished,
	}}
	for _, item := range []struct {
		data types.JSON
		out  interface{}
	}{
		{row.Params, &detail.Params}, {row.Config, &detail.Config}, {row.Metric, &detail.Metric},
		{row.Cost, &detail.Cost}, {row.Duration, &detail.Duration},
	} {
		if err := unmarshalJSON(item.data, item.out); err != nil {
			return nil, fmt.Errorf("decode evaluation run %s: %w", row.ID, err)
		}
	}
	return detail, nil
}

func marshalJSON(value interface{}) (types.JSON, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	return types.JSON(data), err
}

func unmarshalJSON(data types.JSON, target interface{}) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	return json.Unmarshal(data, target)
}
