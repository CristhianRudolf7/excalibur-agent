package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

// globalJSON mirrors the top-level --json flag for commands that do
// not repeat it.
var globalJSON bool

func wantJSON(flags *flag.FlagSet) *bool {
	return flags.Bool("json", false, "machine-readable output")
}

func jsonEnabled(set bool) bool { return set || globalJSON }

func printJSON(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(os.Stdout, "%s\n", buf)
	return err
}

// runExecCmd implements `excalibur exec` (Section 6.5): one
// non-interactive request, exit code from the final run state.
func runExecCmd(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	project := fs.String("project", "", "project directory scope")
	schema := fs.String("output-schema", "", "validate the final result against this JSON schema file")
	asJSON := wantJSON(fs)
	budget := fs.Float64("budget-usd", -1, "spend cap in USD (negative means unset)")
	maxSteps := fs.Int("max-steps", 0, "step cap (0 means profile default)")
	timeout := fs.Int64("timeout", 0, "wall-time bound in seconds (0 means unset)")
	profile := fs.String("profile", "", "execution profile: default|delegate|eval")
	noMemory := fs.Bool("no-memory", false, "disable memory entirely (delegate profile)")
	ephemeral := fs.Bool("ephemeral", false, "temporary database removed on exit (not with default)")
	override := fs.String("profile-override", "", "declare an eval profile deviation (recorded, never compares clean)")
	logsDir := fs.String("logs-dir", "", "trial log directory (default: /logs/agent when present)")
	neverAsk := fs.Bool("never-ask", false, "assume instead of asking (exec always assumes)")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	rest = append(rest[:0], rest...)
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur exec \"<request>\" [flags]")}
	}
	request := rest[0]
	opts := cli.ExecOptions{
		Request:         request,
		ProjectDir:      *project,
		OutputSchema:    *schema,
		EmitJSON:        jsonEnabled(*asJSON),
		Emit:            os.Stdout,
		Profile:         cli.ProfileName(*profile),
		NoMemory:        *noMemory,
		Ephemeral:       *ephemeral,
		ProfileOverride: *override,
		LogsDir:         *logsDir,
		NeverAsk:        *neverAsk || true,
		MaxSteps:        *maxSteps,
		TimeoutSecs:     *timeout,
	}
	if *budget >= 0 {
		opts.BudgetUSD = budget
	}
	if *profile != "" {
		p, err := cli.ParseProfile(*profile)
		if err != nil {
			return &cli.CodedError{Code: cli.ExitError, Err: err}
		}
		opts.Profile = p
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if wall := cli.EffectiveBudgetsFor(opts, opts.Profile).WallSecs; wall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(wall)*time.Second)
		defer cancel()
	}
	done := make(chan struct{})
	defer close(done)
	safego.Go("exec-shutdown", nil, func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
			os.Exit(cli.ExitPaused)
		}
	})
	outcome, err := cli.RunExec(ctx, opts)
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		// The JSONL flow already streamed; close with the outcome.
		return printJSON(outcomeSummary(outcome))
	}
	fmt.Fprintf(os.Stdout, "%s\n", outcome.Summary)
	if outcome.ExitCode == 0 {
		return nil
	}
	return &cli.CodedError{Code: outcome.ExitCode, Err: fmt.Errorf("%s", outcome.Summary)}
}

func outcomeSummary(o *cli.ExecOutcome) map[string]any {
	m := map[string]any{
		"state": string(o.State), "summary": o.Summary,
		"task_id": string(o.TaskID), "run_id": string(o.RunID),
		"exit_code": o.ExitCode, "profile": string(o.Profile),
	}
	if o.Reason != "" {
		m["reason"] = o.Reason
	}
	if len(o.Assumptions) > 0 {
		m["assumptions"] = o.Assumptions
	}
	return m
}
