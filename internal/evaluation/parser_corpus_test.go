package evaluation

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/stretchr/testify/require"
)

func TestBuildParserCorpusIsByteStableInTwoDirectories(t *testing.T) {
	templateRoot := parserCorpusRepositoryRoot(t)
	first := filepath.Join(t.TempDir(), "v1")
	second := filepath.Join(t.TempDir(), "v1")
	require.NoError(t, BuildParserCorpus(templateRoot, first))
	require.NoError(t, BuildParserCorpus(templateRoot, second))

	firstFiles := corpusFileFacts(t, first)
	secondFiles := corpusFileFacts(t, second)
	require.Equal(t, firstFiles, secondFiles)

	wantDocuments := []string{
		"documents/docx-embedded-image.docx",
		"documents/docx-heading-list-table.docx",
		"documents/md-code-link.md",
		"documents/md-heading-list-table.md",
		"documents/pdf-digital-multicolumn.pdf",
		"documents/pdf-scanned-ocr.pdf",
		"documents/pdf-table-formula.pdf",
	}
	for _, path := range wantDocuments {
		_, exists := firstFiles[path]
		require.True(t, exists, "missing generated document %s", path)
	}
	require.Len(t, wantDocuments, 7)

	for _, path := range wantDocuments[:2] {
		archive, err := zip.OpenReader(filepath.Join(first, filepath.FromSlash(path)))
		require.NoError(t, err, path)
		require.NotEmpty(t, archive.File)
		require.NoError(t, archive.Close())
	}
	for _, path := range wantDocuments[4:] {
		contents, err := os.ReadFile(filepath.Join(first, filepath.FromSlash(path)))
		require.NoError(t, err)
		require.True(t, bytes.HasPrefix(contents, []byte("%PDF-1.4\n")), path)
	}
}

func TestCommittedParserCorpusMatchesAFreshDeterministicBuild(t *testing.T) {
	repositoryCorpus := parserCorpusRepositoryRoot(t)
	fresh := filepath.Join(t.TempDir(), "v1")
	require.NoError(t, BuildParserCorpus(repositoryCorpus, fresh))

	for relative, want := range corpusFileFacts(t, fresh) {
		contents, err := os.ReadFile(filepath.Join(repositoryCorpus, filepath.FromSlash(relative)))
		require.NoError(t, err, relative)
		require.Equal(t, want, fmt.Sprintf("%d:%s", len(contents), sha256Hex(contents)), relative)
	}
}

func TestGeneratedParserCorpusContractMatchesTheFrozenRegistry(t *testing.T) {
	templateRoot := parserCorpusRepositoryRoot(t)
	generated := filepath.Join(t.TempDir(), "v1")
	require.NoError(t, BuildParserCorpus(templateRoot, generated))

	loaded, err := LoadParserCorpusDirectory(generated, false)
	require.NoError(t, err)
	require.Len(t, loaded.Manifest.Documents, 7)
	require.Len(t, loaded.GroundTruth.Payload.Documents, 7)
	require.Equal(t, int64(56), loaded.TotalCells)
	require.Equal(t, int64(37), loaded.SupportedCells)
	require.Equal(t, int64(19), loaded.UnsupportedCells)
	require.LessOrEqual(t, loaded.TotalDocumentBytes, int64(5*1024*1024))
	require.Equal(t, loaded.GroundTruthPayloadSHA256, loaded.Manifest.GroundTruthPayloadSHA256)
	require.Equal(t, ParserMetricContractVersion, loaded.GroundTruth.Payload.ParserMetricContractVersion)
	require.Equal(t, ParserNormalizationVersion, loaded.GroundTruth.Payload.ParserNormalizationVersion)

	registry := docparser.RegisteredEngineFileTypesSnapshot(false)
	require.Len(t, registry, 8)
	for engineIndex, row := range loaded.Manifest.SupportMatrix {
		require.Equal(t, registry[engineIndex].Name, row.Engine)
		for documentIndex, cell := range row.Documents {
			extension := loaded.Manifest.Documents[documentIndex].Extension
			require.Equal(t, containsParserFileType(registry[engineIndex].FileTypes, extension), cell.Supported,
				"engine=%s document=%s extension=%s", row.Engine, cell.DocumentID, extension)
		}
	}

	for _, document := range loaded.GroundTruth.Payload.Documents {
		require.GreaterOrEqual(t, len(document.Components.AnchorMatches), 3, document.ID)
		for _, anchor := range document.Components.AnchorMatches {
			require.Equal(t, 1, anchor.Count, "%s anchor %q must be unique in ground truth", document.ID, anchor.Text)
		}
	}
	require.Equal(t, "pending", loaded.GroundTruth.Review.Status)
	require.Equal(t, "not_reviewed", loaded.GroundTruth.Review.Reason)
}

func parserCorpusRepositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "dataset", "parser_benchmark", "v1"))
	require.NoError(t, err)
	return root
}

func corpusFileFacts(t *testing.T, root string) map[string]string {
	t.Helper()
	facts := make(map[string]string)
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		facts[filepath.ToSlash(relative)] = fmt.Sprintf("%d:%s", len(contents), sha256Hex(contents))
		return nil
	}))
	return facts
}

func TestParserCorpusJSONHasStableArrayOrder(t *testing.T) {
	templateRoot := parserCorpusRepositoryRoot(t)
	generated := filepath.Join(t.TempDir(), "v1")
	require.NoError(t, BuildParserCorpus(templateRoot, generated))
	contents, err := os.ReadFile(filepath.Join(generated, "corpus-manifest.json"))
	require.NoError(t, err)
	var manifest ParserCorpusManifestV1
	require.NoError(t, json.Unmarshal(contents, &manifest))
	documentIDs := make([]string, 0, len(manifest.Documents))
	for _, document := range manifest.Documents {
		documentIDs = append(documentIDs, document.ID)
	}
	require.True(t, sort.StringsAreSorted(documentIDs), strings.Join(documentIDs, ","))
	for _, row := range manifest.SupportMatrix {
		for index, cell := range row.Documents {
			require.Equal(t, documentIDs[index], cell.DocumentID)
		}
	}
}

func TestParserCorpusStrictJSONRejectsTrailingValues(t *testing.T) {
	var value struct {
		SchemaVersion int `json:"schema_version"`
	}
	require.Error(t, decodeStrictJSON([]byte(`{"schema_version":1}{"schema_version":2}`), &value))
}
