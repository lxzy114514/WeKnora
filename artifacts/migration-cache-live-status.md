# Live cold/warm cache run

- Status: `partial`
- Branch: `feat/topic3-evaluation-cost-cache-20260828`
- Commit: `db7ca387210c3b423e67b426b8fc23cacb2e74be`
- Evaluation runs created: `2`
- Runtime commit: `db7ca387210c`
- Config hash: `689bd0ea6add9ac43005e2511b729db7c78e33500f542267a6c63ca288a22f77`

Docker recovered, the current branch image built successfully, and the healthy service reported the expected commit. The cold run recorded 0/5 Embedding cache hits; the warm run recorded 5/5. Chat cache-read tokens rose from 0 to 1024 and cache-miss tokens fell from 1137 to 113.

Retrieval metrics were unchanged and ROUGE-L changed by about -0.006, within the configured quality tolerance. The complete evaluation gate still failed because average case latency increased by 13,917 ms as the remote generation call took longer. Model pricing was not configured, so both cost totals are estimated zero and do not prove a currency saving.

The result is therefore `partial`: live cache behavior is proven, while currency savings are unknown and the full latency gate is failed. See `migration-live-cold-warm-comparison.json` and the cold/warm detail reports for the exact evidence.
