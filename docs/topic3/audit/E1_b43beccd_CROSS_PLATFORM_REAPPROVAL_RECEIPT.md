# E1 Parser Corpus Cross Platform Reapproval Receipt

gate=E1
decision=accepted
corpus_manifest_sha256=1189575f4f3b71a565225b0fd3610627038fe4c733dde67bacc3b6065dbfb246
render_manifest_sha256=5d00f74e34e3fbc645b8a0b5dc1b85117527f69f4efe84f5e5466a07b8853c28
ground_truth_payload_sha256=62b6748d92fc51e63da5a62999006ac0938e57102766d7e71aef7b496e180703
extraction_rules_sha256=ee9df07b09be9dcee23196911640f6b10ddb9767f81ccc3c5909b84a8501dc0b
auditor_task_id=01a0906d-6717-73c1-a530-6947632b8c4b
reviewed_at=2026-09-12T19:24:10.1418974+08:00
review_base=b43beccdf4c70b13770c8b0a06a920c4b7905c1e
supersedes_receipt=docs/topic3/audit/E1_4edbde49_ROUND2_REVIEW_RECEIPT.md

## Scope

This bounded reapproval covers the cross-platform deterministic encoding repair,
the regenerated parser corpus, and the refreshed E1 render evidence. It does not
approve parser-engine quality rankings or any live benchmark result.

## Independent evidence

- The ground-truth payload and extraction rules are unchanged from the prior
  approval. The change is limited to deterministic PNG, fixed-Huffman DEFLATE,
  and ZIP container encoding plus propagation of generator errors.
- All five final DOCX/PDF page renders were reopened at original detail. Layout,
  anchors, tables, formula, embedded image, and reading order remain correct.
  Every page hash and the aggregate render hash are unchanged.
- Both Markdown sources retain their expected headings, lists, tables, code,
  link text, anchors, and reading order.
- Submission hashes and all referenced corpus, source, render-input, page, and
  intermediate-PDF identities were independently recomputed and match.
- Both DOCX archives use Store entries with create/extract version 20 and valid
  `1980-01-01 00:00:00` DOS timestamps; all CRC and complete-member reads pass.
- Fixed DEFLATE tests cover every length-code base and maximum, including a real
  258-byte match using code 285. Windows and Linux committed-corpus rebuilds pass.

## Decision

The current corpus preserves the approved truth and rendered meaning while
removing Go-version and operating-system compression drift. E1 is accepted for
the four hashes recorded above.
