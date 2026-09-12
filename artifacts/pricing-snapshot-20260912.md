# DeepSeek price snapshot for one-time currency evidence

- Provider: DeepSeek
- Model: deepseek-v4-pro
- Official source: https://api-docs.deepseek.com/quick_start/pricing/
- Retrieved at (UTC): 2026-09-12T13:15:03.2227023Z
- Schedule effective from (UTC): 2026-08-16T16:00:00Z
- Applied period: off_peak
- Debug call (UTC): 2026-09-12T13:15:04.8639924Z to 2026-09-12T13:15:06.6321197Z
- Evaluation call window (UTC): 2026-09-12T13:15:06.6343218Z to 2026-09-12T13:15:30.1369547Z

| Period | UTC windows | Cache-hit input / 1M | Cache-miss input / 1M | Output / 1M |
| --- | --- | ---: | ---: | ---: |
| Off-peak | All other UTC hours, including weekends | USD 0.022 | USD 0.66 | USD 1.98 |
| Peak | Monday–Friday, 01:00–04:00 and 06:00–10:00 UTC | USD 0.044 | USD 1.32 | USD 3.96 |

The applied WeKnora fields map cache-miss input to input_per_million, cache-hit input to
cache_read_per_million, and output to output_per_million. DeepSeek publishes no separate
cache-write surcharge in this table, so no cache-write rate was configured.

This snapshot was used only for calls wholly inside the recorded tariff period. WeKnora's current
ModelPricing stores one static rate set and cannot represent a time-varying provider schedule.
The saved model configuration was explicitly restored with "pricing": {} after the run.

The local bge-m3:latest embedding model has no provider token tariff configured. Its provider
compute remains unknown; no zero price was invented. Historical calls were not updated.
