// Command topic3-parser-benchmark runs the approved Topic 3 parser corpus
// through the repository's registered document-parser engines.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/evaluation"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func main() {
	repositoryRoot := flag.String("repository-root", ".", "WeKnora repository root")
	runID := flag.String("run-id", "", "stable identifier for this benchmark run")
	jsonOutput := flag.String("json-output", "artifacts/parser-benchmark-report.json", "JSON report path")
	markdownOutput := flag.String("markdown-output", "artifacts/parser-benchmark-report.md", "Markdown report path")
	cellTimeout := flag.Duration("cell-timeout", 20*time.Minute, "maximum duration for one supported parser cell")
	overallTimeout := flag.Duration("overall-timeout", 75*time.Minute, "maximum duration for the complete benchmark")
	docreaderAddress := flag.String("docreader-address", envOr("TOPIC3_DOCREADER_ADDR", os.Getenv("DOCREADER_ADDR")), "optional DocReader address")
	docreaderTransport := flag.String("docreader-transport", envOr("TOPIC3_DOCREADER_TRANSPORT", os.Getenv("DOCREADER_TRANSPORT")), "DocReader transport: grpc or http")
	flag.Parse()

	if err := run(*repositoryRoot, *runID, *jsonOutput, *markdownOutput, *docreaderAddress, *docreaderTransport, *cellTimeout, *overallTimeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(repositoryRoot, runID, jsonOutput, markdownOutput, address, transport string, cellTimeout, overallTimeout time.Duration) error {
	absoluteRoot, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	corpus, err := evaluation.LoadApprovedParserCorpus(absoluteRoot)
	if err != nil {
		return fmt.Errorf("parser benchmark preflight: %w", err)
	}
	if strings.TrimSpace(runID) == "" {
		runID = "parser-" + time.Now().UTC().Format("20060102T150405Z")
	}
	overrides, err := parserOverridesFromEnvironment()
	if err != nil {
		return err
	}
	credentials := parserCloudCredentialsFromEnvironment()
	if credentials != nil {
		overrides["weknoracloud_app_id"] = credentials.AppID
	}

	remote, remoteEngines := connectDocReader(address, transport, overrides)
	if remote != nil {
		defer func() { _ = remote.Close() }()
	}
	connected := remote != nil && remote.IsConnected()
	engines := docparser.ListAllEngines(connected, overrides, remoteEngines)
	deps := docparser.ReaderDeps{Overrides: overrides, Remote: remote}
	if credentials != nil {
		deps.WeKnoraCloudCredentials = func(context.Context) *types.WeKnoraCloudCredentials { return credentials }
	}

	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()
	report, err := evaluation.RunParserBenchmark(ctx, corpus, evaluation.ParserBenchmarkOptions{
		CorpusRoot: filepath.Join(absoluteRoot, filepath.FromSlash(evaluation.ParserCorpusRepositoryPath)),
		RunID:      runID, GeneratedAt: time.Now().UTC(), PerCellTimeout: cellTimeout, Engines: engines,
		NewReader: func(ctx context.Context, engine, fileType string) (interfaces.DocReader, error) {
			return docparser.NewReader(ctx, engine, fileType, false, deps)
		},
	})
	if err != nil {
		return err
	}
	if !filepath.IsAbs(jsonOutput) {
		jsonOutput = filepath.Join(absoluteRoot, jsonOutput)
	}
	if !filepath.IsAbs(markdownOutput) {
		markdownOutput = filepath.Join(absoluteRoot, markdownOutput)
	}
	if err := evaluation.WriteParserBenchmarkReports(report, jsonOutput, markdownOutput); err != nil {
		return err
	}
	fmt.Printf("parser benchmark %s: %s, ready engines %d/%d, scored cells %d/%d\n", report.RunID, report.Status, report.ReadyEngineCount, report.EngineCount, report.ScoredCellCount, report.SupportedCellCount)
	return nil
}

type closeableDocumentReader interface {
	interfaces.DocumentReader
	Close() error
}

func connectDocReader(address, transport string, overrides map[string]string) (closeableDocumentReader, []types.ParserEngineInfo) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, nil
	}
	var (
		remote closeableDocumentReader
		err    error
	)
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "grpc":
		remote, err = docparser.NewGRPCDocumentReader(address)
	case "http":
		remote, err = docparser.NewHTTPDocumentReader(address)
	default:
		fmt.Fprintln(os.Stderr, "parser benchmark: unsupported DocReader transport; continuing without DocReader")
		return nil, nil
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "parser benchmark: DocReader initialization failed; continuing with independently available engines")
		return nil, nil
	}
	probeContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engines, err := remote.ListEngines(probeContext, overrides)
	if err != nil {
		_ = remote.Close()
		fmt.Fprintln(os.Stderr, "parser benchmark: DocReader availability probe failed; continuing with independently available engines")
		return nil, nil
	}
	return remote, engines
}

func parserOverridesFromEnvironment() (map[string]string, error) {
	overrides := map[string]string{}
	if encoded := strings.TrimSpace(os.Getenv("TOPIC3_PARSER_OVERRIDES_JSON")); encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &overrides); err != nil {
			return nil, fmt.Errorf("TOPIC3_PARSER_OVERRIDES_JSON is invalid: %w", err)
		}
	}
	for key, environmentName := range map[string]string{
		"mineru_endpoint":          "TOPIC3_MINERU_ENDPOINT",
		"mineru_api_key":           "TOPIC3_MINERU_API_KEY",
		"paddleocr_vl_endpoint":    "TOPIC3_PADDLEOCR_VL_ENDPOINT",
		"paddleocr_vl_cloud_token": "TOPIC3_PADDLEOCR_VL_CLOUD_TOKEN",
	} {
		if value := strings.TrimSpace(os.Getenv(environmentName)); value != "" {
			overrides[key] = value
		}
	}
	return overrides, nil
}

func parserCloudCredentialsFromEnvironment() *types.WeKnoraCloudCredentials {
	appID := strings.TrimSpace(os.Getenv("TOPIC3_WEKNORA_CLOUD_APP_ID"))
	appSecret := strings.TrimSpace(os.Getenv("TOPIC3_WEKNORA_CLOUD_APP_SECRET"))
	if appID == "" || appSecret == "" {
		return nil
	}
	return &types.WeKnoraCloudCredentials{AppID: appID, AppSecret: appSecret}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
