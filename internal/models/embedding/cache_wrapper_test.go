package embedding

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testModelFingerprint = types.HashEmbeddingText("test-provider-config")

func TestCachedEmbedderReusesSameTextAndPreservesBatchOrder(t *testing.T) {
	inner := &countingEmbedder{id: "embed-a", dimensions: 2}
	cache := &memoryEmbeddingCache{values: map[types.EmbeddingCacheKey][]float32{}}
	recorder := &memoryCallRecorder{}
	embedder := NewCachedEmbedder(inner, 7, cache, recorder, types.ModelPricing{}, testModelFingerprint)

	first, err := embedder.BatchEmbed(context.Background(), []string{"alpha", "beta"})
	require.NoError(t, err)
	second, err := embedder.BatchEmbed(context.Background(), []string{"beta", "alpha"})
	require.NoError(t, err)

	assert.Equal(t, [][]float32{{5, 1}, {4, 2}}, first)
	assert.Equal(t, [][]float32{{4, 2}, {5, 1}}, second)
	assert.Equal(t, 1, inner.batchCalls)
	require.Len(t, recorder.records, 2)
	assert.Equal(t, 2, recorder.records[1].CacheHitItems)
	assert.Equal(t, 0, recorder.records[1].CacheMissItems)
}

func TestCachedEmbedderSingleflightsConcurrentMisses(t *testing.T) {
	inner := &countingEmbedder{id: "embed-a", dimensions: 2}
	cache := &memoryEmbeddingCache{values: map[types.EmbeddingCacheKey][]float32{}}
	embedder := NewCachedEmbedder(
		inner, 7, cache, &memoryCallRecorder{}, types.ModelPricing{}, testModelFingerprint,
	)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := embedder.Embed(context.Background(), "same")
			require.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, inner.embedCalls)
}

func TestCachedEmbedderSeparatesProviderConfigurationFingerprints(t *testing.T) {
	inner := &countingEmbedder{id: "embed-a", dimensions: 2}
	cache := &memoryEmbeddingCache{values: map[types.EmbeddingCacheKey][]float32{}}
	firstConfig := NewCachedEmbedder(
		inner, 7, cache, &memoryCallRecorder{}, types.ModelPricing{}, types.HashEmbeddingText("config-a"),
	)
	secondConfig := NewCachedEmbedder(
		inner, 7, cache, &memoryCallRecorder{}, types.ModelPricing{}, types.HashEmbeddingText("config-b"),
	)

	_, err := firstConfig.Embed(context.Background(), "same")
	require.NoError(t, err)
	_, err = secondConfig.Embed(context.Background(), "same")
	require.NoError(t, err)

	assert.Equal(t, 2, inner.embedCalls)
}

func TestCachedEmbedderCoalescesOverlappingBatchMisses(t *testing.T) {
	inner := &countingEmbedder{
		id: "embed-a", dimensions: 2,
		batchStarted: make(chan struct{}), batchRelease: make(chan struct{}),
	}
	cache := &memoryEmbeddingCache{values: map[types.EmbeddingCacheKey][]float32{}}
	embedders := []Embedder{
		NewCachedEmbedder(inner, 7, cache, &memoryCallRecorder{}, types.ModelPricing{}, testModelFingerprint),
		NewCachedEmbedder(inner, 7, cache, &memoryCallRecorder{}, types.ModelPricing{}, testModelFingerprint),
	}

	var wg sync.WaitGroup
	for _, embedder := range embedders {
		embedder := embedder
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := embedder.BatchEmbed(context.Background(), []string{"same"})
			require.NoError(t, err)
		}()
		if inner.batchStarted != nil {
			select {
			case <-inner.batchStarted:
			case <-time.After(time.Second):
				t.Fatal("first batch call did not start")
			}
		}
	}
	// Give the second goroutine time to observe the same initial miss while the
	// first provider request is held open.
	time.Sleep(20 * time.Millisecond)
	close(inner.batchRelease)
	wg.Wait()

	assert.Equal(t, 1, inner.batchCalls)
}

func TestObservedEmbedderRecordsWithoutCaching(t *testing.T) {
	inner := &countingEmbedder{id: "embed-observed", dimensions: 2}
	recorder := &memoryCallRecorder{}
	observed := NewObservedEmbedder(inner, 7, recorder, types.ModelPricing{})
	ctx := types.WithLLMCallMetadata(context.Background(), "model_connection_test", "")

	_, err := observed.Embed(ctx, "hello")
	require.NoError(t, err)
	_, err = observed.Embed(ctx, "hello")
	require.NoError(t, err)

	assert.Equal(t, 2, inner.embedCalls, "connectivity checks must always call the provider")
	require.Len(t, recorder.records, 2)
	assert.Equal(t, "model_connection_test", recorder.records[0].Purpose)
	assert.Equal(t, 1, recorder.records[0].CacheMissItems)
}

type countingEmbedder struct {
	mu           sync.Mutex
	id           string
	dimensions   int
	embedCalls   int
	batchCalls   int
	batchStarted chan struct{}
	batchRelease chan struct{}
	startOnce    sync.Once
}

func (e *countingEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.embedCalls++
	return []float32{float32(len(text)), float32(e.embedCalls)}, nil
}

func (e *countingEmbedder) BatchEmbed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.batchCalls++
	if e.batchStarted != nil {
		e.startOnce.Do(func() { close(e.batchStarted) })
		<-e.batchRelease
	}
	result := make([][]float32, len(texts))
	for i, text := range texts {
		result[i] = []float32{float32(len(text)), float32(i + 1)}
	}
	return result, nil
}

func (e *countingEmbedder) BatchEmbedWithPool(ctx context.Context, _ Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}

func (e *countingEmbedder) GetModelName() string { return e.id }
func (e *countingEmbedder) GetDimensions() int   { return e.dimensions }
func (e *countingEmbedder) GetModelID() string   { return e.id }

type memoryEmbeddingCache struct {
	mu     sync.Mutex
	values map[types.EmbeddingCacheKey][]float32
}

func (c *memoryEmbeddingCache) Get(_ context.Context, key types.EmbeddingCacheKey) ([]float32, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, ok := c.values[key]
	return append([]float32(nil), value...), ok, nil
}

func (c *memoryEmbeddingCache) Put(_ context.Context, key types.EmbeddingCacheKey, vector []float32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(vector) == 0 {
		return fmt.Errorf("empty vector")
	}
	c.values[key] = append([]float32(nil), vector...)
	return nil
}

type memoryCallRecorder struct {
	mu      sync.Mutex
	records []*types.ModelCallRecord
}

func (r *memoryCallRecorder) Record(_ context.Context, record *types.ModelCallRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *record
	r.records = append(r.records, &copy)
	return nil
}
