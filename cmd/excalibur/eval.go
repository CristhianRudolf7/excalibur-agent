package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// runEvalSmoke implements `excalibur eval smoke`: it runs the
// repository-owned local tasks (Section 23.2) with no Harbor, Docker or
// paid API, validating installation, profile, ATIF and metrics before
// any paid trial. It returns a non-nil error when any task fails.
func runEvalSmoke(args []string) error {
	fs := flag.NewFlagSet("eval smoke", flag.ContinueOnError)
	dir := fs.String("dir", "", "output directory for smoke trials (default: fresh temp dir)")
	if _, err := parseCmd(fs, args); err != nil {
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
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	rest = append(rest[:0], rest...)
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

// runEvalQuick implements `excalibur eval quick`: it runs the
// repository-owned quick suite (25 small terminal tasks with exact
// verifiers) for cheap regression checks between arms. Each task runs
// `exec --profile eval` in an isolated work directory; results land
// as trial.json files plus the manifest and report.json, so `bench
// compare` consumes quick dirs exactly like bench jobs.
func runEvalQuick(args []string) error {
	fs := flag.NewFlagSet("eval quick", flag.ContinueOnError)
	dir := fs.String("dir", "", "run directory (default: fresh temp dir)")
	armName := fs.String("arm", "", "named harness configuration (recorded in the manifest)")
	switches := stringSliceFlag{}
	fs.Var(&switches, "set", "mechanism switch key=value (repeatable; forwarded to task env, recorded in manifest)")
	concurrency := fs.Int("concurrency", 4, "parallel tasks")
	taskTimeout := fs.Int("timeout", eval.QuickDefaultTimeout, "per-task wall bound in seconds")
	filter := fs.String("filter", "", "run only tasks whose name contains the substring")
	list := fs.Bool("list", false, "list tasks and exit")
	skipDocker := fs.Bool("skip-docker", false, "skip tasks needing the docker daemon")
	model := fs.String("model", "", "model id for the manifest annotation")
	provider := fs.String("provider", "", "provider for the manifest annotation")
	effort := fs.String("effort", "", "reasoning effort for the manifest annotation")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	if *list {
		names := eval.QuickTaskNames()
		for _, cat := range quickCategories() {
			fmt.Fprintf(os.Stdout, "[%s]\n", cat)
			for _, t := range eval.QuickTasks() {
				if t.Category == cat {
					fmt.Fprintf(os.Stdout, "  %s\n", t.Name)
				}
			}
		}
		_ = names
		return nil
	}
	if err := eval.ValidateArm(*armName); err != nil {
		return fmt.Errorf("eval quick: %w", err)
	}
	switchMap, err := eval.ParseSwitches(switches)
	if err != nil {
		return fmt.Errorf("eval quick: %w", err)
	}
	out := *dir
	if out == "" {
		tmp, err := os.MkdirTemp("", "excalibur-quick-")
		if err != nil {
			return fmt.Errorf("eval quick: %w", err)
		}
		out = tmp
	}
	runner := eval.QuickRunner{
		Dir: out, Concurrency: *concurrency,
		TaskTimeout: time.Duration(*taskTimeout) * time.Second,
		Arm:         *armName, Switches: switchMap,
		Model: *model, Provider: *provider, Effort: *effort,
		Filter: *filter, SkipDocker: *skipDocker,
	}
	ctx := context.Background()
	if !isTerminalEnv() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Minute)
		defer cancel()
	}
	outcomes, err := runner.Run(ctx)
	if err != nil {
		return fmt.Errorf("eval quick: %w", err)
	}
	if err := runner.WriteResults(outcomes); err != nil {
		return fmt.Errorf("eval quick: %w", err)
	}
	var passed, failed, skipped int
	var failedNames []string
	for _, oc := range outcomes {
		switch {
		case oc.Skipped:
			skipped++
		case oc.Passed:
			passed++
		default:
			failed++
			failedNames = append(failedNames, oc.Task)
		}
	}
	sort.Strings(failedNames)
	fmt.Fprintf(os.Stdout, "quick: passed=%d failed=%d skipped=%d in %s (arm %s)\n",
		passed, failed, skipped, out, displayArmName(*armName))
	for _, oc := range outcomes {
		if oc.Skipped {
			fmt.Fprintf(os.Stdout, "  SKIP %s (%s)\n", oc.Task, oc.Detail)
		} else if !oc.Passed {
			fmt.Fprintf(os.Stdout, "  FAIL %s (%s)\n", oc.Task, oc.Detail)
		}
	}
	fmt.Fprintf(os.Stdout, "report in %s/report.json and manifest.json\n", out)
	if failed > 0 {
		return fmt.Errorf("eval quick: %d task(s) failed", failed)
	}
	return nil
}

func quickCategories() []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range eval.QuickTasks() {
		if !seen[t.Category] {
			seen[t.Category] = true
			out = append(out, t.Category)
		}
	}
	sort.Strings(out)
	return out
}

func displayArmName(arm string) string {
	if arm == "" {
		return "default"
	}
	return arm
}

// isTerminalEnv reports whether stdin looks interactive; non-interactive
// quick runs get a backstop context timeout.
func isTerminalEnv() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
