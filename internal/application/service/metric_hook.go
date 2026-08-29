package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/application/service/metric"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

const sourcePassageIndexMetadataKey = "source_passage_index"

// MetricList stores and aggregates metric results
type MetricList struct {
	results []*types.MetricResult
}

// Append calculates and stores metrics for given input
func (m *MetricList) Append(metricInput *types.MetricInput) *types.MetricResult {
	result := &types.MetricResult{}
	// Calculate all configured metrics
	for _, definition := range metric.Definitions() {
		definition.Set(result, definition.Compute(metricInput))
	}
	logger.Infof(context.Background(), "metric: %v", result)
	m.results = append(m.results, result)
	return result
}

// Avg calculates average of all stored metric results
func (m *MetricList) Avg() *types.MetricResult {
	if len(m.results) == 0 {
		return &types.MetricResult{}
	}

	avgResult := &types.MetricResult{}
	count := float64(len(m.results))

	// Calculate average for each metric
	for _, definition := range metric.Definitions() {
		sum := 0.0
		for _, r := range m.results {
			sum += definition.Get(r)
		}
		definition.Set(avgResult, sum/count)
	}
	return avgResult
}

// HookMetric tracks evaluation metrics for QA pairs
type HookMetric struct {
	qaPairMetricList []*qaPairMetric // Per-QA pair metrics
	metricResults    *MetricList     // Aggregated results
	corpus           []string        // Passage text indexed by dataset PID
	mu               *sync.RWMutex   // Thread safety
}

// qaPairMetric stores metrics for a single QA pair
type qaPairMetric struct {
	qaPair       *types.QAPair
	searchResult []*types.SearchResult
	rerankResult []*types.SearchResult
	chatResponse *types.ChatResponse
}

// NewHookMetric creates a new HookMetric with given capacity
func NewHookMetric(capacity int, corpus []string) *HookMetric {
	return &HookMetric{
		metricResults:    &MetricList{},
		qaPairMetricList: make([]*qaPairMetric, capacity),
		corpus:           corpus,
		mu:               &sync.RWMutex{},
	}
}

// recordInit initializes metric tracking for a QA pair
func (h *HookMetric) recordInit(index int) {
	h.qaPairMetricList[index] = &qaPairMetric{}
}

// recordQaPair records the QA pair data
func (h *HookMetric) recordQaPair(index int, qaPair *types.QAPair) {
	h.qaPairMetricList[index].qaPair = qaPair
}

// recordSearchResult records search results
func (h *HookMetric) recordSearchResult(index int, searchResult []*types.SearchResult) {
	h.qaPairMetricList[index].searchResult = searchResult
}

// recordRerankResult records reranked results
func (h *HookMetric) recordRerankResult(index int, rerankResult []*types.SearchResult) {
	h.qaPairMetricList[index].rerankResult = rerankResult
}

// recordChatResponse records the generated chat response
func (h *HookMetric) recordChatResponse(index int, chatResponse *types.ChatResponse) {
	h.qaPairMetricList[index].chatResponse = chatResponse
}

// recordFinish finalizes metrics for a QA pair
func (h *HookMetric) recordFinish(index int) *types.MetricResult {
	// Prepare retrieval source: prefer rerank results, fall back to search results
	retrievalSource := h.qaPairMetricList[index].rerankResult
	if len(retrievalSource) == 0 {
		retrievalSource = h.qaPairMetricList[index].searchResult
	}

	// Map retrieved chunks back to original passage IDs via content matching.
	// ChunkIndex is the chunk's ordinal position in the knowledge base, which
	// does NOT correspond to the dataset's passage IDs. Match against the whole
	// corpus, not just ground-truth passages; otherwise false positives vanish
	// from RetrievalIDs and precision is incorrectly inflated.
	qaPair := h.qaPairMetricList[index].qaPair
	retrievalIDs := mapSearchResultsToPassageIDs(h.corpus, retrievalSource)

	// Get generated text if available
	generatedTexts := ""
	if h.qaPairMetricList[index].chatResponse != nil {
		generatedTexts = h.qaPairMetricList[index].chatResponse.Content
	}

	// Prepare metric input data
	metricInput := &types.MetricInput{
		RetrievalGT:    [][]int{qaPair.PIDs},
		RetrievalIDs:   retrievalIDs,
		GeneratedTexts: generatedTexts,
		GeneratedGT:    qaPair.Answer,
	}

	// Thread-safe append of metrics
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.metricResults.Append(metricInput)
}

func mapSearchResultsToPassageIDs(corpus []string, results []*types.SearchResult) []int {
	retrievalIDs := make([]int, 0, len(results))
	seen := make(map[int]struct{})
	for _, r := range results {
		var metadata map[string]string
		if len(r.ChunkMetadata) > 0 && json.Unmarshal(r.ChunkMetadata, &metadata) == nil {
			if rawPID, ok := metadata[sourcePassageIndexMetadataKey]; ok {
				pid, err := strconv.Atoi(rawPID)
				if err == nil && pid >= 0 && pid < len(corpus) && corpus[pid] != "" {
					if _, exists := seen[pid]; !exists {
						seen[pid] = struct{}{}
						retrievalIDs = append(retrievalIDs, pid)
					}
					continue
				}
			}
		}
		if r.Content == "" {
			continue
		}
		for pid, passage := range corpus {
			if passage == "" {
				continue
			}
			if strings.Contains(passage, r.Content) || strings.Contains(r.Content, passage) {
				if _, ok := seen[pid]; !ok {
					seen[pid] = struct{}{}
					retrievalIDs = append(retrievalIDs, pid)
				}
				break
			}
		}
	}
	return retrievalIDs
}

// MetricResult returns the averaged metric results
func (h *HookMetric) MetricResult() *types.MetricResult {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.metricResults.Avg()
}
