# Reviewed live baselines

Do not create a baseline by editing a configuration hash. Run `evalctl` against
the reviewed deployment and commit its portable JSON report unchanged as:

```text
<config_hash>.json
```

Set `WEKNORA_EVAL_BASELINE_CONFIG_HASH` to that exact hash. The workflow loads
the matching immutable file and `evalgate` independently verifies that the live
report has the same hash. Baseline metric changes therefore remain normal,
reviewable code changes.

The approval record for every promoted baseline must also bind
`metric_contract_version=1`. The portable JSON schema remains backward
compatible with older diagnostic reports; changing a metric formula,
passage-ID mapping, tokenization, or normalization requires a new approval and
pilot even though the version is recorded outside the portable JSON.
