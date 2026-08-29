package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEmbeddingCacheRepositoryUsesTenantModelConfigDimensionAndHashKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:embedding-cache?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&embeddingCacheRow{}))
	repo := NewEmbeddingCacheRepository(db)
	ctx := context.Background()
	key := types.EmbeddingCacheKey{
		TenantID: 7, ModelID: "embed-a", Dimensions: 3,
		ModelFingerprint: types.HashEmbeddingText("provider-config-a"),
		ContentHash:      types.HashEmbeddingText("hello"),
	}

	_, found, err := repo.Get(ctx, key)
	require.NoError(t, err)
	assert.False(t, found)

	require.NoError(t, repo.Put(ctx, key, []float32{1, 2, 3}))
	vector, found, err := repo.Get(ctx, key)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []float32{1, 2, 3}, vector)

	otherTenant := key
	otherTenant.TenantID = 8
	_, found, err = repo.Get(ctx, otherTenant)
	require.NoError(t, err)
	assert.False(t, found)

	otherConfig := key
	otherConfig.ModelFingerprint = types.HashEmbeddingText("provider-config-b")
	_, found, err = repo.Get(ctx, otherConfig)
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(t, repo.Put(ctx, otherConfig, []float32{4, 5, 6}))

	vector, found, err = repo.Get(ctx, key)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []float32{1, 2, 3}, vector)

	var row embeddingCacheRow
	require.NoError(t, db.First(&row).Error)
	assert.NotContains(t, row.ContentHash, "hello")
	assert.NotEmpty(t, row.ModelFingerprint)

	require.NoError(t, repo.InvalidateModel(ctx, key.ModelID))
	_, found, err = repo.Get(ctx, key)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestEmbeddingCacheRepositoryReturnsHitWhenAccessStatsUpdateFails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:embedding-cache-stats?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&embeddingCacheRow{}))
	repo := NewEmbeddingCacheRepository(db)
	ctx := context.Background()
	key := types.EmbeddingCacheKey{
		TenantID: 7, ModelID: "embed-a", Dimensions: 2,
		ModelFingerprint: types.HashEmbeddingText("provider-config"),
		ContentHash:      types.HashEmbeddingText("hello"),
	}
	require.NoError(t, repo.Put(ctx, key, []float32{1, 2}))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(
		"test:fail_embedding_cache_stats", func(tx *gorm.DB) {
			tx.AddError(errors.New("forced access stats failure"))
		},
	))

	vector, found, err := repo.Get(ctx, key)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, []float32{1, 2}, vector)
}
