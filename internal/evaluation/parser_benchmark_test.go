package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestRunParserBenchmarkProducesTheFrozenMatrixAndFactualOutcomes(t *testing.T) {
	corpusRoot := parserCorpusRepositoryRoot(t)
	corpus, err := LoadParserCorpusDirectory(corpusRoot, false)
	require.NoError(t, err)

	engines := benchmarkTestEngines(corpus)
	for index := range engines {
		if engines[index].Name == "paddleocr_vl_cloud" {
			engines[index].Available = false
			engines[index].UnavailableReason = "credential not configured"
		}
	}

	report, err := RunParserBenchmark(context.Background(), corpus, ParserBenchmarkOptions{
		CorpusRoot:     corpusRoot,
		RunID:          "parser-test-run",
		GeneratedAt:    time.Date(2026, 9, 12, 3, 4, 5, 0, time.UTC),
		PerCellTimeout: time.Second,
		Engines:        engines,
		NewReader: func(_ context.Context, engine, _ string) (interfaces.DocReader, error) {
			return parserBenchmarkReaderFunc(func(_ context.Context, request *types.ReadRequest) (*types.ReadResult, error) {
				if engine == "builtin" && request.FileName == "pdf-scanned-ocr.pdf" {
					return nil, errors.New("deliberate test failure")
				}
				metadata := map[string]string{"parser": engine}
				if engine == "anydoc" && request.FileName == "pdf-scanned-ocr.pdf" {
					metadata["parser"] = "builtin"
					metadata["anydoc_fallback"] = "scanned_pdf"
				}
				return &types.ReadResult{
					MarkdownContent: "# TEST OUTPUT\n\nNORTH PINE CENTER RIVER SOUTH STONE\n\nsecret-output-must-not-be-persisted",
					Metadata:        metadata,
				}, nil
			}), nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, ParserBenchmarkStatusPartial, report.Status)
	require.Equal(t, 56, report.CellCount)
	require.Equal(t, 19, report.UnsupportedCellCount)
	require.Equal(t, 3, report.UnavailableCellCount)
	require.Equal(t, 34, report.AttemptedCellCount)
	require.Equal(t, 33, report.ScoredCellCount)
	require.Equal(t, 1, report.FailedCellCount)
	require.Equal(t, 7, report.ReadyEngineCount)
	require.Len(t, report.Cells, 56)

	require.Equal(t, "parser:builtin:docx-embedded-image", report.Cells[0].SampleID)
	require.Equal(t, "builtin", report.Cells[0].RequestedEngine)
	require.Equal(t, "docx-embedded-image", report.Cells[0].DocumentID)

	fallback := requireParserCell(t, report, "anydoc", "pdf-scanned-ocr")
	require.Equal(t, ParserBenchmarkOutcomeFallbackSucceeded, fallback.Outcome)
	require.Equal(t, "builtin", fallback.ExecutedEngine)
	require.Equal(t, "scanned_pdf", fallback.FallbackReason)
	require.Equal(t, ParserBenchmarkBillingUnknown, fallback.BillingStatus)
	require.NotEmpty(t, fallback.OutputSHA256)
	require.NotNil(t, fallback.Metrics)

	failed := requireParserCell(t, report, "builtin", "pdf-scanned-ocr")
	require.Equal(t, ParserBenchmarkOutcomeReadFailed, failed.Outcome)
	require.Equal(t, "parser_read_failed", failed.ErrorCode)
	require.Equal(t, ParserBenchmarkBillingUnknown, failed.BillingStatus)

	unavailable := requireParserCell(t, report, "paddleocr_vl_cloud", "pdf-scanned-ocr")
	require.Equal(t, ParserBenchmarkOutcomeUnavailable, unavailable.Outcome)
	require.Equal(t, ParserBenchmarkBillingNotBillable, unavailable.BillingStatus)

	unsupported := requireParserCell(t, report, "simple", "pdf-scanned-ocr")
	require.Equal(t, ParserBenchmarkOutcomeUnsupported, unsupported.Outcome)
	require.Equal(t, ParserBenchmarkBillingNotBillable, unsupported.BillingStatus)

	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-output-must-not-be-persisted")
}

func TestRunParserBenchmarkClassifiesContextDeadlineAsParseTimeout(t *testing.T) {
	corpusRoot := parserCorpusRepositoryRoot(t)
	corpus, err := LoadParserCorpusDirectory(corpusRoot, false)
	require.NoError(t, err)
	engines := benchmarkTestEngines(corpus)
	for index := range engines {
		engines[index].Available = engines[index].Name == "builtin"
	}

	report, err := RunParserBenchmark(context.Background(), corpus, ParserBenchmarkOptions{
		CorpusRoot:     corpusRoot,
		RunID:          "parser-timeout-run",
		GeneratedAt:    time.Date(2026, 9, 12, 3, 4, 5, 0, time.UTC),
		PerCellTimeout: time.Millisecond,
		Engines:        engines,
		NewReader: func(_ context.Context, _ string, _ string) (interfaces.DocReader, error) {
			return parserBenchmarkReaderFunc(func(ctx context.Context, _ *types.ReadRequest) (*types.ReadResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}), nil
		},
	})
	require.NoError(t, err)

	cell := requireParserCell(t, report, "builtin", "pdf-scanned-ocr")
	require.Equal(t, ParserBenchmarkOutcomeParseTimeout, cell.Outcome)
	require.Equal(t, "parser_timeout", cell.ErrorCode)
	require.Equal(t, ParserBenchmarkBillingUnknown, cell.BillingStatus)
}

func TestRunParserBenchmarkKeepsReadyEngineAvailableWhenDeadlinePreventsFirstAttempt(t *testing.T) {
	corpusRoot := parserCorpusRepositoryRoot(t)
	corpus, err := LoadParserCorpusDirectory(corpusRoot, false)
	require.NoError(t, err)
	engines := benchmarkTestEngines(corpus)
	for index := range engines {
		engines[index].Available = engines[index].Name == "builtin"
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := RunParserBenchmark(ctx, corpus, ParserBenchmarkOptions{
		CorpusRoot: corpusRoot, RunID: "parser-cancelled-before-first-attempt",
		GeneratedAt: time.Date(2026, 9, 12, 3, 4, 5, 0, time.UTC), PerCellTimeout: time.Second,
		Engines: engines,
		NewReader: func(context.Context, string, string) (interfaces.DocReader, error) {
			require.FailNow(t, "reader must not be created after the overall deadline")
			return nil, nil
		},
	})
	require.NoError(t, err)
	require.Equal(t, 1, report.ReadyEngineCount)
	require.Equal(t, []string{"builtin"}, report.AvailableEngines)
	for _, engine := range report.Engines {
		if engine.RequestedEngine == "builtin" {
			require.True(t, engine.Available)
			require.Zero(t, engine.AttemptedCellCount)
			require.Equal(t, engine.SupportedCellCount, engine.NotAttemptedCellCount)
			return
		}
	}
	require.FailNow(t, "builtin summary not found")
}

type parserBenchmarkReaderFunc func(context.Context, *types.ReadRequest) (*types.ReadResult, error)

func (function parserBenchmarkReaderFunc) Read(ctx context.Context, request *types.ReadRequest) (*types.ReadResult, error) {
	return function(ctx, request)
}

func benchmarkTestEngines(corpus *LoadedParserCorpus) []types.ParserEngineInfo {
	engines := make([]types.ParserEngineInfo, 0, len(corpus.Manifest.SupportMatrix))
	for _, row := range corpus.Manifest.SupportMatrix {
		fileTypes := make([]string, 0)
		for index, cell := range row.Documents {
			if cell.Supported {
				fileTypes = append(fileTypes, corpus.Manifest.Documents[index].Extension)
			}
		}
		engines = append(engines, types.ParserEngineInfo{Name: row.Engine, FileTypes: fileTypes, Available: true})
	}
	return engines
}

func requireParserCell(t *testing.T, report *ParserBenchmarkReportV1, engine, document string) ParserBenchmarkCellV1 {
	t.Helper()
	for _, cell := range report.Cells {
		if cell.RequestedEngine == engine && cell.DocumentID == document {
			return cell
		}
	}
	require.FailNow(t, "parser benchmark cell not found", "engine=%s document=%s", engine, document)
	return ParserBenchmarkCellV1{}
}

func TestWriteParserBenchmarkReportsCreatesJSONAndMarkdown(t *testing.T) {
	report := &ParserBenchmarkReportV1{
		SchemaVersion: 1,
		RunID:         "run-1",
		RunKind:       "parser_benchmark",
		GeneratedAt:   "2026-09-12T03:04:05Z",
		Status:        ParserBenchmarkStatusPartial,
		EngineCount:   8,
		CellCount:     56,
		Cells:         []ParserBenchmarkCellV1{},
	}
	root := t.TempDir()
	jsonPath := filepath.Join(root, "parser-report.json")
	markdownPath := filepath.Join(root, "parser-report.md")
	require.NoError(t, WriteParserBenchmarkReports(report, jsonPath, markdownPath))

	jsonBytes, err := filepath.Abs(jsonPath)
	require.NoError(t, err)
	require.FileExists(t, jsonBytes)
	require.FileExists(t, markdownPath)
}
