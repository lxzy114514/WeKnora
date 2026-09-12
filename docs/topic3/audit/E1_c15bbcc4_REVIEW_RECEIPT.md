# E1 Parser Corpus and Ground Truth Review Receipt

gate=E1
decision=rejected
corpus_manifest_sha256=c2a62b5d689436d7de47d74fc9233d8fc24de85857c437f8a2babf63a5d6ec78
render_manifest_sha256=5d00f74e34e3fbc645b8a0b5dc1b85117527f69f4efe84f5e5466a07b8853c28
ground_truth_payload_sha256=62b6748d92fc51e63da5a62999006ac0938e57102766d7e71aef7b496e180703
extraction_rules_sha256=ee9df07b09be9dcee23196911640f6b10ddb9767f81ccc3c5909b84a8501dc0b
auditor_task_id=01a0906d-6717-73c1-a530-6947632b8c4b
reviewed_at=2026-09-12T11:56:43+08:00
frozen_corpus_commit=c15bbcc4cf5fb9cf5cb29b9e7f8d27dec8acb8d0
review_head=c38f748d98da43257c7b7f41e088c49514f3c56e

## Scope and method

This review is limited to the frozen parser corpus and ground truth in `dataset/parser_benchmark/v1`. The corpus tree has no changes between the frozen corpus commit and the review head. I did not use the implementation self-review conclusion as evidence, run a code review, invoke document or PDF skills, or regenerate the files. I independently opened all five committed page PNGs at original detail and directly read both Markdown sources, then compared each visible component with `visual-checklist.md`, `extraction-rules.json`, `ground-truth.json`, `corpus-manifest.json`, and `implementation-render-manifest.json`.

## Independent hash verification

- Seven document byte lengths and SHA-256 values match `corpus-manifest.json`.
- Every page or source-text artifact byte length and SHA-256 matches `implementation-render-manifest.json`.
- Every per-document page-row manifest SHA-256 recomputes successfully.
- The ordered seven-row aggregate recomputes to the recorded render binding.
- Canonical JSON of `ground-truth.payload` recomputes to the recorded ground-truth binding. Review metadata is outside this payload.
- The extraction-rules file bytes recompute to the recorded extraction-rules binding.
- The support matrix contains seven documents, eight engines, 56 cells, 37 supported cells, and 19 unsupported cells.

## Seven-document conclusions

### docx-embedded-image

- Input SHA-256: `c10ea759ee914973db0af1f58fc58227bc69d25380af6e101bf6260d04a9cca7`.
- Viewed `docx-embedded-image/page-001.png`, SHA-256 `c1f5339e1363bb7ec7300facd9964301028439114d5b75849d5617b36a52a08a`.
- The title and the NORTH PINE, CENTER RIVER, and SOUTH STONE anchors are visible exactly once and in that order.
- The page contains one visible embedded diagram: a blue river band between two green banks. All surrounding text is readable, with no clipping or overlap.

### docx-heading-list-table

- Input SHA-256: `8e647295dda9004d99da7f5ad34e36b8474adb15529d53afc27c5ee62a7122ad`.
- Viewed `docx-heading-list-table/page-001.png`, SHA-256 `2ef7e7e51322c808f8782843e678b4d1ddbbb2fcc1f29e5cae6483c5d7512a45`.
- The title and the NORTH GATE, CENTER BED, and SOUTH BENCH anchors are visible exactly once and in that order.
- The two unordered items and the nested ordered item are visibly distinct. The Plot and Seedlings table contains East with 12 and West with 9. No row, border, label, or text is clipped.

### md-code-link

- Input and source-text SHA-256: `99a72656f1ba2dfb71aa2f7ebfdbd43104cd7c46145ae159cd01f668faa8650e`.
- Direct source inspection confirms the level-one heading, the visible route guide link label, the `go` code block with the CENTER ARCH anchor, and the NORTH LAMP then CENTER ARCH then SOUTH CLOCK order.
- The link target is present in source while the ground-truth visible-text component correctly retains only its visible label. The code contents are retained.

### md-heading-list-table

- Input and source-text SHA-256: `ee390711259dc4aa236569268b83e9b69f4afa383eaa26a037c285e3d3cdde79`.
- Direct source inspection confirms the level-one heading, Hammer and Wrench unordered items, the nested ordered Small wrench item, and the Tool and Count table with Hammer 2 and Wrench 3.
- NORTH SHELF, CENTER DRAWER, and SOUTH DOOR each occur once and appear in the required order.

### pdf-digital-multicolumn

- Input SHA-256: `46508a494a23c8a865d3f936d21b08bee2eb0f24e34651c12d8f32c229e7d1ee`.
- Viewed `pdf-digital-multicolumn/page-001.png`, SHA-256 `16a3bedf8eb61f68f68b160edd6e8a921a24add57fb11419d20a3b6513a3461e`.
- The title is fully visible. The left column contains NORTH STAR, the left note, then CENTER BRIDGE; the right column contains the second-column note then SOUTH HARBOR. This supports the ground-truth left-column-before-right-column reading order.
- The divider and all text are within the page and readable.

### pdf-scanned-ocr

- Input SHA-256: `fa54e8a41a497372dd2525c888dbffb496c1d01e8b02040d6a0767957815e3e3`.
- Viewed the raster presentation `pdf-scanned-ocr/page-001.png`, SHA-256 `6df489874d6fa631dd106fd12765b9c759197d7e08fa6b3801508cc979d40445`.
- The raster visibly contains the title and every ground-truth sentence, including the seven young trees statement. NORTH APPLE, CENTER PEAR, and SOUTH PLUM each appear once and in order.
- This conclusion is based on the displayed raster pixels rather than a PDF text layer. All glyphs are readable and uncropped.

### pdf-table-formula

- Input SHA-256: `0ea5582803709f50b6f99a7010f9f19f593d20da38167315ba0686c8f543a2ef`.
- Viewed `pdf-table-formula/page-001.png`, SHA-256 `acc683d1eff2c11792b84d18fad6bc8abaaa274a6299244240d6d57737f864ba`.
- The title and the NORTH CABLE, CENTER PIER, and SOUTH DECK anchors are visible exactly once and in that order.
- The Span and Load table contains A with 12 kN and B with 18 kN. The displayed formula is `F=m*a`, matching the normalized formula truth. All components are readable and uncropped.

## Validation defect

- With the proposed approved review metadata in place, the required command `go test ./internal/evaluation -run 'TestCommittedParserCorpus|TestGeneratedParserCorpus|TestParserCorpus|TestExtractParser|TestScoreParser'` failed in `TestCommittedParserCorpusMatchesAFreshDeterministicBuild`.
- The fresh deterministic generator produced the pending-review `ground-truth.json` identity `10448:db5c5cb0dcf8fe056b2c2f2055e32c10d3e0fed70ce4f92bdcd8f65ca2c55680`, while the correctly approved review envelope produced `11181:2d43b055d603a114681ccf71b5e43543ca350479d65b60aaf912c434f7c42f45`.
- A temporary Go overlay test, stored outside the repository and removed after execution, directly called `LoadApprovedParserCorpus` against the proposed approval. It passed and returned all four expected bindings. This proves the approval loader accepts the data but does not resolve the committed-corpus determinism test conflict.

## Decision

All seven frozen presentations visually and semantically support the ordered ground-truth payload, and all four binding hashes recompute exactly. E1 is nevertheless rejected because the required committed-corpus deterministic-build test fails as soon as the permitted approval metadata is applied. The approval metadata and submission status were restored to their pending values; this receipt is the only retained review change. The blocker belongs to the corpus generation or deterministic comparison contract and must be fixed before a new E1 approval attempt.
