package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
)

const (
	ParserCorpusSchemaVersion        = 1
	ParserGroundTruthSchemaVersion   = 1
	ParserNormalizationVersion       = 1
	ParserCorpusRepositoryPath       = "dataset/parser_benchmark/v1"
	ParserCorpusMaximumDocumentBytes = int64(5 * 1024 * 1024)
)

var FrozenParserEngineOrder = []string{
	"builtin", "simple", "anydoc", "weknoracloud", "mineru", "mineru_cloud", "paddleocr_vl", "paddleocr_vl_cloud",
}

type ParserFileIdentityV1 struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type ParserGeneratorIdentityV1 struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

type ParserCorpusDocumentV1 struct {
	ID         string                    `json:"id"`
	Path       string                    `json:"path"`
	Extension  string                    `json:"extension"`
	Bytes      int64                     `json:"bytes"`
	SHA256     string                    `json:"sha256"`
	Attributes []string                  `json:"attributes"`
	Sources    []string                  `json:"sources"`
	Generator  ParserGeneratorIdentityV1 `json:"generator"`
}

type ParserSupportCellV1 struct {
	DocumentID string `json:"document_id"`
	Supported  bool   `json:"supported"`
}

type ParserSupportRowV1 struct {
	Engine    string                `json:"engine"`
	Documents []ParserSupportCellV1 `json:"documents"`
}

type ParserCorpusManifestV1 struct {
	SchemaVersion            int                      `json:"schema_version"`
	Name                     string                   `json:"name"`
	Version                  string                   `json:"version"`
	GeneratorVersion         string                   `json:"generator_version"`
	SampleIDRule             string                   `json:"sample_id_rule"`
	SortRule                 string                   `json:"sort_rule"`
	Sources                  []ParserFileIdentityV1   `json:"sources"`
	Documents                []ParserCorpusDocumentV1 `json:"documents"`
	GroundTruthPath          string                   `json:"ground_truth_path"`
	GroundTruthPayloadSHA256 string                   `json:"ground_truth_payload_sha256"`
	ExtractionRules          ParserFileIdentityV1     `json:"extraction_rules"`
	RenderInputManifest      ParserFileIdentityV1     `json:"render_input_manifest"`
	SupportMatrix            []ParserSupportRowV1     `json:"support_matrix"`
}

type ParserGroundTruthDocumentV1 struct {
	ID                         string             `json:"id"`
	Components                 ParserComponentsV1 `json:"components"`
	ExpectedEmbeddedImageCount int                `json:"expected_embedded_image_count"`
}

type ParserGroundTruthPayloadV1 struct {
	CorpusVersion               string                        `json:"corpus_version"`
	ParserNormalizationVersion  int                           `json:"parser_normalization_version"`
	ParserMetricContractVersion int                           `json:"parser_metric_contract_version"`
	Documents                   []ParserGroundTruthDocumentV1 `json:"documents"`
}

type ParserApprovalHashesV1 struct {
	CorpusManifestSHA256     string `json:"corpus_manifest_sha256"`
	RenderManifestSHA256     string `json:"render_manifest_sha256"`
	GroundTruthPayloadSHA256 string `json:"ground_truth_payload_sha256"`
	ExtractionRulesSHA256    string `json:"extraction_rules_sha256"`
}

type ParserFileRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ParserApprovalReviewV1 struct {
	Status         string                  `json:"status"`
	Reason         string                  `json:"reason,omitempty"`
	ReviewedBy     string                  `json:"reviewed_by,omitempty"`
	AuditorTaskID  string                  `json:"auditor_task_id,omitempty"`
	ReviewedAt     string                  `json:"reviewed_at,omitempty"`
	ApprovedHashes *ParserApprovalHashesV1 `json:"approved_hashes,omitempty"`
	Receipt        *ParserFileRefV1        `json:"receipt,omitempty"`
}

type ParserGroundTruthV1 struct {
	SchemaVersion int                        `json:"schema_version"`
	Payload       ParserGroundTruthPayloadV1 `json:"payload"`
	Review        ParserApprovalReviewV1     `json:"review"`
}

type LoadedParserCorpus struct {
	Manifest                  ParserCorpusManifestV1
	GroundTruth               ParserGroundTruthV1
	GroundTruthPayloadSHA256  string
	CorpusManifestFileSHA256  string
	RenderInputManifestSHA256 string
	RenderAggregateSHA256     string
	ExtractionRulesSHA256     string
	TotalDocumentBytes        int64
	TotalCells                int64
	SupportedCells            int64
	UnsupportedCells          int64
}

func LoadParserCorpusDirectory(corpusRoot string, requireApproval bool) (*LoadedParserCorpus, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(corpusRoot, "corpus-manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("parser_corpus_manifest_missing: %w", err)
	}
	var manifest ParserCorpusManifestV1
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parser_corpus_manifest_invalid: %w", err)
	}
	groundTruthBytes, err := os.ReadFile(filepath.Join(corpusRoot, "ground-truth.json"))
	if err != nil {
		return nil, fmt.Errorf("parser_ground_truth_missing: %w", err)
	}
	var groundTruth ParserGroundTruthV1
	if err := decodeStrictJSON(groundTruthBytes, &groundTruth); err != nil {
		return nil, fmt.Errorf("parser_ground_truth_invalid: %w", err)
	}
	loaded := &LoadedParserCorpus{
		Manifest:                 manifest,
		GroundTruth:              groundTruth,
		CorpusManifestFileSHA256: sha256Hex(manifestBytes),
	}
	if err := validateParserCorpusDirectory(corpusRoot, loaded); err != nil {
		return nil, err
	}
	if requireApproval && groundTruth.Review.Status != "approved" {
		return nil, fmt.Errorf("parser_ground_truth_not_approved")
	}
	return loaded, nil
}

func validateParserCorpusDirectory(corpusRoot string, loaded *LoadedParserCorpus) error {
	manifest := &loaded.Manifest
	groundTruth := &loaded.GroundTruth
	if manifest.SchemaVersion != ParserCorpusSchemaVersion || manifest.Name != "topic3-parser-benchmark" || manifest.Version != "v1" || manifest.SampleIDRule != "parser:<corpus_document_id>" || manifest.SortRule != "document_id_ascending" {
		return fmt.Errorf("parser_corpus_manifest_invalid")
	}
	if groundTruth.SchemaVersion != ParserGroundTruthSchemaVersion || groundTruth.Payload.CorpusVersion != "v1" || groundTruth.Payload.ParserNormalizationVersion != ParserNormalizationVersion || groundTruth.Payload.ParserMetricContractVersion != ParserMetricContractVersion {
		return fmt.Errorf("parser_ground_truth_invalid")
	}
	if len(manifest.Documents) != 7 || len(groundTruth.Payload.Documents) != 7 {
		return fmt.Errorf("parser_document_count_mismatch")
	}
	documentIDs := make([]string, len(manifest.Documents))
	formatCounts := map[string]int{}
	for i, document := range manifest.Documents {
		documentIDs[i] = document.ID
		if i > 0 && documentIDs[i-1] >= document.ID {
			return fmt.Errorf("parser_documents_not_sorted")
		}
		if filepath.Ext(document.Path) != "."+document.Extension || (document.Extension != "pdf" && document.Extension != "docx" && document.Extension != "md") {
			return fmt.Errorf("parser_document_extension_invalid: %s", document.ID)
		}
		formatCounts[document.Extension]++
		contents, err := readParserRelativeFile(corpusRoot, document.Path)
		if err != nil {
			return fmt.Errorf("parser_document_missing: %s: %w", document.ID, err)
		}
		if int64(len(contents)) != document.Bytes || sha256Hex(contents) != document.SHA256 {
			return fmt.Errorf("parser_corpus_hash_mismatch: %s", document.ID)
		}
		if document.Bytes <= 0 || document.Bytes > ParserCorpusMaximumDocumentBytes {
			return fmt.Errorf("parser_document_size_invalid: %s", document.ID)
		}
		loaded.TotalDocumentBytes += document.Bytes
	}
	if formatCounts["pdf"] != 3 || formatCounts["docx"] != 2 || formatCounts["md"] != 2 || loaded.TotalDocumentBytes > ParserCorpusMaximumDocumentBytes {
		return fmt.Errorf("parser_corpus_format_or_size_invalid")
	}
	groundTruthIDs := make([]string, len(groundTruth.Payload.Documents))
	for i, document := range groundTruth.Payload.Documents {
		groundTruthIDs[i] = document.ID
		if len(document.Components.AnchorMatches) < 3 {
			return fmt.Errorf("parser_ground_truth_anchor_count_invalid: %s", document.ID)
		}
		seenAnchors := make(map[string]struct{}, len(document.Components.AnchorMatches))
		for _, anchor := range document.Components.AnchorMatches {
			if anchor.Text == "" || anchor.Count != 1 || anchor.RuneOffset < 0 {
				return fmt.Errorf("parser_ground_truth_anchor_invalid: %s", document.ID)
			}
			if _, duplicate := seenAnchors[anchor.Text]; duplicate {
				return fmt.Errorf("parser_ground_truth_anchor_duplicate: %s", document.ID)
			}
			seenAnchors[anchor.Text] = struct{}{}
		}
		if len(document.Components.Formulas) > 1 || tableCount(document.Components.TableCells) > 1 {
			return fmt.Errorf("parser_ground_truth_component_limit: %s", document.ID)
		}
	}
	if !equalStrings(documentIDs, groundTruthIDs) {
		return fmt.Errorf("parser_ground_truth_document_mismatch")
	}
	payloadHash, err := CanonicalSHA256(groundTruth.Payload)
	if err != nil {
		return err
	}
	loaded.GroundTruthPayloadSHA256 = payloadHash
	if payloadHash != manifest.GroundTruthPayloadSHA256 {
		return fmt.Errorf("parser_ground_truth_hash_mismatch")
	}
	if manifest.GroundTruthPath != ParserCorpusRepositoryPath+"/ground-truth.json" {
		return fmt.Errorf("parser_ground_truth_path_invalid")
	}
	if err := verifyParserIdentity(corpusRoot, manifest.ExtractionRules); err != nil {
		return fmt.Errorf("parser_extraction_rules_hash_mismatch: %w", err)
	}
	loaded.ExtractionRulesSHA256 = manifest.ExtractionRules.SHA256
	if err := verifyParserIdentity(corpusRoot, manifest.RenderInputManifest); err != nil {
		return fmt.Errorf("parser_render_input_hash_mismatch: %w", err)
	}
	loaded.RenderInputManifestSHA256 = manifest.RenderInputManifest.SHA256
	for _, source := range manifest.Sources {
		if err := verifyParserIdentity(corpusRoot, source); err != nil {
			return fmt.Errorf("parser_source_hash_mismatch: %w", err)
		}
	}
	if err := validateParserSupportMatrix(manifest.Documents, manifest.SupportMatrix, loaded); err != nil {
		return err
	}
	return nil
}

func validateParserSupportMatrix(documents []ParserCorpusDocumentV1, matrix []ParserSupportRowV1, loaded *LoadedParserCorpus) error {
	registry := docparser.RegisteredEngineFileTypesSnapshot(false)
	if len(registry) != len(FrozenParserEngineOrder) || len(matrix) != len(FrozenParserEngineOrder) {
		return fmt.Errorf("parser_support_matrix_engine_count_mismatch")
	}
	for engineIndex, expectedEngine := range FrozenParserEngineOrder {
		if registry[engineIndex].Name != expectedEngine || matrix[engineIndex].Engine != expectedEngine || len(matrix[engineIndex].Documents) != len(documents) {
			return fmt.Errorf("parser_support_matrix_engine_mismatch: %s", expectedEngine)
		}
		for documentIndex, document := range documents {
			cell := matrix[engineIndex].Documents[documentIndex]
			expected := containsParserFileType(registry[engineIndex].FileTypes, document.Extension)
			if cell.DocumentID != document.ID || cell.Supported != expected {
				return fmt.Errorf("parser_support_matrix_mismatch: %s/%s", expectedEngine, document.ID)
			}
			loaded.TotalCells++
			if cell.Supported {
				loaded.SupportedCells++
			} else {
				loaded.UnsupportedCells++
			}
		}
	}
	if loaded.TotalCells != 56 || loaded.SupportedCells != 37 || loaded.UnsupportedCells != 19 {
		return fmt.Errorf("parser_support_matrix_totals_mismatch")
	}
	return nil
}

func containsParserFileType(fileTypes []string, extension string) bool {
	for _, fileType := range fileTypes {
		if strings.EqualFold(strings.TrimPrefix(fileType, "."), strings.TrimPrefix(extension, ".")) {
			return true
		}
	}
	return false
}

func verifyParserIdentity(corpusRoot string, identity ParserFileIdentityV1) error {
	contents, err := readParserRelativeFile(corpusRoot, identity.Path)
	if err != nil {
		return err
	}
	if int64(len(contents)) != identity.Bytes || sha256Hex(contents) != identity.SHA256 {
		return fmt.Errorf("identity mismatch for %s", identity.Path)
	}
	return nil
}

func readParserRelativeFile(corpusRoot, repositoryPath string) ([]byte, error) {
	prefix := ParserCorpusRepositoryPath + "/"
	if !strings.HasPrefix(repositoryPath, prefix) {
		return nil, fmt.Errorf("path is not under frozen corpus root: %s", repositoryPath)
	}
	relative := strings.TrimPrefix(repositoryPath, prefix)
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe parser corpus path: %s", repositoryPath)
	}
	return os.ReadFile(filepath.Join(corpusRoot, clean))
}

func decodeStrictJSON(contents []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

// CanonicalSHA256 hashes encoding/json's stable representation of structs and
// arrays. Parser benchmark contracts avoid maps in hashed payloads.
func CanonicalSHA256(value any) (string, error) {
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("canonical JSON: %w", err)
	}
	return sha256Hex(canonical), nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func parserFileIdentity(corpusRoot, repositoryPath string) (ParserFileIdentityV1, error) {
	contents, err := readParserRelativeFile(corpusRoot, repositoryPath)
	if err != nil {
		return ParserFileIdentityV1{}, err
	}
	return ParserFileIdentityV1{Path: repositoryPath, Bytes: int64(len(contents)), SHA256: sha256Hex(contents)}, nil
}

func writeDeterministicJSON(path string, value any) error {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	contents = append(contents, '\n')
	return os.WriteFile(path, contents, 0o644)
}

func tableCount(cells []ParserTableCellV1) int {
	set := make(map[int]struct{})
	for _, cell := range cells {
		set[cell.Table] = struct{}{}
	}
	return len(set)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
