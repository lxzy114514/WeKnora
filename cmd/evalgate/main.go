package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/internal/evaluation/gate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("evalgate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baselinePath := flags.String("baseline", "", "baseline evaluation report JSON")
	currentPath := flags.String("current", "", "current evaluation report JSON")
	thresholdsPath := flags.String("thresholds", "", "threshold policy JSON")
	jsonOut := flags.String("json-out", "", "optional gate result JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *baselinePath == "" || *currentPath == "" || *thresholdsPath == "" {
		fmt.Fprintln(stderr, "evalgate: -baseline, -current and -thresholds are required")
		return 2
	}
	var baseline, current gate.Report
	var thresholds []gate.Threshold
	for _, item := range []struct {
		path  string
		value interface{}
	}{
		{*baselinePath, &baseline}, {*currentPath, &current}, {*thresholdsPath, &thresholds},
	} {
		if err := readJSON(item.path, item.value); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	result, err := gate.Compare(baseline, current, thresholds)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if *jsonOut != "" {
		data, marshalErr := json.MarshalIndent(result, "", "  ")
		if marshalErr != nil {
			fmt.Fprintln(stderr, marshalErr)
			return 2
		}
		if writeErr := os.MkdirAll(filepath.Dir(*jsonOut), 0o755); writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			return 2
		}
		if writeErr := os.WriteFile(*jsonOut, append(data, '\n'), 0o600); writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			return 2
		}
	}
	if result.Passed {
		fmt.Fprintf(stdout, "evaluation gate passed (%d checks)\n", len(result.Checks))
		return 0
	}
	for _, failure := range result.Failures {
		fmt.Fprintf(stdout,
			"REGRESSION %s: baseline=%.6f current=%.6f change=%+.6f allowed=%.6f direction=%s\n",
			failure.Metric, failure.Baseline, failure.Current, failure.Change,
			failure.MaxRegression, failure.Direction,
		)
	}
	return 1
}

func readJSON(path string, value interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("evalgate: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("evalgate: decode %s: %w", path, err)
	}
	return nil
}
