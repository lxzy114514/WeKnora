package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const ParserE1AuditorTaskID = "01a0906d-6717-73c1-a530-6947632b8c4b"

type ParserToolIdentityV1 struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256,omitempty"`
	Version string `json:"version,omitempty"`
}

type ParserRenderProvenanceV1 struct {
	Role      string                 `json:"role"`
	CreatedAt string                 `json:"created_at"`
	Tools     []ParserToolIdentityV1 `json:"tools,omitempty"`
	Commands  []string               `json:"commands,omitempty"`
}

type ParserRenderedPageV1 struct {
	Page         int                  `json:"page"`
	Kind         string               `json:"kind"`
	RelativePath string               `json:"relative_path"`
	Artifact     ParserFileIdentityV1 `json:"artifact"`
}

type ParserRenderedDocumentV1 struct {
	ID                    string                 `json:"id"`
	InputSHA256           string                 `json:"input_sha256"`
	InspectionMode        string                 `json:"inspection_mode"`
	Pages                 []ParserRenderedPageV1 `json:"pages"`
	IntermediateArtifacts []ParserFileIdentityV1 `json:"intermediate_artifacts,omitempty"`
	PageManifestSHA256    string                 `json:"page_manifest_sha256"`
}

type ParserRenderManifestV1 struct {
	SchemaVersion   int                        `json:"schema_version"`
	CorpusVersion   string                     `json:"corpus_version"`
	Provenance      ParserRenderProvenanceV1   `json:"provenance"`
	Documents       []ParserRenderedDocumentV1 `json:"documents"`
	AggregateSHA256 string                     `json:"aggregate_sha256"`
}

type ParserE1SubmissionV1 struct {
	SchemaVersion              int                      `json:"schema_version"`
	Gate                       string                   `json:"gate"`
	Status                     string                   `json:"status"`
	Hashes                     ParserApprovalHashesV1   `json:"hashes"`
	RenderManifestFile         ParserFileIdentityV1     `json:"render_manifest_file"`
	GroundTruthFile            ParserFileIdentityV1     `json:"ground_truth_file"`
	RenderInput                ParserFileIdentityV1     `json:"render_input"`
	Sources                    []ParserFileIdentityV1   `json:"sources"`
	Documents                  []ParserCorpusDocumentV1 `json:"documents"`
	VisualChecklist            ParserFileIdentityV1     `json:"visual_checklist"`
	ImplementationVisualReview ParserFileIdentityV1     `json:"implementation_visual_review"`
	AuditorTaskID              string                   `json:"auditor_task_id"`
	NonCircularContract        string                   `json:"non_circular_contract"`
}

func SealParserE1Submission(corpusRoot string, provenance ParserRenderProvenanceV1) (ParserFileIdentityV1, error) {
	loaded, err := LoadParserCorpusDirectory(corpusRoot, false)
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	if provenance.Role == "" || provenance.CreatedAt == "" {
		return ParserFileIdentityV1{}, fmt.Errorf("parser_render_provenance_incomplete")
	}
	if _, err := time.Parse(time.RFC3339, provenance.CreatedAt); err != nil {
		return ParserFileIdentityV1{}, fmt.Errorf("parser_render_provenance_time_invalid")
	}
	documents := make([]ParserRenderedDocumentV1, 0, len(loaded.Manifest.Documents))
	allRows := make([]string, 0)
	for _, document := range loaded.Manifest.Documents {
		rendered := ParserRenderedDocumentV1{ID: document.ID, InputSHA256: document.SHA256, InspectionMode: "paged_render"}
		if document.Extension == "md" {
			rendered.InspectionMode = "source_text"
			artifact := ParserFileIdentityV1{Path: document.Path, Bytes: document.Bytes, SHA256: document.SHA256}
			rendered.Pages = []ParserRenderedPageV1{{Page: 1, Kind: "source_text", RelativePath: document.ID + "/source.md", Artifact: artifact}}
		} else {
			directory := filepath.Join(corpusRoot, "e1", "implementation-render", document.ID)
			matches, err := filepath.Glob(filepath.Join(directory, "page-*.png"))
			if err != nil {
				return ParserFileIdentityV1{}, err
			}
			sort.Strings(matches)
			if len(matches) == 0 {
				return ParserFileIdentityV1{}, fmt.Errorf("parser_render_pages_missing: %s", document.ID)
			}
			for index, pagePath := range matches {
				contents, err := os.ReadFile(pagePath)
				if err != nil {
					return ParserFileIdentityV1{}, err
				}
				if _, err := png.DecodeConfig(bytes.NewReader(contents)); err != nil {
					return ParserFileIdentityV1{}, fmt.Errorf("parser_render_page_invalid: %s: %w", document.ID, err)
				}
				artifactPath := ParserCorpusRepositoryPath + "/e1/implementation-render/" + document.ID + "/" + filepath.Base(pagePath)
				rendered.Pages = append(rendered.Pages, ParserRenderedPageV1{
					Page: index + 1, Kind: "png", RelativePath: fmt.Sprintf("%s/page-%03d.png", document.ID, index+1),
					Artifact: ParserFileIdentityV1{Path: artifactPath, Bytes: int64(len(contents)), SHA256: sha256Hex(contents)},
				})
			}
			if document.Extension == "docx" {
				intermediatePath := ParserCorpusRepositoryPath + "/e1/implementation-render/" + document.ID + "/intermediate.pdf"
				intermediate, err := parserFileIdentity(corpusRoot, intermediatePath)
				if err != nil {
					return ParserFileIdentityV1{}, fmt.Errorf("parser_docx_intermediate_missing: %s: %w", document.ID, err)
				}
				contents, err := readParserRelativeFile(corpusRoot, intermediatePath)
				if err != nil || !bytes.HasPrefix(contents, []byte("%PDF-")) {
					return ParserFileIdentityV1{}, fmt.Errorf("parser_docx_intermediate_invalid: %s", document.ID)
				}
				rendered.IntermediateArtifacts = []ParserFileIdentityV1{intermediate}
			}
		}
		pageRows := parserRenderRows(rendered)
		rendered.PageManifestSHA256 = hashTextRows(pageRows)
		allRows = append(allRows, pageRows...)
		documents = append(documents, rendered)
	}
	manifest := ParserRenderManifestV1{
		SchemaVersion: 1, CorpusVersion: "v1", Provenance: provenance, Documents: documents,
		AggregateSHA256: hashTextRows(allRows),
	}
	manifestPath := filepath.Join(corpusRoot, "e1", "implementation-render-manifest.json")
	if err := writeDeterministicJSON(manifestPath, manifest); err != nil {
		return ParserFileIdentityV1{}, err
	}
	renderIdentity, err := parserFileIdentity(corpusRoot, ParserCorpusRepositoryPath+"/e1/implementation-render-manifest.json")
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	groundTruthIdentity, err := parserFileIdentity(corpusRoot, ParserCorpusRepositoryPath+"/ground-truth.json")
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	visualChecklist, err := parserFileIdentity(corpusRoot, ParserCorpusRepositoryPath+"/e1/visual-checklist.md")
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	implementationVisualReview, err := parserFileIdentity(corpusRoot, ParserCorpusRepositoryPath+"/e1/implementation-visual-review.md")
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	submission := ParserE1SubmissionV1{
		SchemaVersion: 1, Gate: "E1", Status: "pending_independent_audit",
		Hashes: ParserApprovalHashesV1{
			CorpusManifestSHA256: loaded.CorpusManifestFileSHA256, RenderManifestSHA256: manifest.AggregateSHA256,
			GroundTruthPayloadSHA256: loaded.GroundTruthPayloadSHA256, ExtractionRulesSHA256: loaded.ExtractionRulesSHA256,
		},
		RenderManifestFile: renderIdentity, GroundTruthFile: groundTruthIdentity, RenderInput: loaded.Manifest.RenderInputManifest,
		Sources: loaded.Manifest.Sources, Documents: loaded.Manifest.Documents, VisualChecklist: visualChecklist,
		ImplementationVisualReview: implementationVisualReview,
		AuditorTaskID:              ParserE1AuditorTaskID,
		NonCircularContract:        "corpus hash covers corpus-manifest.json bytes; render hash covers only the sorted rendered-page rows in implementation-render-manifest.json; DOCX intermediate PDF identities are single-run provenance excluded from the render hash; render-manifest file bytes have a separate identity; ground-truth hash covers only ground-truth.payload canonical JSON; extraction-rules hash covers extraction-rules.json bytes; review metadata and receipt are excluded from the ground-truth payload hash",
	}
	if err := writeDeterministicJSON(filepath.Join(corpusRoot, "e1", "submission.json"), submission); err != nil {
		return ParserFileIdentityV1{}, err
	}
	return renderIdentity, nil
}

func validateParserRenderManifest(corpusRoot string, ref ParserFileRefV1, corpus *LoadedParserCorpus) (*ParserRenderManifestV1, error) {
	contents, err := readParserRelativeFile(corpusRoot, ref.Path)
	if err != nil {
		return nil, fmt.Errorf("parser_render_manifest_missing: %w", err)
	}
	if sha256Hex(contents) != ref.SHA256 {
		return nil, fmt.Errorf("reference_hash_mismatch: %s", ref.Path)
	}
	var manifest ParserRenderManifestV1
	if err := decodeStrictJSON(contents, &manifest); err != nil {
		return nil, fmt.Errorf("parser_render_manifest_invalid: %w", err)
	}
	if manifest.SchemaVersion != 1 || manifest.CorpusVersion != "v1" || len(manifest.Documents) != len(corpus.Manifest.Documents) {
		return nil, fmt.Errorf("parser_render_manifest_invalid")
	}
	allRows := make([]string, 0)
	for index, document := range manifest.Documents {
		expected := corpus.Manifest.Documents[index]
		if document.ID != expected.ID || document.InputSHA256 != expected.SHA256 || len(document.Pages) == 0 {
			return nil, fmt.Errorf("parser_render_manifest_document_mismatch")
		}
		if expected.Extension == "md" && document.InspectionMode != "source_text" {
			return nil, fmt.Errorf("parser_render_manifest_mode_mismatch")
		}
		if expected.Extension != "md" && document.InspectionMode != "paged_render" {
			return nil, fmt.Errorf("parser_render_manifest_mode_mismatch")
		}
		for pageIndex, page := range document.Pages {
			if page.Page != pageIndex+1 || page.RelativePath == "" {
				return nil, fmt.Errorf("parser_render_manifest_page_order_invalid")
			}
			if err := verifyParserIdentity(corpusRoot, page.Artifact); err != nil {
				return nil, fmt.Errorf("parser_render_artifact_hash_mismatch: %w", err)
			}
		}
		if expected.Extension == "docx" {
			if len(document.IntermediateArtifacts) != 1 || !strings.HasSuffix(document.IntermediateArtifacts[0].Path, "/intermediate.pdf") {
				return nil, fmt.Errorf("parser_docx_intermediate_invalid: %s", document.ID)
			}
			if err := verifyParserIdentity(corpusRoot, document.IntermediateArtifacts[0]); err != nil {
				return nil, fmt.Errorf("parser_docx_intermediate_hash_mismatch: %w", err)
			}
		} else if len(document.IntermediateArtifacts) != 0 {
			return nil, fmt.Errorf("parser_render_intermediate_unexpected: %s", document.ID)
		}
		rows := parserRenderRows(document)
		if hashTextRows(rows) != document.PageManifestSHA256 {
			return nil, fmt.Errorf("parser_render_page_manifest_hash_mismatch")
		}
		allRows = append(allRows, rows...)
	}
	if hashTextRows(allRows) != manifest.AggregateSHA256 {
		return nil, fmt.Errorf("parser_render_aggregate_hash_mismatch")
	}
	return &manifest, nil
}

func parserRenderRows(document ParserRenderedDocumentV1) []string {
	rows := make([]string, 0, len(document.Pages))
	for _, page := range document.Pages {
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s\n", page.RelativePath, page.Artifact.Bytes, page.Artifact.SHA256))
	}
	return rows
}

func hashTextRows(rows []string) string {
	digest := sha256.Sum256([]byte(strings.Join(rows, "")))
	return hex.EncodeToString(digest[:])
}

// LoadApprovedParserCorpus validates the frozen corpus and its independent E1
// receipt without importing redo-2's six-table profile/preflight framework.
// The parser benchmark is an isolated optional extension on the 0828 branch.
func LoadApprovedParserCorpus(repositoryRoot string) (*LoadedParserCorpus, error) {
	corpusRoot := filepath.Join(repositoryRoot, filepath.FromSlash(ParserCorpusRepositoryPath))
	loaded, err := LoadParserCorpusDirectory(corpusRoot, false)
	if err != nil {
		return nil, err
	}
	submissionContents, err := os.ReadFile(filepath.Join(corpusRoot, "e1", "submission.json"))
	if err != nil {
		return nil, fmt.Errorf("parser_approval_submission_missing: %w", err)
	}
	var submission ParserE1SubmissionV1
	if err := decodeStrictJSON(submissionContents, &submission); err != nil {
		return nil, fmt.Errorf("parser_approval_submission_invalid: %w", err)
	}
	if submission.SchemaVersion != 1 || submission.Gate != "E1" || submission.AuditorTaskID != ParserE1AuditorTaskID {
		return nil, fmt.Errorf("parser_approval_submission_invalid")
	}
	if submission.Status != "accepted" && submission.Status != "approved" {
		return nil, fmt.Errorf("parser_ground_truth_not_approved")
	}
	if err := verifyParserIdentity(corpusRoot, submission.RenderManifestFile); err != nil {
		return nil, fmt.Errorf("parser_render_manifest_file_hash_mismatch: %w", err)
	}
	renderManifest, err := validateParserRenderManifest(corpusRoot, ParserFileRefV1{
		Path: submission.RenderManifestFile.Path, SHA256: submission.RenderManifestFile.SHA256,
	}, loaded)
	if err != nil {
		return nil, err
	}
	loaded.RenderAggregateSHA256 = renderManifest.AggregateSHA256
	wantHashes := ParserApprovalHashesV1{
		CorpusManifestSHA256:     loaded.CorpusManifestFileSHA256,
		RenderManifestSHA256:     renderManifest.AggregateSHA256,
		GroundTruthPayloadSHA256: loaded.GroundTruthPayloadSHA256,
		ExtractionRulesSHA256:    loaded.ExtractionRulesSHA256,
	}
	if submission.Hashes != wantHashes {
		return nil, fmt.Errorf("parser_approval_submission_hash_mismatch")
	}
	if err := validateParserApproval(repositoryRoot, loaded); err != nil {
		return nil, err
	}
	return loaded, nil
}

func validateParserApproval(repositoryRoot string, corpus *LoadedParserCorpus) error {
	review := corpus.GroundTruth.Review
	if review.Status != "approved" {
		return fmt.Errorf("parser_ground_truth_not_approved")
	}
	if review.ReviewedBy != "independent_auditor" || review.AuditorTaskID != ParserE1AuditorTaskID || review.ReviewedAt == "" || review.ApprovedHashes == nil || review.Receipt == nil {
		return fmt.Errorf("parser_approval_incomplete")
	}
	if _, err := time.Parse(time.RFC3339, review.ReviewedAt); err != nil {
		return fmt.Errorf("parser_approval_incomplete")
	}
	want := ParserApprovalHashesV1{
		CorpusManifestSHA256: corpus.CorpusManifestFileSHA256, RenderManifestSHA256: corpus.RenderAggregateSHA256,
		GroundTruthPayloadSHA256: corpus.GroundTruthPayloadSHA256, ExtractionRulesSHA256: corpus.ExtractionRulesSHA256,
	}
	if *review.ApprovedHashes != want {
		return fmt.Errorf("parser_approval_hash_mismatch")
	}
	if !strings.HasPrefix(review.Receipt.Path, "docs/topic3/audit/") {
		return fmt.Errorf("parser_approval_receipt_invalid")
	}
	receipt, err := readParserRepositoryFile(repositoryRoot, *review.Receipt)
	if err != nil {
		return err
	}
	fields, err := parseApprovalReceipt(receipt)
	if err != nil {
		return fmt.Errorf("parser_approval_receipt_invalid: %w", err)
	}
	wantFields := map[string]string{
		"gate": "E1", "decision": "accepted", "corpus_manifest_sha256": want.CorpusManifestSHA256,
		"render_manifest_sha256": want.RenderManifestSHA256, "ground_truth_payload_sha256": want.GroundTruthPayloadSHA256,
		"extraction_rules_sha256": want.ExtractionRulesSHA256,
	}
	for key, value := range wantFields {
		if fields[key] != value {
			return fmt.Errorf("parser_approval_receipt_invalid: %s", key)
		}
	}
	return nil
}

func readParserRepositoryFile(repositoryRoot string, ref ParserFileRefV1) ([]byte, error) {
	clean := filepath.Clean(filepath.FromSlash(ref.Path))
	if ref.Path == "" || ref.SHA256 == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("parser_approval_receipt_invalid")
	}
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, clean))
	if err != nil {
		return nil, err
	}
	if sha256Hex(contents) != ref.SHA256 {
		return nil, fmt.Errorf("reference_hash_mismatch: %s", ref.Path)
	}
	return contents, nil
}

func parseApprovalReceipt(contents []byte) (map[string]string, error) {
	result := make(map[string]string)
	for _, rawLine := range strings.Split(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "```") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate field %s", key)
		}
		result[key] = value
	}
	return result, nil
}
