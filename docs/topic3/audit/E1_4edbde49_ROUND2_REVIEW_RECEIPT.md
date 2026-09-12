# E1 Parser Corpus Round 2 Review Receipt

gate=E1
decision=accepted
corpus_manifest_sha256=c2a62b5d689436d7de47d74fc9233d8fc24de85857c437f8a2babf63a5d6ec78
render_manifest_sha256=5d00f74e34e3fbc645b8a0b5dc1b85117527f69f4efe84f5e5466a07b8853c28
ground_truth_payload_sha256=62b6748d92fc51e63da5a62999006ac0938e57102766d7e71aef7b496e180703
extraction_rules_sha256=ee9df07b09be9dcee23196911640f6b10ddb9767f81ccc3c5909b84a8501dc0b
auditor_task_id=01a0906d-6717-73c1-a530-6947632b8c4b
reviewed_at=2026-09-12T12:04:09+08:00
frozen_corpus_commit=c15bbcc4cf5fb9cf5cb29b9e7f8d27dec8acb8d0
review_head=4edbde495b8bb37d98e5ffef0ec198dafb8e4334
prior_rejected_receipt_sha256=0f5b3102f61c346d7ef9935c1baa17831d59c74cf59f4749c5a554814e750c95

## Bounded repair review

The repair changes only `internal/evaluation/parser_corpus_test.go` in addition to committing the prior rejection receipt. The deterministic-build test continues to compare every generated file byte for byte except `ground-truth.json`. For that file it strictly decodes the generated and committed schemas, requires identical `schema_version` values and complete payload values, and permits the committed review status to be only `pending` or `approved`.

This is a valid separation of responsibilities. Corpus generation owns the stable schema and payload. Independent approval owns the mutable review envelope. An approved envelope is not trusted by the relaxed deterministic comparison: `LoadApprovedParserCorpus` separately requires `reviewed_by=independent_auditor`, the fixed auditor task ID, a valid RFC3339 review time, all four current hashes, a repository-local receipt path, a matching receipt byte hash, and matching receipt fields. Pending data remains unable to pass the approval loader.

The complete `dataset/parser_benchmark/v1` tree has no changes between frozen corpus commit `c15bbcc4cf5fb9cf5cb29b9e7f8d27dec8acb8d0` and review head `4edbde495b8bb37d98e5ffef0ec198dafb8e4334`. All seven document identities, all seven render/source rows, every per-document page manifest, the aggregate render hash, the canonical ground-truth payload hash, the extraction-rules hash, and the 56-cell support matrix were independently recomputed without drift.

## Reused visual and semantic evidence

The prior E1 review opened all five frozen PNGs at original detail and directly inspected both Markdown sources. Because every corpus, page, source-text, manifest, payload, and rules hash is unchanged, this round reuses those hash-bound conclusions as authorized:

- `docx-embedded-image`: title, NORTH PINE to CENTER RIVER to SOUTH STONE order, explanatory text, and the single blue-river-between-green-banks embedded image are correct and uncropped.
- `docx-heading-list-table`: title, two bullet items, nested ordered item, Plot and Seedlings table with East 12 and West 9, and NORTH GATE to CENTER BED to SOUTH BENCH order are correct.
- `md-code-link`: heading, visible link label, retained Go code, and NORTH LAMP to CENTER ARCH to SOUTH CLOCK order are correct.
- `md-heading-list-table`: heading, unordered and nested ordered lists, Tool and Count table with Hammer 2 and Wrench 3, and NORTH SHELF to CENTER DRAWER to SOUTH DOOR order are correct.
- `pdf-digital-multicolumn`: heading, complete left column followed by complete right column, and NORTH STAR to CENTER BRIDGE to SOUTH HARBOR order are correct and readable.
- `pdf-scanned-ocr`: raster-visible title, seven-young-trees sentence, and NORTH APPLE to CENTER PEAR to SOUTH PLUM order exactly support the OCR truth without relying on a text layer.
- `pdf-table-formula`: heading, Span and Load table with A 12 kN and B 18 kN, normalized formula `F=m*a`, and NORTH CABLE to CENTER PIER to SOUTH DECK order are correct.

## Decision

The deterministic comparison now preserves byte stability for generated corpus content while allowing the independently validated approval envelope to exist. No corpus drift or new blocker was found. E1 is accepted for the four hashes recorded above.
