# Topic 3 parser benchmark corpus v1

This corpus is original project-authored material. It contains three PDFs, two DOCX files, and two Markdown files. The seven document bytes are frozen inputs, not provider outputs.

Rebuild from the repository root with the managed Go toolchain:

~~~text
go run ./cmd/topic3-parser-corpus --output dataset/parser_benchmark/v1
~~~

The generator fixes PDF object order, DOCX ZIP entry order/timestamps, JSON order, and all document metadata. Verify determinism by building into two empty directories and comparing every relative path, byte length, and SHA-256.

For E1, compute each frozen input hash first. Render PDFs with Poppler at 150 DPI. Render DOCX with the local documents skill canonical render_docx.py using --emit_pdf, then render that PDF with the same Poppler. Inspect Markdown source directly. The independent Auditor must open every page/image and must also open sources/pdf-scanned-ocr-page-1.png. Hash-only checks cannot approve E1.

Hash contract: ground_truth_payload_sha256 covers only ground-truth.payload. Review metadata, receipt path/hash, and approved hash backlinks are excluded from that payload digest. corpus-manifest.json may therefore include the payload digest without a cycle. The later approval envelope binds four independent categories: corpus-manifest file bytes, independent render-page manifest, ground-truth payload, and extraction-rules file bytes.
