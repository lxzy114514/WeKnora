package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const ParserBenchmarkReportSchemaVersion = 1

type ParserBenchmarkReaderFactory func(context.Context, string, string) (interfaces.DocReader, error)

type ParserBenchmarkOptions struct {
	CorpusRoot     string
	RunID          string
	GeneratedAt    time.Time
	PerCellTimeout time.Duration
	Engines        []types.ParserEngineInfo
	NewReader      ParserBenchmarkReaderFactory
}

type ParserBenchmarkCellV1 struct {
	SampleID        string             `json:"sample_id"`
	RequestedEngine string             `json:"requested_engine"`
	ExecutedEngine  string             `json:"executed_engine,omitempty"`
	FallbackReason  string             `json:"fallback_reason,omitempty"`
	DocumentID      string             `json:"document_id"`
	DocumentPath    string             `json:"document_path"`
	Extension       string             `json:"extension"`
	Supported       bool               `json:"supported"`
	Outcome         string             `json:"outcome"`
	ErrorCode       string             `json:"error_code,omitempty"`
	DurationMillis  int64              `json:"duration_ms,omitempty"`
	OutputSHA256    string             `json:"output_sha256,omitempty"`
	BillingStatus   string             `json:"billing_status"`
	Metrics         *ParserMetricSetV1 `json:"metrics,omitempty"`
}

type ParserMetricMeanV1 struct {
	ApplicableCellCount int      `json:"applicable_cell_count"`
	Value               *float64 `json:"value"`
}

type ParserMetricMeanSetV1 struct {
	TextSimilarity       ParserMetricMeanV1 `json:"text_similarity"`
	StructureF1          ParserMetricMeanV1 `json:"structure_f1"`
	TableCellF1          ParserMetricMeanV1 `json:"table_cell_f1"`
	FormulaSimilarity    ParserMetricMeanV1 `json:"formula_similarity"`
	ReadingOrderAccuracy ParserMetricMeanV1 `json:"reading_order_accuracy"`
}

type ParserBenchmarkEngineSummaryV1 struct {
	RequestedEngine       string                `json:"requested_engine"`
	Available             bool                  `json:"available"`
	SupportedCellCount    int                   `json:"supported_cell_count"`
	AttemptedCellCount    int                   `json:"attempted_cell_count"`
	NotAttemptedCellCount int                   `json:"not_attempted_cell_count"`
	ScoredCellCount       int                   `json:"scored_cell_count"`
	FailedCellCount       int                   `json:"failed_cell_count"`
	UnavailableCellCount  int                   `json:"unavailable_cell_count"`
	ExecutedEngines       []string              `json:"executed_engines"`
	MetricMeans           ParserMetricMeanSetV1 `json:"metric_means"`
}

type ParserBenchmarkReportV1 struct {
	SchemaVersion            int                              `json:"schema_version"`
	RunID                    string                           `json:"run_id"`
	RunKind                  string                           `json:"run_kind"`
	GeneratedAt              string                           `json:"generated_at"`
	Status                   string                           `json:"status"`
	CorpusVersion            string                           `json:"corpus_version"`
	CorpusManifestSHA256     string                           `json:"corpus_manifest_sha256"`
	GroundTruthPayloadSHA256 string                           `json:"ground_truth_payload_sha256"`
	ExtractionRulesSHA256    string                           `json:"extraction_rules_sha256"`
	NormalizationVersion     int                              `json:"normalization_version"`
	MetricContractVersion    int                              `json:"metric_contract_version"`
	EngineCount              int                              `json:"engine_count"`
	ReadyEngineCount         int                              `json:"ready_engine_count"`
	AvailableEngines         []string                         `json:"available_engines"`
	CellCount                int                              `json:"cell_count"`
	SupportedCellCount       int                              `json:"supported_cell_count"`
	UnsupportedCellCount     int                              `json:"unsupported_cell_count"`
	UnavailableCellCount     int                              `json:"unavailable_cell_count"`
	AttemptedCellCount       int                              `json:"attempted_cell_count"`
	NotAttemptedCellCount    int                              `json:"not_attempted_cell_count"`
	ScoredCellCount          int                              `json:"scored_cell_count"`
	FailedCellCount          int                              `json:"failed_cell_count"`
	Engines                  []ParserBenchmarkEngineSummaryV1 `json:"engines"`
	Cells                    []ParserBenchmarkCellV1          `json:"cells"`
}

// RunParserBenchmark executes the frozen engine-by-document matrix. Parsed
// document contents never enter the report: only hashes, metrics, timings, and
// low-cardinality outcome codes are retained.
func RunParserBenchmark(ctx context.Context, corpus *LoadedParserCorpus, options ParserBenchmarkOptions) (*ParserBenchmarkReportV1, error) {
	if corpus == nil {
		return nil, fmt.Errorf("parser_benchmark_corpus_required")
	}
	if strings.TrimSpace(options.CorpusRoot) == "" || strings.TrimSpace(options.RunID) == "" {
		return nil, fmt.Errorf("parser_benchmark_options_invalid")
	}
	if options.PerCellTimeout <= 0 || options.NewReader == nil {
		return nil, fmt.Errorf("parser_benchmark_runtime_invalid")
	}
	if options.GeneratedAt.IsZero() {
		options.GeneratedAt = time.Now().UTC()
	}

	engineInfo := make(map[string]types.ParserEngineInfo, len(options.Engines))
	for _, engine := range options.Engines {
		engineInfo[engine.Name] = engine
	}
	truth := make(map[string]ParserGroundTruthDocumentV1, len(corpus.GroundTruth.Payload.Documents))
	for _, document := range corpus.GroundTruth.Payload.Documents {
		truth[document.ID] = document
	}
	documents := make(map[string]ParserCorpusDocumentV1, len(corpus.Manifest.Documents))
	for _, document := range corpus.Manifest.Documents {
		documents[document.ID] = document
	}

	report := &ParserBenchmarkReportV1{
		SchemaVersion: ParserBenchmarkReportSchemaVersion,
		RunID:         strings.TrimSpace(options.RunID), RunKind: "parser_benchmark",
		GeneratedAt: options.GeneratedAt.UTC().Format(time.RFC3339), Status: "complete",
		CorpusVersion:            corpus.Manifest.Version,
		CorpusManifestSHA256:     corpus.CorpusManifestFileSHA256,
		GroundTruthPayloadSHA256: corpus.GroundTruthPayloadSHA256,
		ExtractionRulesSHA256:    corpus.ExtractionRulesSHA256,
		NormalizationVersion:     corpus.GroundTruth.Payload.ParserNormalizationVersion,
		MetricContractVersion:    corpus.GroundTruth.Payload.ParserMetricContractVersion,
		EngineCount:              len(corpus.Manifest.SupportMatrix),
		AvailableEngines:         make([]string, 0, len(corpus.Manifest.SupportMatrix)),
		Engines:                  make([]ParserBenchmarkEngineSummaryV1, 0, len(corpus.Manifest.SupportMatrix)),
		Cells:                    make([]ParserBenchmarkCellV1, 0, corpus.TotalCells),
	}

	for _, row := range corpus.Manifest.SupportMatrix {
		info, listed := engineInfo[row.Engine]
		available := listed && info.Available
		if available {
			report.ReadyEngineCount++
			report.AvailableEngines = append(report.AvailableEngines, row.Engine)
		}
		for _, support := range row.Documents {
			document, ok := documents[support.DocumentID]
			if !ok {
				return nil, fmt.Errorf("parser_benchmark_document_missing: %s", support.DocumentID)
			}
			groundTruth, ok := truth[support.DocumentID]
			if !ok {
				return nil, fmt.Errorf("parser_benchmark_ground_truth_missing: %s", support.DocumentID)
			}
			cell := ParserBenchmarkCellV1{
				SampleID:        "parser:" + row.Engine + ":" + document.ID,
				RequestedEngine: row.Engine,
				DocumentID:      document.ID, DocumentPath: document.Path, Extension: document.Extension,
				Supported: support.Supported, BillingStatus: "not_billable",
			}
			report.CellCount++
			if !support.Supported {
				cell.Outcome = "unsupported"
				report.UnsupportedCellCount++
				report.Cells = append(report.Cells, cell)
				continue
			}
			report.SupportedCellCount++
			if !available {
				cell.Outcome = "unavailable"
				cell.ErrorCode = "parser_engine_unavailable"
				report.UnavailableCellCount++
				report.Status = "partial"
				report.Cells = append(report.Cells, cell)
				continue
			}
			if ctx.Err() != nil {
				cell.Outcome = "not_attempted"
				cell.ErrorCode = "parser_benchmark_deadline"
				report.NotAttemptedCellCount++
				report.Status = "partial"
				report.Cells = append(report.Cells, cell)
				continue
			}

			report.AttemptedCellCount++
			cell.BillingStatus = "unknown"
			contents, err := readParserRelativeFile(options.CorpusRoot, document.Path)
			if err != nil {
				return nil, fmt.Errorf("parser_benchmark_read_document: %w", err)
			}
			cellContext, cancel := context.WithTimeout(ctx, options.PerCellTimeout)
			started := time.Now()
			reader, err := options.NewReader(cellContext, row.Engine, document.Extension)
			if err == nil {
				request := &types.ReadRequest{
					FileContent: contents, FileName: filepath.Base(filepath.FromSlash(document.Path)),
					FileType: document.Extension, ParserEngine: row.Engine,
					RequestID: report.RunID + ":" + row.Engine + ":" + document.ID,
				}
				var result *types.ReadResult
				result, err = reader.Read(cellContext, request)
				cell.DurationMillis = time.Since(started).Milliseconds()
				if err == nil {
					completeParserBenchmarkCell(&cell, result, groundTruth)
				}
			} else {
				cell.DurationMillis = time.Since(started).Milliseconds()
			}
			deadline := errors.Is(err, context.DeadlineExceeded) || errors.Is(cellContext.Err(), context.DeadlineExceeded)
			cancel()
			if err != nil {
				if deadline {
					cell.Outcome = "parse_timeout"
					cell.ErrorCode = "parser_timeout"
				} else {
					cell.Outcome = "read_failed"
					cell.ErrorCode = "parser_read_failed"
				}
			}
			if cell.Metrics != nil {
				report.ScoredCellCount++
			} else {
				report.FailedCellCount++
				report.Status = "partial"
			}
			report.Cells = append(report.Cells, cell)
		}
	}
	report.Engines = summarizeParserBenchmarkEngines(report)
	return report, nil
}

func completeParserBenchmarkCell(cell *ParserBenchmarkCellV1, result *types.ReadResult, truth ParserGroundTruthDocumentV1) {
	if result == nil {
		cell.Outcome = "parse_error"
		cell.ErrorCode = "parser_empty_result"
		return
	}
	cell.ExecutedEngine = cell.RequestedEngine
	if engine := strings.TrimSpace(result.Metadata["parser"]); engine != "" {
		cell.ExecutedEngine = engine
	}
	if cell.ExecutedEngine != cell.RequestedEngine {
		cell.FallbackReason = strings.TrimSpace(result.Metadata["anydoc_fallback"])
		if cell.FallbackReason == "" {
			cell.FallbackReason = "runtime_reported"
		}
	}
	if strings.TrimSpace(result.Error) != "" {
		cell.Outcome = "parse_error"
		cell.ErrorCode = "parser_result_error"
		return
	}
	if strings.TrimSpace(result.MarkdownContent) == "" {
		cell.Outcome = "parse_empty"
		cell.ErrorCode = "parser_empty_output"
		return
	}
	anchors := make([]string, 0, len(truth.Components.AnchorMatches))
	for _, anchor := range truth.Components.AnchorMatches {
		anchors = append(anchors, anchor.Text)
	}
	prediction, err := ExtractParserComponents(result.MarkdownContent, anchors)
	if err != nil {
		cell.Outcome = "metric_error"
		cell.ErrorCode = "parser_metric_failed"
		return
	}
	metrics := ScoreParserComponents(truth.Components, prediction)
	cell.Metrics = &metrics
	cell.OutputSHA256 = sha256Hex([]byte(result.MarkdownContent))
	cell.Outcome = "succeeded"
	if cell.ExecutedEngine != cell.RequestedEngine {
		cell.Outcome = "fallback_succeeded"
	}
}

func summarizeParserBenchmarkEngines(report *ParserBenchmarkReportV1) []ParserBenchmarkEngineSummaryV1 {
	result := make([]ParserBenchmarkEngineSummaryV1, 0, report.EngineCount)
	for _, engine := range FrozenParserEngineOrder {
		summary := ParserBenchmarkEngineSummaryV1{RequestedEngine: engine, ExecutedEngines: []string{}}
		executed := map[string]bool{}
		metrics := make([]ParserMetricSetV1, 0)
		for _, cell := range report.Cells {
			if cell.RequestedEngine != engine {
				continue
			}
			if cell.Supported {
				summary.SupportedCellCount++
			}
			switch cell.Outcome {
			case "unavailable":
				summary.UnavailableCellCount++
			case "succeeded", "fallback_succeeded":
				summary.AttemptedCellCount++
				summary.ScoredCellCount++
				metrics = append(metrics, *cell.Metrics)
			case "unsupported":
			case "not_attempted":
				summary.NotAttemptedCellCount++
			default:
				summary.AttemptedCellCount++
				summary.FailedCellCount++
			}
			if cell.ExecutedEngine != "" {
				executed[cell.ExecutedEngine] = true
			}
		}
		summary.Available = summary.AttemptedCellCount > 0
		for name := range executed {
			summary.ExecutedEngines = append(summary.ExecutedEngines, name)
		}
		sort.Strings(summary.ExecutedEngines)
		summary.MetricMeans = parserMetricMeans(metrics)
		result = append(result, summary)
	}
	return result
}

func parserMetricMeans(metrics []ParserMetricSetV1) ParserMetricMeanSetV1 {
	values := func(selectValue func(ParserMetricSetV1) ParserMetricValueV1) ParserMetricMeanV1 {
		total := 0.0
		count := 0
		for _, metric := range metrics {
			value := selectValue(metric)
			if value.Applicable && value.Value != nil {
				total += *value.Value
				count++
			}
		}
		if count == 0 {
			return ParserMetricMeanV1{}
		}
		mean := math.Round(total/float64(count)*1_000_000) / 1_000_000
		return ParserMetricMeanV1{ApplicableCellCount: count, Value: &mean}
	}
	return ParserMetricMeanSetV1{
		TextSimilarity:       values(func(metric ParserMetricSetV1) ParserMetricValueV1 { return metric.TextSimilarity }),
		StructureF1:          values(func(metric ParserMetricSetV1) ParserMetricValueV1 { return metric.StructureF1 }),
		TableCellF1:          values(func(metric ParserMetricSetV1) ParserMetricValueV1 { return metric.TableCellF1 }),
		FormulaSimilarity:    values(func(metric ParserMetricSetV1) ParserMetricValueV1 { return metric.FormulaSimilarity }),
		ReadingOrderAccuracy: values(func(metric ParserMetricSetV1) ParserMetricValueV1 { return metric.ReadingOrderAccuracy }),
	}
}

func WriteParserBenchmarkReports(report *ParserBenchmarkReportV1, jsonPath, markdownPath string) error {
	if report == nil || strings.TrimSpace(jsonPath) == "" || strings.TrimSpace(markdownPath) == "" {
		return fmt.Errorf("parser_benchmark_report_paths_invalid")
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode parser benchmark report: %w", err)
	}
	if err := writeParserBenchmarkFile(jsonPath, append(encoded, '\n')); err != nil {
		return err
	}
	var markdown strings.Builder
	fmt.Fprintf(&markdown, "# Parser benchmark %s\n\n", report.RunID)
	fmt.Fprintf(&markdown, "- Status: `%s`\n", report.Status)
	fmt.Fprintf(&markdown, "- Generated at: `%s`\n", report.GeneratedAt)
	fmt.Fprintf(&markdown, "- Ready engines: `%d/%d`\n", report.ReadyEngineCount, report.EngineCount)
	fmt.Fprintf(&markdown, "- Cells: `%d` total, `%d` scored, `%d` unavailable, `%d` unsupported, `%d` not attempted, `%d` failed\n", report.CellCount, report.ScoredCellCount, report.UnavailableCellCount, report.UnsupportedCellCount, report.NotAttemptedCellCount, report.FailedCellCount)
	fmt.Fprintf(&markdown, "- Corpus manifest SHA-256: `%s`\n", report.CorpusManifestSHA256)
	fmt.Fprintf(&markdown, "- Ground-truth payload SHA-256: `%s`\n\n", report.GroundTruthPayloadSHA256)
	markdown.WriteString("| Requested engine | Available | Supported | Attempted | Scored | Failed |\n")
	markdown.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, engine := range report.Engines {
		fmt.Fprintf(&markdown, "| %s | %t | %d | %d | %d | %d |\n", engine.RequestedEngine, engine.Available, engine.SupportedCellCount, engine.AttemptedCellCount, engine.ScoredCellCount, engine.FailedCellCount)
	}
	markdown.WriteString("\nUnsupported and unavailable cells are retained in the JSON matrix. Unknown billing facts remain `unknown`; the report never substitutes zero.\n")
	return writeParserBenchmarkFile(markdownPath, []byte(markdown.String()))
}

func writeParserBenchmarkFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parser benchmark report directory: %w", err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		return fmt.Errorf("write parser benchmark report: %w", err)
	}
	return nil
}
