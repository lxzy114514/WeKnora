# WeKnora evaluation evaluation_10000_1789189543094_0c9b29f6_baselinev1

- Dataset: `baseline-v1`
- Dataset version: `1`
- Dataset hash: `f3369dfc145dfc0e3552542d3985911b36cd8dfc6cd95af43bc31284cccb193a`
- Configuration hash: `689bd0ea6add9ac43005e2511b729db7c78e33500f542267a6c63ca288a22f77`
- Code version: `db7ca387210c`
- Started: `2026-09-12T05:05:43Z`
- Ended: `2026-09-12T05:06:11Z`

## Quality, cost, and latency

| Metric | Value |
|---|---:|
| answer.bleu1 | 0.066298 |
| answer.bleu2 | 0.038384 |
| answer.bleu4 | 0.017439 |
| answer.rouge1 | 0.222222 |
| answer.rouge2 | 0.048387 |
| answer.rougel | 0.197531 |
| cost.call_count | 3.000000 |
| cost.chat_usd | 0.000000 |
| cost.embedding_usd | 0.000000 |
| cost.rerank_usd | 0.000000 |
| cost.total_usd | 0.000000 |
| latency.average_case_ms | 13449.000000 |
| latency.generation_ms | 13135.000000 |
| latency.index_ms | 14986.000000 |
| latency.model_call_ms | 28153.000000 |
| latency.rerank_ms | 0.000000 |
| latency.retrieval_ms | 310.000000 |
| latency.total_ms | 28463.000000 |
| retrieval.map | 1.000000 |
| retrieval.mrr | 1.000000 |
| retrieval.ndcg10 | 1.000000 |
| retrieval.ndcg3 | 1.000000 |
| retrieval.precision | 1.000000 |
| retrieval.recall | 1.000000 |

## Per-question evidence

| QID | Retrieved | Reranked | Duration (ms) | Answer | Error |
|---:|---|---|---:|---|---|
| 1 | [2 3 4 1] | [] | 13449 | 根据检索到的信息，计算机的操作系统包括以下几种：  - **Mac OS**：Apple Macintosh 操作系统的正式名称，具有图形用户界面（GUI），利用窗口、图标，所有应用程序都具有类似的用户界面。<kb doc="" chunk_id="e25b7261-ef5d-4bdd-adc2-55d3de0063d… |  |
