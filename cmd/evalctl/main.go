package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/evaluation/gate"
	"github.com/Tencent/WeKnora/internal/types"
)

type apiResponse struct {
	Success bool                    `json:"success"`
	Data    *types.EvaluationDetail `json:"data"`
}

type casesResponse struct {
	Success bool                          `json:"success"`
	Data    []*types.EvaluationCaseResult `json:"data"`
}

type startRequest struct {
	DatasetID        string `json:"dataset_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id,omitempty"`
	ChatModelID      string `json:"chat_id,omitempty"`
	EmbeddingModelID string `json:"embedding_id,omitempty"`
	RerankModelID    string `json:"rerank_id,omitempty"`
	SummaryModelID   string `json:"summary_id,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(parent context.Context, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("evalctl", flag.ContinueOnError)
	baseURL := flags.String("base-url", envOr("WEKNORA_BASE_URL", "http://localhost:8080"), "WeKnora server URL")
	apiKey := flags.String("api-key", os.Getenv("WEKNORA_API_KEY"), "API key (prefer WEKNORA_API_KEY)")
	bearer := flags.String("bearer-token", os.Getenv("WEKNORA_BEARER_TOKEN"), "JWT (prefer WEKNORA_BEARER_TOKEN)")
	tenantID := flags.String("tenant-id", os.Getenv("WEKNORA_TENANT_ID"), "optional X-Tenant-ID")
	datasetID := flags.String("dataset", "baseline-v1", "versioned dataset ID")
	kbID := flags.String("knowledge-base", "", "optional source knowledge-base ID")
	chatID := flags.String("chat-model", "", "chat model ID; empty selects configured model")
	embeddingID := flags.String("embedding-model", "", "embedding model ID; empty selects configured model")
	rerankID := flags.String("rerank-model", "", "rerank model ID; empty selects configured model")
	summaryID := flags.String("summary-model", "", "summary model ID; empty uses chat/configured model")
	output := flags.String("output", filepath.Join("artifacts", "evaluation-report.json"), "portable report path")
	detailOutput := flags.String("detail-output", filepath.Join("artifacts", "evaluation-detail.json"), "full report path")
	markdownOutput := flags.String("markdown-output", filepath.Join("artifacts", "evaluation-report.md"), "human report path")
	timeout := flags.Duration("timeout", 30*time.Minute, "maximum run duration")
	poll := flags.Duration("poll", 2*time.Second, "poll interval")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *apiKey == "" && *bearer == "" {
		return errors.New("evalctl: set WEKNORA_API_KEY or WEKNORA_BEARER_TOKEN")
	}
	ctx, cancel := context.WithTimeout(parent, *timeout)
	defer cancel()
	client := &http.Client{Timeout: 60 * time.Second}
	endpoint := strings.TrimRight(*baseURL, "/") + "/api/v1/evaluation"
	body, _ := json.Marshal(startRequest{
		DatasetID: *datasetID, KnowledgeBaseID: *kbID, ChatModelID: *chatID,
		EmbeddingModelID: *embeddingID, RerankModelID: *rerankID, SummaryModelID: *summaryID,
	})
	response, err := do(ctx, client, http.MethodPost, endpoint, body, *apiKey, *bearer, *tenantID)
	if err != nil {
		return err
	}
	if response.Data == nil || response.Data.Task == nil {
		return errors.New("evalctl: server returned no evaluation task")
	}
	taskID := response.Data.Task.ID
	fmt.Fprintf(stdout, "evaluation started: %s\n", taskID)

	var detail *types.EvaluationDetail
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("evalctl: evaluation %s timed out: %w", taskID, ctx.Err())
		case <-time.After(*poll):
		}
		statusURL := endpoint + "?task_id=" + url.QueryEscape(taskID)
		response, err = do(ctx, client, http.MethodGet, statusURL, nil, *apiKey, *bearer, *tenantID)
		if err != nil {
			return err
		}
		detail = response.Data
		if detail == nil || detail.Task == nil {
			return errors.New("evalctl: server returned an empty evaluation result")
		}
		fmt.Fprintf(stdout, "progress: %d/%d\n", detail.Task.Finished, detail.Task.Total)
		if detail.Task.Status == types.EvaluationStatueSuccess {
			break
		}
		if detail.Task.Status == types.EvaluationStatueFailed {
			return fmt.Errorf("evalctl: evaluation %s failed: %s", taskID, detail.Task.ErrMsg)
		}
	}
	detail.Cases, err = fetchEvaluationCases(
		ctx, client, endpoint, taskID, *apiKey, *bearer, *tenantID,
	)
	if err != nil {
		return err
	}
	if err := writeJSON(*detailOutput, detail); err != nil {
		return err
	}
	if err := writeJSON(*output, portableReport(detail)); err != nil {
		return err
	}
	if err := writeMarkdown(*markdownOutput, detail); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "portable report: %s\nfull report: %s\nhuman report: %s\n", *output, *detailOutput, *markdownOutput)
	return nil
}

func do(
	ctx context.Context, client *http.Client, method, url string, body []byte,
	apiKey, bearer, tenantID string,
) (*apiResponse, error) {
	var response apiResponse
	if err := doJSON(ctx, client, method, url, body, apiKey, bearer, tenantID, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func doJSON(
	ctx context.Context, client *http.Client, method, url string, body []byte,
	apiKey, bearer, tenantID string, target interface{},
) error {
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("X-API-Key", apiKey)
	} else {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if tenantID != "" {
		request.Header.Set("X-Tenant-ID", tenantID)
	}
	result, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("evalctl: %s request failed: %w", method, err)
	}
	defer result.Body.Close()
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(result.Body, 64<<10))
		return fmt.Errorf("evalctl: %s returned HTTP %d", method, result.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(result.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("evalctl: decode response: %w", err)
	}
	return nil
}

func fetchEvaluationCases(
	ctx context.Context, client *http.Client, endpoint, taskID, apiKey, bearer, tenantID string,
) ([]*types.EvaluationCaseResult, error) {
	const pageSize = 100
	all := make([]*types.EvaluationCaseResult, 0)
	for offset := 0; ; offset += pageSize {
		casesURL := endpoint + "/cases?task_id=" + url.QueryEscape(taskID) +
			"&limit=" + fmt.Sprint(pageSize) + "&offset=" + fmt.Sprint(offset)
		var response casesResponse
		if err := doJSON(
			ctx, client, http.MethodGet, casesURL, nil, apiKey, bearer, tenantID, &response,
		); err != nil {
			return nil, err
		}
		all = append(all, response.Data...)
		if len(response.Data) < pageSize {
			return all, nil
		}
	}
}

func portableReport(detail *types.EvaluationDetail) gate.Report {
	report := gate.Report{Metrics: map[string]float64{}}
	if detail == nil {
		return report
	}
	if detail.Task != nil {
		report.RunID = detail.Task.ID
	}
	if detail.Config != nil {
		report.ConfigHash = detail.Config.ConfigHash
	}
	if detail.Metric != nil {
		r := detail.Metric.RetrievalMetrics
		g := detail.Metric.GenerationMetrics
		report.Metrics["retrieval.precision"] = r.Precision
		report.Metrics["retrieval.recall"] = r.Recall
		report.Metrics["retrieval.ndcg3"] = r.NDCG3
		report.Metrics["retrieval.ndcg10"] = r.NDCG10
		report.Metrics["retrieval.mrr"] = r.MRR
		report.Metrics["retrieval.map"] = r.MAP
		report.Metrics["answer.bleu1"] = g.BLEU1
		report.Metrics["answer.bleu2"] = g.BLEU2
		report.Metrics["answer.bleu4"] = g.BLEU4
		report.Metrics["answer.rouge1"] = g.ROUGE1
		report.Metrics["answer.rouge2"] = g.ROUGE2
		report.Metrics["answer.rougel"] = g.ROUGEL
	}
	if detail.Cost != nil {
		report.Metrics["cost.total_usd"] = detail.Cost.Total
		report.Metrics["cost.chat_usd"] = detail.Cost.Chat
		report.Metrics["cost.embedding_usd"] = detail.Cost.Embedding
		report.Metrics["cost.rerank_usd"] = detail.Cost.Rerank
		report.Metrics["cost.call_count"] = float64(detail.Cost.CallCount)
		report.MetricEligibility = map[string]gate.MetricEligibility{
			"cost.call_count": {Eligible: true, CoverageRatio: 1},
		}
		reason := ""
		if !detail.Cost.CostGateEligible {
			reason = fmt.Sprintf("price coverage incomplete (%.6f)", detail.Cost.CostCoverageRatio)
		}
		for _, metric := range []string{"cost.total_usd", "cost.chat_usd", "cost.embedding_usd", "cost.rerank_usd"} {
			report.MetricEligibility[metric] = gate.MetricEligibility{
				Eligible: detail.Cost.CostGateEligible, CoverageRatio: detail.Cost.CostCoverageRatio, Reason: reason,
			}
		}
	}
	if detail.Duration != nil {
		report.Metrics["latency.total_ms"] = float64(detail.Duration.TotalMS)
		report.Metrics["latency.average_case_ms"] = detail.Duration.AverageCaseMS
		report.Metrics["latency.model_call_ms"] = float64(detail.Duration.ModelCallMS)
		report.Metrics["latency.index_ms"] = float64(detail.Duration.IndexMS)
		report.Metrics["latency.retrieval_ms"] = float64(detail.Duration.RetrievalMS)
		report.Metrics["latency.rerank_ms"] = float64(detail.Duration.RerankMS)
		report.Metrics["latency.generation_ms"] = float64(detail.Duration.GenerationMS)
	}
	return report
}

func writeJSON(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func writeMarkdown(path string, detail *types.EvaluationDetail) error {
	if detail == nil || detail.Task == nil {
		return errors.New("evalctl: cannot render an empty evaluation")
	}
	report := portableReport(detail)
	var out strings.Builder
	fmt.Fprintf(&out, "# WeKnora evaluation %s\n\n", markdownCell(detail.Task.ID))
	fmt.Fprintf(&out, "- Dataset: `%s`\n", markdownCell(detail.Task.DatasetID))
	if detail.Config != nil {
		fmt.Fprintf(&out, "- Dataset version: `%s`\n", markdownCell(detail.Config.DatasetVersion))
		fmt.Fprintf(&out, "- Dataset hash: `%s`\n", markdownCell(detail.Config.DatasetHash))
		fmt.Fprintf(&out, "- Configuration hash: `%s`\n", markdownCell(detail.Config.ConfigHash))
		fmt.Fprintf(&out, "- Code version: `%s`\n", markdownCell(detail.Config.CodeVersion))
	}
	fmt.Fprintf(&out, "- Started: `%s`\n", detail.Task.StartTime.UTC().Format(time.RFC3339))
	if detail.Task.EndTime != nil {
		fmt.Fprintf(&out, "- Ended: `%s`\n", detail.Task.EndTime.UTC().Format(time.RFC3339))
	}
	out.WriteString("\n## Quality, cost, and latency\n\n| Metric | Value |\n|---|---:|\n")
	keys := make([]string, 0, len(report.Metrics))
	for key := range report.Metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if eligibility, exists := report.MetricEligibility[key]; exists && !eligibility.Eligible {
			fmt.Fprintf(&out, "| %s | unknown (%s) |\n", markdownCell(key), markdownCell(eligibility.Reason))
			continue
		}
		fmt.Fprintf(&out, "| %s | %.6f |\n", markdownCell(key), report.Metrics[key])
	}
	out.WriteString("\n## Per-question evidence\n\n| QID | Retrieved | Reranked | Duration (ms) | Answer | Error |\n|---:|---|---|---:|---|---|\n")
	for _, item := range detail.Cases {
		if item == nil {
			continue
		}
		fmt.Fprintf(&out, "| %d | %s | %s | %d | %s | %s |\n",
			item.QID, markdownCell(fmt.Sprint(item.RetrievedPIDs)), markdownCell(fmt.Sprint(item.RerankedPIDs)),
			item.DurationMS, markdownCell(truncateRunes(item.FinalAnswer, 160)), markdownCell(item.Error))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out.String()), 0o600)
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
