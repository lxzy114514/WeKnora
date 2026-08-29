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
