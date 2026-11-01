package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// runBenchDiff implements `excalibur bench diff <runA> <runB>
// [--base baseline.json]`: the arm diff with per-task pass rates,
// flipped tasks with triage evidence, mean resource differences and
// the paired Section 23.7 bootstrap. Report side only: it reads
// finished run directories and never invokes Harbor.
func runBenchDiff(args []string) error {
	fs := flag.NewFlagSet("bench diff", flag.ContinueOnError)
	out := fs.String("out", "", "directory for diff.json and diff.md (default: <runB>)")
	k := fs.Int("k", 5, "pass@k horizon")
	reps := fs.Int("reps", 10000, "bootstrap resamples")
	seed := fs.Int64("seed", 7, "bootstrap seed (deterministic)")
	bar := fs.Float64("bar", eval.DefaultPracticalBar, "practical acceptance bar (fraction, default 0.03)")
	base := fs.String("base", "", "baseline file {\"baseline_rate\": {task: rate}} for context")
	noTriage := fs.Bool("no-triage", false, "skip flip triage (same as EXCALIBUR_DIFF_TRIAGE=0)")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: excalibur bench diff <runA> <runB> [--out DIR] [--k N] [--reps N] [--seed N] [--bar F] [--base baseline.json] [--no-triage]")
	}
	rep, err := eval.DiffRuns(rest[0], rest[1], eval.DiffOptions{
		K: *k, Reps: *reps, Seed: *seed, Bar: *bar,
		BasePath: *base, NoTriage: *noTriage,
	})
	if err != nil {
		return fmt.Errorf("bench diff: %w", err)
	}
	dest := *out
	if dest == "" {
		dest = rest[1]
	}
	if err := eval.WriteDiffReport(dest, rep); err != nil {
		return fmt.Errorf("bench diff: %w", err)
	}
	md := eval.RenderDiffMarkdown(rep)
	if err := os.WriteFile(filepath.Join(dest, "diff.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("bench diff: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s: paired=%d success A=%.3f [%.3f, %.3f] B=%.3f [%.3f, %.3f] diff=%+.3f [%+.3f, %+.3f] flips=%d\n",
		rep.Verdict, len(rep.PairedTasks),
		rep.SuccessA.Mean, rep.SuccessA.Low, rep.SuccessA.High,
		rep.SuccessB.Mean, rep.SuccessB.Low, rep.SuccessB.High,
		rep.Diff.Mean, rep.Diff.Low, rep.Diff.High, len(rep.Flips))
	fmt.Fprintf(os.Stdout, "diff in %s/diff.json and diff.md\n", dest)
	return nil
}

// runBenchInterim implements `excalibur bench interim <run>
// --against <run|baseline.json>`: the paired estimate on the tasks
// completed so far with the STOP/CONTINUE early-stop verdict. It
// works on partial runs while they are still executing: only trials
// already completed enter the estimate. Report side only.
func runBenchInterim(args []string) error {
	fs := flag.NewFlagSet("bench interim", flag.ContinueOnError)
	against := fs.String("against", "", "reference run directory or baseline JSON file")
	out := fs.String("out", "", "directory for interim.json and interim.md (default: <run>)")
	reps := fs.Int("reps", 10000, "bootstrap resamples")
	seed := fs.Int64("seed", 7, "bootstrap seed (deterministic)")
	bar := fs.Float64("bar", 0, "practical bar (fraction; default EXCALIBUR_INTERIM_BAR or 0.03)")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 || *against == "" {
		return fmt.Errorf("usage: excalibur bench interim <run> --against <run|baseline.json> [--out DIR] [--reps N] [--seed N] [--bar F]")
	}
	rep, err := eval.InterimEstimate(rest[0], eval.InterimOptions{
		Against: *against, Reps: *reps, Seed: *seed, Bar: *bar,
	})
	if err != nil {
		return fmt.Errorf("bench interim: %w", err)
	}
	dest := *out
	if dest == "" {
		dest = rest[0]
	}
	if err := eval.WriteInterimReport(dest, rep); err != nil {
		return fmt.Errorf("bench interim: %w", err)
	}
	md := eval.RenderInterimMarkdown(rep)
	if err := os.WriteFile(filepath.Join(dest, "interim.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("bench interim: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s: completed=%d current=%.3f [%.3f, %.3f] ref=%.3f diff=%+.3f [%+.3f, %+.3f]\n",
		rep.Verdict, len(rep.CompletedTasks),
		rep.Current.Mean, rep.Current.Low, rep.Current.High,
		rep.Reference, rep.Diff.Mean, rep.Diff.Low, rep.Diff.High)
	fmt.Fprintf(os.Stdout, "interim in %s/interim.json and interim.md\n", dest)
	return nil
}
