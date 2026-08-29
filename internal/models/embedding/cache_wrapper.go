package embedding

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

type CacheStore interface {
	Get(ctx context.Context, key types.EmbeddingCacheKey) ([]float32, bool, error)
	Put(ctx context.Context, key types.EmbeddingCacheKey, vector []float32) error
}

type CallRecorder interface {
	Record(ctx context.Context, record *types.ModelCallRecord) error
}

type cachedEmbedder struct {
	inner            Embedder
	tenantID         uint64
	cache            CacheStore
	recorder         CallRecorder
	pricing          types.ModelPricing
	modelFingerprint string
}

type observedEmbedder struct {
	inner    Embedder
	tenantID uint64
	recorder CallRecorder
	pricing  types.ModelPricing
}

type embeddingKeyLock struct {
	mu   sync.Mutex
	refs int
}

type embeddingKeyLockRegistry struct {
	mu    sync.Mutex
	locks map[string]*embeddingKeyLock
}

// ModelService creates lightweight embedder instances on demand. The lock
// registry must therefore be process-wide, otherwise two requests holding
// different wrappers could still duplicate the same provider call.
var processEmbeddingLocks = embeddingKeyLockRegistry{locks: make(map[string]*embeddingKeyLock)}

func NewCachedEmbedder(
	inner Embedder,
	tenantID uint64,
	cache CacheStore,
	recorder CallRecorder,
	pricing types.ModelPricing,
	modelFingerprint string,
) Embedder {
	if inner == nil || cache == nil {
		return inner
	}
	return &cachedEmbedder{
		inner: inner, tenantID: tenantID, cache: cache, recorder: recorder,
		pricing: pricing, modelFingerprint: modelFingerprint,
	}
}

// NewObservedEmbedder records provider calls without serving cached vectors.
// Connectivity checks use this path so a previous cache hit cannot mask a
// provider outage while the call still participates in unified telemetry.
func NewObservedEmbedder(
	inner Embedder, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing,
) Embedder {
	if inner == nil || recorder == nil {
		return inner
	}
	return &observedEmbedder{inner: inner, tenantID: tenantID, recorder: recorder, pricing: pricing}
}

func (w *observedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	started := time.Now()
	vector, err := w.inner.Embed(ctx, text)
	recordEmbeddingCall(ctx, w.inner, w.tenantID, w.recorder, w.pricing,
		started, 1, 0, 1, estimateEmbeddingTokens(text), err)
	return vector, err
}

func (w *observedEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	started := time.Now()
	vectors, err := w.inner.BatchEmbed(ctx, texts)
	recordEmbeddingCall(ctx, w.inner, w.tenantID, w.recorder, w.pricing,
		started, len(texts), 0, len(texts), embeddingInputUnits(texts), err)
	return vectors, err
}

func (w *observedEmbedder) BatchEmbedWithPool(
	ctx context.Context, embedder Embedder, texts []string,
) ([][]float32, error) {
	started := time.Now()
	vectors, err := w.inner.BatchEmbedWithPool(ctx, embedder, texts)
	recordEmbeddingCall(ctx, w.inner, w.tenantID, w.recorder, w.pricing,
		started, len(texts), 0, len(texts), embeddingInputUnits(texts), err)
	return vectors, err
}

func (w *observedEmbedder) GetModelName() string { return w.inner.GetModelName() }
func (w *observedEmbedder) GetDimensions() int   { return w.inner.GetDimensions() }
func (w *observedEmbedder) GetModelID() string   { return w.inner.GetModelID() }

func (w *cachedEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	started := time.Now()
	key := w.key(text)
	if vector, found, err := w.cache.Get(ctx, key); err == nil && found {
		w.record(ctx, started, 1, 1, 0, 0, nil)
		return vector, nil
	} else if err != nil {
		logger.Warnf(ctx, "embedding cache read failed: %v", err)
	}

	unlock := w.lockBatchKeys([]string{w.singleflightKey(key)})
	defer unlock()
	if vector, found, cacheErr := w.cache.Get(ctx, key); cacheErr == nil && found {
		w.record(ctx, started, 1, 1, 0, 0, nil)
		return vector, nil
	} else if cacheErr != nil {
		logger.Warnf(ctx, "embedding cache recheck failed: %v", cacheErr)
	}
	vector, err := w.inner.Embed(ctx, text)
	inputUnits := estimateEmbeddingTokens(text)
	w.record(ctx, started, 1, 0, 1, inputUnits, err)
	if err != nil {
		return nil, err
	}
	if cacheErr := w.cache.Put(ctx, key, vector); cacheErr != nil {
		logger.Warnf(ctx, "embedding cache write failed: %v", cacheErr)
	}
	return vector, nil
}

func (w *cachedEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	return w.batch(ctx, texts, false)
}

func (w *cachedEmbedder) BatchEmbedWithPool(
	ctx context.Context, _ Embedder, texts []string,
) ([][]float32, error) {
	return w.batch(ctx, texts, true)
}

func (w *cachedEmbedder) batch(ctx context.Context, texts []string, withPool bool) ([][]float32, error) {
	started := time.Now()
	results := make([][]float32, len(texts))
	type missingText struct {
		text    string
		key     types.EmbeddingCacheKey
		indices []int
	}
	missingByHash := make(map[string]*missingText)
	hits := 0
	for index, text := range texts {
		key := w.key(text)
		vector, found, err := w.cache.Get(ctx, key)
		if err != nil {
			logger.Warnf(ctx, "embedding cache read failed: %v", err)
		}
		if found {
			results[index] = vector
			hits++
			continue
		}
		item := missingByHash[key.ContentHash]
		if item == nil {
			item = &missingText{text: text, key: key}
			missingByHash[key.ContentHash] = item
		}
		item.indices = append(item.indices, index)
	}

	// Preserve first-occurrence order even though the dedupe index is a map.
	missing := make([]*missingText, 0, len(missingByHash))
	seen := make(map[string]struct{}, len(missingByHash))
	for _, text := range texts {
		hash := types.HashEmbeddingText(text)
		if _, ok := seen[hash]; ok {
			continue
		}
		if item := missingByHash[hash]; item != nil {
			seen[hash] = struct{}{}
			missing = append(missing, item)
		}
	}

	if len(missing) == 0 {
		w.record(ctx, started, len(texts), hits, 0, 0, nil)
		return results, nil
	}

	// Lock all absent content keys in a stable order, then recheck the durable
	// cache. This preserves provider batching while preventing overlapping batch
	// requests from embedding the same text concurrently.
	lockKeys := make([]string, len(missing))
	for i, item := range missing {
		lockKeys[i] = w.singleflightKey(item.key)
	}
	unlock := w.lockBatchKeys(lockKeys)
	defer unlock()
	stillMissing := make([]*missingText, 0, len(missing))
	for _, item := range missing {
		vector, found, err := w.cache.Get(ctx, item.key)
		if err != nil {
			logger.Warnf(ctx, "embedding cache recheck failed: %v", err)
		}
		if !found {
			stillMissing = append(stillMissing, item)
			continue
		}
		for _, index := range item.indices {
			results[index] = vector
		}
		hits += len(item.indices)
	}
	missing = stillMissing
	if len(missing) == 0 {
		w.record(ctx, started, len(texts), hits, 0, 0, nil)
		return results, nil
	}
	missingTexts := make([]string, len(missing))
	inputUnits := 0
	missItems := 0
	for i, item := range missing {
		missingTexts[i] = item.text
		inputUnits += estimateEmbeddingTokens(item.text)
		missItems += len(item.indices)
	}
	var (
		vectors [][]float32
		err     error
	)
	if withPool {
		vectors, err = w.inner.BatchEmbedWithPool(ctx, w.inner, missingTexts)
	} else {
		vectors, err = w.inner.BatchEmbed(ctx, missingTexts)
	}
	if err == nil && len(vectors) != len(missing) {
		err = fmt.Errorf("embedding model returned %d embeddings for %d inputs", len(vectors), len(missing))
	}
	if err != nil {
		w.record(ctx, started, len(texts), hits, missItems, inputUnits, err)
		return nil, err
	}
	for i, item := range missing {
		vector := vectors[i]
		for _, index := range item.indices {
			results[index] = vector
		}
		if cacheErr := w.cache.Put(ctx, item.key, vector); cacheErr != nil {
			logger.Warnf(ctx, "embedding cache write failed: %v", cacheErr)
		}
	}
	w.record(ctx, started, len(texts), hits, missItems, inputUnits, nil)
	return results, nil
}

func (w *cachedEmbedder) key(text string) types.EmbeddingCacheKey {
	return types.EmbeddingCacheKey{
		TenantID: w.tenantID, ModelID: w.inner.GetModelID(),
		Dimensions: w.inner.GetDimensions(), ModelFingerprint: w.modelFingerprint,
		ContentHash: types.HashEmbeddingText(text),
	}
}

func (w *cachedEmbedder) singleflightKey(key types.EmbeddingCacheKey) string {
	return fmt.Sprintf("%d:%s:%d:%s:%s",
		key.TenantID, key.ModelID, key.Dimensions, key.ModelFingerprint, key.ContentHash)
}

func (w *cachedEmbedder) lockBatchKeys(keys []string) func() {
	sort.Strings(keys)
	processEmbeddingLocks.mu.Lock()
	locks := make([]*embeddingKeyLock, 0, len(keys))
	for _, key := range keys {
		lock := processEmbeddingLocks.locks[key]
		if lock == nil {
			lock = &embeddingKeyLock{}
			processEmbeddingLocks.locks[key] = lock
		}
		lock.refs++
		locks = append(locks, lock)
	}
	processEmbeddingLocks.mu.Unlock()
	for _, lock := range locks {
		lock.mu.Lock()
	}
	return func() {
		for i := len(locks) - 1; i >= 0; i-- {
			locks[i].mu.Unlock()
		}
		processEmbeddingLocks.mu.Lock()
		for i, key := range keys {
			locks[i].refs--
			if locks[i].refs == 0 {
				delete(processEmbeddingLocks.locks, key)
			}
		}
		processEmbeddingLocks.mu.Unlock()
	}
}

func (w *cachedEmbedder) record(
	ctx context.Context, started time.Time, items, hits, misses, inputUnits int, callErr error,
) {
	recordEmbeddingCall(ctx, w.inner, w.tenantID, w.recorder, w.pricing,
		started, items, hits, misses, inputUnits, callErr)
}

func recordEmbeddingCall(
	ctx context.Context, inner Embedder, tenantID uint64, recorder CallRecorder, pricing types.ModelPricing,
	started time.Time, items, hits, misses, inputUnits int, callErr error,
) {
	if recorder == nil {
		return
	}
	purpose, prefix := types.LLMCallMetadataFromContext(ctx)
	if purpose == "" {
		purpose = "embedding"
	}
	record := &types.ModelCallRecord{
		TenantID: tenantID, ModelID: inner.GetModelID(), ModelType: types.ModelTypeEmbedding,
		Purpose: purpose, CorrelationID: types.ModelCallCorrelationFromContext(ctx),
		PromptPrefixFingerprint: prefix, InputUnits: inputUnits, ItemCount: items,
		CacheHitItems: hits, CacheMissItems: misses, DurationMS: time.Since(started).Milliseconds(),
		Success: callErr == nil, Pricing: pricing,
	}
	if callErr != nil {
		record.ErrorCode = fmt.Sprintf("%T", callErr)
	}
	recordCtx, cancel := types.ModelCallPersistenceContext(ctx)
	defer cancel()
	if err := recorder.Record(recordCtx, record); err != nil {
		logger.Warnf(ctx, "record embedding model call failed: %v", err)
	}
}

func embeddingInputUnits(texts []string) int {
	total := 0
	for _, text := range texts {
		total += estimateEmbeddingTokens(text)
	}
	return total
}

func estimateEmbeddingTokens(text string) int {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func (w *cachedEmbedder) GetModelName() string { return w.inner.GetModelName() }
func (w *cachedEmbedder) GetDimensions() int   { return w.inner.GetDimensions() }
func (w *cachedEmbedder) GetModelID() string   { return w.inner.GetModelID() }
