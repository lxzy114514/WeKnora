package metric

import (
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// Definition binds one stable portable name to the exact runtime calculator
// and MetricResult field. Both the online evaluator and deterministic CI use
// this registry, so they cannot silently drift to different metric sets.
type Definition struct {
	Name       string
	calculator interfaces.Metrics
	set        func(*types.MetricResult, float64)
	get        func(*types.MetricResult) float64
}

func (d Definition) Compute(input *types.MetricInput) float64 {
	return d.calculator.Compute(input)
}

func (d Definition) Set(result *types.MetricResult, value float64) {
	d.set(result, value)
}

func (d Definition) Get(result *types.MetricResult) float64 {
	return d.get(result)
}

var definitions = []Definition{
	{"retrieval.precision", NewPrecisionMetric(),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.Precision = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.Precision }},
	{"retrieval.recall", NewRecallMetric(),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.Recall = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.Recall }},
	{"retrieval.ndcg3", NewNDCGMetric(3),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.NDCG3 = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.NDCG3 }},
	{"retrieval.ndcg10", NewNDCGMetric(10),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.NDCG10 = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.NDCG10 }},
	{"retrieval.mrr", NewMRRMetric(),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.MRR = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.MRR }},
	{"retrieval.map", NewMAPMetric(),
		func(r *types.MetricResult, v float64) { r.RetrievalMetrics.MAP = v },
		func(r *types.MetricResult) float64 { return r.RetrievalMetrics.MAP }},
	{"answer.bleu1", NewBLEUMetric(true, BLEU1Gram),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.BLEU1 = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.BLEU1 }},
	{"answer.bleu2", NewBLEUMetric(true, BLEU2Gram),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.BLEU2 = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.BLEU2 }},
	{"answer.bleu4", NewBLEUMetric(true, BLEU4Gram),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.BLEU4 = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.BLEU4 }},
	{"answer.rouge1", NewRougeMetric(true, "rouge-1", "f"),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.ROUGE1 = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.ROUGE1 }},
	{"answer.rouge2", NewRougeMetric(true, "rouge-2", "f"),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.ROUGE2 = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.ROUGE2 }},
	{"answer.rougel", NewRougeMetric(true, "rouge-l", "f"),
		func(r *types.MetricResult, v float64) { r.GenerationMetrics.ROUGEL = v },
		func(r *types.MetricResult) float64 { return r.GenerationMetrics.ROUGEL }},
}

func Definitions() []Definition {
	return append([]Definition(nil), definitions...)
}
