package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
)

type parserCorpusAuthoringV1 struct {
	SchemaVersion    int                        `json:"schema_version"`
	GeneratorVersion string                     `json:"generator_version"`
	Documents        []parserAuthoredDocumentV1 `json:"documents"`
}

type parserAuthoredDocumentV1 struct {
	ID                 string   `json:"id"`
	Format             string   `json:"format"`
	Title              string   `json:"title"`
	Anchors            []string `json:"anchors"`
	EmbeddedImageCount int      `json:"embedded_image_count,omitempty"`
	Markdown           string   `json:"markdown"`
	Tags               []string `json:"tags"`
}

var frozenParserAuthoringDocuments = map[string]string{
	"docx-embedded-image":     "docx",
	"docx-heading-list-table": "docx",
	"md-code-link":            "md",
	"md-heading-list-table":   "md",
	"pdf-digital-multicolumn": "pdf",
	"pdf-scanned-ocr":         "pdf",
	"pdf-table-formula":       "pdf",
}

type parserRenderInputManifestV1 struct {
	SchemaVersion int                           `json:"schema_version"`
	CorpusVersion string                        `json:"corpus_version"`
	Documents     []parserRenderInputDocumentV1 `json:"documents"`
	Contract      parserRenderContractV1        `json:"contract"`
}

type parserRenderInputDocumentV1 struct {
	ID                string                `json:"id"`
	Input             ParserFileIdentityV1  `json:"input"`
	ExpectedPages     int                   `json:"expected_pages"`
	InspectionMode    string                `json:"inspection_mode"`
	OriginalPageImage *ParserFileIdentityV1 `json:"original_page_image,omitempty"`
}

type parserRenderContractV1 struct {
	PDF      string `json:"pdf"`
	DOCX     string `json:"docx"`
	Markdown string `json:"markdown"`
	Digest   string `json:"digest"`
}

func BuildParserCorpus(templateRoot, outputRoot string) error {
	authoringBytes, err := os.ReadFile(filepath.Join(templateRoot, "sources", "authoring.json"))
	if err != nil {
		return fmt.Errorf("read parser authoring source: %w", err)
	}
	extractionRulesBytes, err := os.ReadFile(filepath.Join(templateRoot, "extraction-rules.json"))
	if err != nil {
		return fmt.Errorf("read parser extraction rules: %w", err)
	}
	var authoring parserCorpusAuthoringV1
	if err := decodeStrictJSON(authoringBytes, &authoring); err != nil {
		return fmt.Errorf("decode parser authoring source: %w", err)
	}
	if err := validateParserAuthoring(authoring); err != nil {
		return err
	}

	for _, directory := range []string{"sources", "documents", "e1"} {
		if err := os.MkdirAll(filepath.Join(outputRoot, directory), 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "sources", "authoring.json"), authoringBytes, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "extraction-rules.json"), extractionRulesBytes, 0o644); err != nil {
		return err
	}

	documents := make([]ParserCorpusDocumentV1, 0, len(authoring.Documents))
	groundTruthDocuments := make([]ParserGroundTruthDocumentV1, 0, len(authoring.Documents))
	renderInputs := make([]parserRenderInputDocumentV1, 0, len(authoring.Documents))
	var scannedPageIdentity *ParserFileIdentityV1
	for _, authored := range authoring.Documents {
		filename := authored.ID + "." + authored.Format
		repositoryPath := ParserCorpusRepositoryPath + "/documents/" + filename
		outputPath, err := parserCorpusOutputPath(outputRoot, "documents", filename)
		if err != nil {
			return err
		}
		var contents []byte
		var generator ParserGeneratorIdentityV1
		switch authored.ID {
		case "pdf-digital-multicolumn":
			contents = buildDigitalMulticolumnPDF(authored)
			generator = ParserGeneratorIdentityV1{Tool: "weknora-deterministic-pdf", Version: "1"}
		case "pdf-scanned-ocr":
			pagePNG, err := buildScannedPagePNG(authored)
			if err != nil {
				return fmt.Errorf("generate parser document %s page image: %w", authored.ID, err)
			}
			pagePath := filepath.Join(outputRoot, "sources", "pdf-scanned-ocr-page-1.png")
			if err := os.WriteFile(pagePath, pagePNG, 0o644); err != nil {
				return err
			}
			identity, err := parserFileIdentity(outputRoot, ParserCorpusRepositoryPath+"/sources/pdf-scanned-ocr-page-1.png")
			if err != nil {
				return err
			}
			scannedPageIdentity = &identity
			contents = buildImagePDF(pagePNG)
			generator = ParserGeneratorIdentityV1{Tool: "weknora-deterministic-raster-pdf", Version: "1"}
		case "pdf-table-formula":
			contents = buildTableFormulaPDF(authored)
			generator = ParserGeneratorIdentityV1{Tool: "weknora-deterministic-pdf", Version: "1"}
		case "docx-heading-list-table":
			contents, err = buildHeadingListTableDOCX(authored)
			if err != nil {
				return fmt.Errorf("generate parser document %s: %w", authored.ID, err)
			}
			generator = ParserGeneratorIdentityV1{Tool: "weknora-deterministic-docx", Version: "1"}
		case "docx-embedded-image":
			contents, err = buildEmbeddedImageDOCX(authored)
			if err != nil {
				return fmt.Errorf("generate parser document %s: %w", authored.ID, err)
			}
			generator = ParserGeneratorIdentityV1{Tool: "weknora-deterministic-docx", Version: "1"}
		case "md-code-link", "md-heading-list-table":
			contents = []byte(normalizeParserSource(authored.Markdown))
			generator = ParserGeneratorIdentityV1{Tool: "literal-utf8", Version: "1"}
		default:
			return fmt.Errorf("parser_authoring_document_unknown: %s", authored.ID)
		}
		if err := os.WriteFile(outputPath, contents, 0o644); err != nil {
			return err
		}
		identity, err := parserFileIdentity(outputRoot, repositoryPath)
		if err != nil {
			return err
		}
		documents = append(documents, ParserCorpusDocumentV1{
			ID: authored.ID, Path: repositoryPath, Extension: authored.Format, Bytes: identity.Bytes,
			SHA256: identity.SHA256, Attributes: sortedStrings(authored.Tags),
			Sources: []string{ParserCorpusRepositoryPath + "/sources/authoring.json"}, Generator: generator,
		})
		components, err := ExtractParserComponents(authored.Markdown, authored.Anchors)
		if err != nil {
			return fmt.Errorf("extract ground truth %s: %w", authored.ID, err)
		}
		groundTruthDocuments = append(groundTruthDocuments, ParserGroundTruthDocumentV1{
			ID: authored.ID, Components: components, ExpectedEmbeddedImageCount: authored.EmbeddedImageCount,
		})
		renderInput := parserRenderInputDocumentV1{ID: authored.ID, Input: identity, ExpectedPages: 1, InspectionMode: "paged_render"}
		if authored.Format == "md" {
			renderInput.InspectionMode = "source_text"
		}
		if authored.ID == "pdf-scanned-ocr" {
			renderInput.OriginalPageImage = scannedPageIdentity
		}
		renderInputs = append(renderInputs, renderInput)
	}

	payload := ParserGroundTruthPayloadV1{
		CorpusVersion: "v1", ParserNormalizationVersion: ParserNormalizationVersion,
		ParserMetricContractVersion: ParserMetricContractVersion, Documents: groundTruthDocuments,
	}
	payloadHash, err := CanonicalSHA256(payload)
	if err != nil {
		return err
	}
	groundTruth := ParserGroundTruthV1{
		SchemaVersion: ParserGroundTruthSchemaVersion, Payload: payload,
		Review: ParserApprovalReviewV1{Status: "pending", Reason: "not_reviewed"},
	}
	if err := writeDeterministicJSON(filepath.Join(outputRoot, "ground-truth.json"), groundTruth); err != nil {
		return err
	}

	renderManifest := parserRenderInputManifestV1{
		SchemaVersion: 1, CorpusVersion: "v1", Documents: renderInputs,
		Contract: parserRenderContractV1{
			PDF: "pdftoppm -png -r 150", DOCX: "documents skill render_docx.py --emit_pdf, then pdftoppm at 150 DPI",
			Markdown: "inspect frozen UTF-8 source directly", Digest: "sha256 of UTF-8 TSV rows sorted by document ID then page number: relative_path<TAB>bytes<TAB>sha256<LF>",
		},
	}
	if err := writeDeterministicJSON(filepath.Join(outputRoot, "e1", "render-input-manifest.json"), renderManifest); err != nil {
		return err
	}

	authoringIdentity, err := parserFileIdentity(outputRoot, ParserCorpusRepositoryPath+"/sources/authoring.json")
	if err != nil {
		return err
	}
	extractionIdentity, err := parserFileIdentity(outputRoot, ParserCorpusRepositoryPath+"/extraction-rules.json")
	if err != nil {
		return err
	}
	renderInputIdentity, err := parserFileIdentity(outputRoot, ParserCorpusRepositoryPath+"/e1/render-input-manifest.json")
	if err != nil {
		return err
	}
	sources := []ParserFileIdentityV1{authoringIdentity}
	if scannedPageIdentity != nil {
		sources = append(sources, *scannedPageIdentity)
	}
	manifest := ParserCorpusManifestV1{
		SchemaVersion: ParserCorpusSchemaVersion, Name: "topic3-parser-benchmark", Version: "v1",
		GeneratorVersion: authoring.GeneratorVersion, SampleIDRule: "parser:<corpus_document_id>", SortRule: "document_id_ascending",
		Sources: sources, Documents: documents, GroundTruthPath: ParserCorpusRepositoryPath + "/ground-truth.json",
		GroundTruthPayloadSHA256: payloadHash, ExtractionRules: extractionIdentity, RenderInputManifest: renderInputIdentity,
		SupportMatrix: buildParserSupportMatrix(documents),
	}
	if err := writeDeterministicJSON(filepath.Join(outputRoot, "corpus-manifest.json"), manifest); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "README.md"), []byte(parserCorpusREADME), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputRoot, "e1", "visual-checklist.md"), []byte(buildParserVisualChecklist(authoring.Documents)), 0o644); err != nil {
		return err
	}
	_, err = LoadParserCorpusDirectory(outputRoot, false)
	return err
}

func validateParserAuthoring(authoring parserCorpusAuthoringV1) error {
	if authoring.SchemaVersion != 1 || authoring.GeneratorVersion != "topic3-parser-corpus-go-v1" || len(authoring.Documents) != 7 {
		return fmt.Errorf("parser_authoring_invalid")
	}
	ids := make([]string, len(authoring.Documents))
	formats := make(map[string]int)
	for index, document := range authoring.Documents {
		ids[index] = document.ID
		formats[document.Format]++
		if document.ID == "" || document.Title == "" || document.Markdown == "" || len(document.Anchors) < 3 {
			return fmt.Errorf("parser_authoring_document_invalid")
		}
		wantFormat, known := frozenParserAuthoringDocuments[document.ID]
		if !known || document.Format != wantFormat {
			return fmt.Errorf("parser_authoring_document_unknown: %s", document.ID)
		}
	}
	if !sort.StringsAreSorted(ids) || len(uniqueStrings(ids)) != len(frozenParserAuthoringDocuments) || formats["pdf"] != 3 || formats["docx"] != 2 || formats["md"] != 2 {
		return fmt.Errorf("parser_authoring_order_or_format_invalid")
	}
	return nil
}

func parserCorpusOutputPath(outputRoot string, elements ...string) (string, error) {
	root, err := filepath.Abs(outputRoot)
	if err != nil {
		return "", fmt.Errorf("parser_corpus_output_root_invalid: %w", err)
	}
	candidate, err := filepath.Abs(filepath.Join(append([]string{root}, elements...)...))
	if err != nil {
		return "", fmt.Errorf("parser_corpus_output_path_invalid: %w", err)
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("parser_corpus_output_path_escape")
	}
	return candidate, nil
}

func uniqueStrings(values []string) map[string]struct{} {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	return unique
}

func buildParserSupportMatrix(documents []ParserCorpusDocumentV1) []ParserSupportRowV1 {
	registry := docparser.RegisteredEngineFileTypesSnapshot(false)
	rows := make([]ParserSupportRowV1, 0, len(registry))
	for _, engine := range registry {
		row := ParserSupportRowV1{Engine: engine.Name, Documents: make([]ParserSupportCellV1, 0, len(documents))}
		for _, document := range documents {
			row.Documents = append(row.Documents, ParserSupportCellV1{DocumentID: document.ID, Supported: containsParserFileType(engine.FileTypes, document.Extension)})
		}
		rows = append(rows, row)
	}
	return rows
}

func buildParserVisualChecklist(documents []parserAuthoredDocumentV1) string {
	var out strings.Builder
	out.WriteString("# T11 seven-document visual checklist\n\nStatus: implementation self-check pending; independent Auditor must re-render and inspect every item.\n\n")
	for _, document := range documents {
		fmt.Fprintf(&out, "## %s\n\n- [ ] Frozen input hash matches the E1 submission.\n- [ ] Expected page count: 1.\n- [ ] Page 1 opened at 100%% and all visible content is readable.\n- [ ] Heading/title is visible: %s.\n", document.ID, document.Title)
		for _, anchor := range document.Anchors {
			fmt.Fprintf(&out, "- [ ] Anchor is visible exactly once and in order: %s.\n", anchor)
		}
		for _, tag := range document.Tags {
			fmt.Fprintf(&out, "- [ ] Component checked: %s.\n", tag)
		}
		if document.ID == "pdf-scanned-ocr" {
			out.WriteString("- [ ] Original raster page image opened and compared; no text layer was used as a substitute.\n")
		}
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

const parserCorpusREADME = `# Topic 3 parser benchmark corpus v1

This corpus is original project-authored material. It contains three PDFs, two DOCX files, and two Markdown files. The seven document bytes are frozen inputs, not provider outputs.

Rebuild from the repository root with the managed Go toolchain:

~~~text
go run ./cmd/topic3-parser-corpus --output dataset/parser_benchmark/v1
~~~

The generator fixes PDF object order, DOCX ZIP entry order/timestamps, JSON order, and all document metadata. Verify determinism by building into two empty directories and comparing every relative path, byte length, and SHA-256.

For E1, compute each frozen input hash first. Render PDFs with Poppler at 150 DPI. Render DOCX with the local documents skill canonical render_docx.py using --emit_pdf, then render that PDF with the same Poppler. Inspect Markdown source directly. The independent Auditor must open every page/image and must also open sources/pdf-scanned-ocr-page-1.png. Hash-only checks cannot approve E1.

Hash contract: ground_truth_payload_sha256 covers only ground-truth.payload. Review metadata, receipt path/hash, and approved hash backlinks are excluded from that payload digest. corpus-manifest.json may therefore include the payload digest without a cycle. The later approval envelope binds four independent categories: corpus-manifest file bytes, independent render-page manifest, ground-truth payload, and extraction-rules file bytes.
`
