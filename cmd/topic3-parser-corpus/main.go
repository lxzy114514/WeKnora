// Command topic3-parser-corpus deterministically rebuilds the frozen Topic 3
// parser corpus or seals an already-rendered implementation E1 submission.
// Independent review and approval always remain separate Auditor work.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/internal/evaluation"
)

func main() {
	output := flag.String("output", evaluation.ParserCorpusRepositoryPath, "corpus v1 output directory")
	template := flag.String("template", evaluation.ParserCorpusRepositoryPath, "directory containing sources/authoring.json and extraction-rules.json")
	seal := flag.Bool("seal", false, "seal existing render artifacts into the pending E1 submission")
	provenancePath := flag.String("provenance", "", "render provenance JSON used with --seal")
	flag.Parse()
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		fatal(err)
	}
	if *seal {
		if *provenancePath == "" {
			fatal(fmt.Errorf("--provenance is required with --seal"))
		}
		provenanceFile, err := os.Open(*provenancePath)
		if err != nil {
			fatal(err)
		}
		defer provenanceFile.Close()
		var provenance evaluation.ParserRenderProvenanceV1
		decoder := json.NewDecoder(provenanceFile)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&provenance); err != nil {
			fatal(err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			fatal(fmt.Errorf("provenance must contain exactly one JSON value"))
		}
		renderIdentity, err := evaluation.SealParserE1Submission(outputPath, provenance)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("sealed pending E1 submission; render manifest %s\n", renderIdentity.SHA256)
		return
	}
	templatePath, err := filepath.Abs(*template)
	if err != nil {
		fatal(err)
	}
	if err := evaluation.BuildParserCorpus(templatePath, outputPath); err != nil {
		fatal(err)
	}
	fmt.Printf("rebuilt parser corpus at %s\n", outputPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
