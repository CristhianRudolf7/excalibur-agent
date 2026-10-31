package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// runEvalSmoke implements `excalibur eval smoke`: it runs the
// repository-owned local tasks (Section 23.2) with no Harbor, Docker or
// paid API, validating installation, profile, ATIF and metrics before
// any paid trial. It returns a non-nil error when any task fails.
func runEvalSmoke(args []string) error {
	fs := flag.NewFlagSet("eval smoke", flag.ContinueOnError)
	dir := fs.String("dir", "", "output directory for smoke trials (default: fresh temp dir)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	out := *dir
	if out == "" {
		tmp, err := os.MkdirTemp("", "excalibur-smoke-")
		if err != nil {
			return fmt.Errorf("eval smoke: %w", err)
		}
		out = tmp
	}
	runner := eval.SmokeRunner{Dir: out, Profile: eval.DefaultEvalProfile()}
	results, err := runner.Run(eval.DefaultSmokeTasks())
	if err != nil {
		return fmt.Errorf("eval smoke: %w", err)
	}
	failed := 0
	for _, res := range results {
		status := "passed"
		if !res.Passed {
			status = "failed"
			failed++
		}
		fmt.Fprintf(os.Stdout, "%s: %s (%s)\n", res.Name, status, res.Detail)
	}
	fmt.Fprintf(os.Stdout, "smoke trials in %s\n", out)
	if failed > 0 {
		return fmt.Errorf("eval smoke: %d task(s) failed", failed)
	}
	return nil
}

// runEvalReport implements `excalibur eval report`: it aggregates trial
// directories with the Section 23.7 statistics and writes report.json.
func runEvalReport(args []string) error {
	fs := flag.NewFlagSet("eval report", flag.ContinueOnError)
	out := fs.String("out", "", "directory for report.json (default: <trials>)")
	k := fs.Int("k", 5, "pass@k horizon")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: excalibur eval report <trials-dir> [--out DIR] [--k N]")
	}
	trials, err := eval.LoadTrials(rest[0])
	if err != nil {
		return fmt.Errorf("eval report: %w", err)
	}
	rep, err := eval.Aggregate(trials, *k)
	if err != nil {
		return fmt.Errorf("eval report: %w", err)
	}
	dest := *out
	if dest == "" {
		dest = rest[0]
	}
	if err := eval.WriteReport(dest, rep); err != nil {
		return fmt.Errorf("eval report: %w", err)
	}
	fmt.Fprintf(os.Stdout, "trials=%d tasks=%d success=%.3f [%.3f, %.3f] pass@%d=%.3f pass^%d=%.3f cost/success=$%.4f\n",
		rep.Trials, rep.Tasks, rep.Success.Mean, rep.Success.Low, rep.Success.High,
		rep.K, rep.PassAtK, rep.K, rep.PassPowK, rep.CostPerSuccess)
	fmt.Fprintf(os.Stdout, "report in %s/report.json\n", dest)
	return nil
}
