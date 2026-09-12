# WeKnora evaluation evaluation_10000_1789218907995_d55336e5_baselinev1

- Dataset: `baseline-v1`
- Dataset version: `1`
- Dataset hash: `f3369dfc145dfc0e3552542d3985911b36cd8dfc6cd95af43bc31284cccb193a`
- Configuration hash: `4844ce1fcdfa2de040d2121c55022c0cdb4aeb6ff6fa745882059965f66de967`
- Code version: `c88047bf0285a1ded72f06ff038f3c1679b2ef2d`
- Started: `2026-09-12T13:15:08Z`
- Ended: `2026-09-12T13:15:28Z`

## Quality, cost, and latency

| Metric | Value |
|---|---:|
| answer.bleu1 | 0.070270 |
| answer.bleu2 | 0.039085 |
| answer.bleu4 | 0.017403 |
| answer.rouge1 | 0.232558 |
| answer.rouge2 | 0.047244 |
| answer.rougel | 0.209302 |
| cost.call_count | 3.000000 |
| cost.chat_usd | 0.002909 |
| cost.embedding_usd | 0.000000 |
| cost.rerank_usd | 0.000000 |
| cost.total_usd | 0.002909 |
| latency.average_case_ms | 20211.000000 |
| latency.generation_ms | 19087.000000 |
| latency.index_ms | 465.000000 |
| latency.model_call_ms | 19087.000000 |
| latency.rerank_ms | 0.000000 |
| latency.retrieval_ms | 973.000000 |
| latency.total_ms | 20742.000000 |
| retrieval.map | 1.000000 |
| retrieval.mrr | 1.000000 |
| retrieval.ndcg10 | 1.000000 |
| retrieval.ndcg3 | 1.000000 |
| retrieval.precision | 1.000000 |
| retrieval.recall | 1.000000 |

## Per-question evidence

| QID | Retrieved | Reranked | Duration (ms) | Answer | Error |
|---:|---|---|---:|---|---|
| 1 | [2 3 4 1] | [] | 20211 | 根据检索信息，计算机的操作系统主要有以下几类：  - **Mac OS**：Apple Macintosh 操作系统的正式名称，具有图形用户界面（GUI），使用窗口、图标，运行在 Macintosh 计算机上的应用程序都具有类似的用户界面。<kb doc="" chunk_id="63f63ab3-cecb-4326-… |  |
