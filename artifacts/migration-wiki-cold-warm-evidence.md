# Wiki cold/warm cache evidence

Status: **partial**; strict comparability: **not_comparable**.

- Deployed commit: `db7ca387210c`
- Controlled knowledge IDs: cold `200b3ab3-9bd3-4c8d-b9bf-0b2b53cd04f7`, warm `dff66f18-5612-416c-b5db-0c19fbf95e6d`
- Scope: `purpose LIKE 'wiki_%'` in the two non-overlapping ingestion windows.
- Separation used time windows because production async Wiki calls left `correlation_id` empty; the exact knowledge IDs and timestamps are retained.

| fact | cold | warm |
| --- | ---: | ---: |
| Wiki model calls | 9 | 11 |
| prompt tokens | 13587 | 18862 |
| cache-read tokens | 2560 | 6784 |
| cache-miss tokens | 11027 | 12078 |
| provider cache-read ratio | 0.188415 | 0.359665 |
| summed call duration (ms) | 29215 | 35248 |
| cost | unknown | unknown |

The warm run increased cache-read tokens by 4224 and the cache-read ratio by 0.171250. This proves cache use in the real Wiki path. It does not prove an end-to-end cost or latency improvement: warm cache-miss tokens were 12078 rather than zero, the purpose/call shape changed as Wiki state evolved, and all Wiki cost rows were estimated zero without complete prices. Unknown cost is never treated as zero.

Raw per-call facts are preserved in `artifacts/migration-wiki-cold-warm-evidence.json`.
