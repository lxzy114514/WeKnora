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
	"gorm.io/gorm/clause"
)

type embeddingCacheRow struct {
	ID               string     `gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64     `gorm:"not null;uniqueIndex:uq_embedding_cache_key,priority:1"`
	ModelID          string     `gorm:"type:varchar(64);not null;uniqueIndex:uq_embedding_cache_key,priority:2"`
	Dimensions       int        `gorm:"not null;uniqueIndex:uq_embedding_cache_key,priority:3"`
	ModelFingerprint string     `gorm:"type:char(64);not null;uniqueIndex:uq_embedding_cache_key,priority:4"`
	ContentHash      string     `gorm:"type:char(64);not null;uniqueIndex:uq_embedding_cache_key,priority:5"`
	Vector           types.JSON `gorm:"type:json;not null"`
	HitCount         int64      `gorm:"not null;default:0"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
	AccessedAt       time.Time `gorm:"not null;index"`
}

func (embeddingCacheRow) TableName() string { return "embedding_cache" }

type embeddingCacheRepository struct{ db *gorm.DB }

func NewEmbeddingCacheRepository(db *gorm.DB) interfaces.EmbeddingCacheRepository {
	return &embeddingCacheRepository{db: db}
}

func (r *embeddingCacheRepository) Get(
	ctx context.Context, key types.EmbeddingCacheKey,
) ([]float32, bool, error) {
	if err := validateEmbeddingCacheKey(key); err != nil {
		return nil, false, err
	}
	var row embeddingCacheRow
	err := r.db.WithContext(ctx).Where(
		"tenant_id = ? AND model_id = ? AND dimensions = ? AND model_fingerprint = ? AND content_hash = ?",
		key.TenantID, key.ModelID, key.Dimensions, key.ModelFingerprint, key.ContentHash,
	).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var vector []float32
	if err := json.Unmarshal(row.Vector, &vector); err != nil {
		return nil, false, err
	}
	if len(vector) != key.Dimensions {
		return nil, false, errors.New("embedding cache: stored vector dimension mismatch")
	}
	now := time.Now()
	if err := r.db.WithContext(ctx).Model(&embeddingCacheRow{}).Where("id = ?", row.ID).
		Updates(map[string]interface{}{
			"hit_count": gorm.Expr("hit_count + 1"), "accessed_at": now, "updated_at": now,
		}).Error; err != nil {
		// Access statistics are observational only. A transient lock or an
		// expiring request context must not turn a valid cached vector into an
		// expensive provider miss.
		return vector, true, nil
	}
	return vector, true, nil
}

func (r *embeddingCacheRepository) Put(
	ctx context.Context, key types.EmbeddingCacheKey, vector []float32,
) error {
	if err := validateEmbeddingCacheKey(key); err != nil {
		return err
	}
	if len(vector) != key.Dimensions {
		return errors.New("embedding cache: vector dimension mismatch")
	}
	data, err := json.Marshal(vector)
	if err != nil {
		return err
	}
	now := time.Now()
	row := embeddingCacheRow{
		ID: uuid.New().String(), TenantID: key.TenantID, ModelID: key.ModelID,
		Dimensions: key.Dimensions, ModelFingerprint: key.ModelFingerprint,
		ContentHash: key.ContentHash, Vector: types.JSON(data),
		AccessedAt: now,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "tenant_id"}, {Name: "model_id"}, {Name: "dimensions"},
			{Name: "model_fingerprint"}, {Name: "content_hash"},
		},
		DoNothing: true,
	}).Create(&row).Error
}

func (r *embeddingCacheRepository) InvalidateModel(ctx context.Context, modelID string) error {
	if modelID == "" {
		return errors.New("embedding cache: model ID is required")
	}
	return r.db.WithContext(ctx).Where("model_id = ?", modelID).Delete(&embeddingCacheRow{}).Error
}

func validateEmbeddingCacheKey(key types.EmbeddingCacheKey) error {
	if key.TenantID == 0 || key.ModelID == "" || key.Dimensions <= 0 ||
		len(key.ModelFingerprint) != 64 || len(key.ContentHash) != 64 {
		return errors.New("embedding cache: invalid key")
	}
	return nil
}
