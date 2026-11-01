package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// runBenchTriage implements `excalibur bench triage <run-dir>`:
// automatic failure triage of evaluation trials. For every trial with
// reward 0 it reads the agent's own records plus the pass/fail reward
// and classifies the main cause into the fixed taxonomy. It never
// reads the verifier's tests or reference solutions beyond
// reward/pass-fail. Output is JSONL plus an aggregated Markdown table.
func runBenchTriage(args []string) error {
	fs := flag.NewFlagSet("bench triage", flag.ContinueOnError)
	model := fs.String("model", "", "judge model id (default: the run's own model, then the credentials file model)")
	out := fs.String("out", "", "output JSONL file (default: <run-dir>/triage.jsonl; Markdown lands beside it)")
	credsFile := fs.String("credentials-file", "", "operator credentials JSON (default: ~/.config/excalibur/credentials/openai.json)")
	noModel := fs.Bool("no-model", false, "deterministic only: never call the judge model (same as EXCALIBUR_TRIAGE_MODEL=0)")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: excalibur bench triage <run-dir> [--model ID] [--out FILE] [--credentials-file PATH] [--no-model]")
	}
	runDir := rest[0]
	if _, err := os.Stat(runDir); err != nil {
		return fmt.Errorf("bench triage: %w", err)
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	results, stats, err := eval.TriageRun(ctx, runDir, eval.TriageOptions{
		ModelOverride:   *model,
		CredentialsFile: *credsFile,
		NoModel:         *noModel,
	})
	if err != nil {
		return fmt.Errorf("bench triage: %w", err)
	}
	jsonlPath := *out
	if jsonlPath == "" {
		jsonlPath = filepath.Join(runDir, "triage.jsonl")
	}
	if err := eval.WriteTriageJSONL(jsonlPath, results); err != nil {
		return fmt.Errorf("bench triage: %w", err)
	}
	mdPath := triageMarkdownPath(jsonlPath)
	md := eval.RenderTriageMarkdown(results, stats, runDir)
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("bench triage: write markdown: %w", err)
	}
	byCause := map[string]int{}
	for _, r := range results {
		byCause[r.Cause]++
	}
	causes := make([]string, 0, len(byCause))
	for c, n := range byCause {
		causes = append(causes, c+"="+itoa(n))
	}
	fmt.Fprintf(os.Stdout, "triaged=%d passed=%d failed=%d infra=%d skipped=%d judge=%d errors=%d (%s) in %s and %s\n",
		stats.Total, stats.Passed, stats.Failed, stats.Infra, stats.Skipped,
		stats.JudgeCalls, stats.JudgeErrors,
		strings.Join(causes, " "), jsonlPath, mdPath)
	return nil
}

// triageMarkdownPath places the aggregated table beside the JSONL.
func triageMarkdownPath(jsonlPath string) string {
	if strings.HasSuffix(jsonlPath, ".jsonl") {
		return strings.TrimSuffix(jsonlPath, ".jsonl") + ".md"
	}
	return jsonlPath + ".md"
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
