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

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

// globalJSON mirrors the top-level --json flag for commands that do
// not repeat it.
var globalJSON bool

// Enabled only by the binary entry point, never by in-process CLI tests.
var requestReexec bool

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
	instructionFile := fs.String("instruction-file", "", "read the request text from this private file (written by the argv hand-off; see EXCALIBUR_HIDE_INSTRUCTION_ARGV)")
	// Exec accepts arbitrary instruction text, including leading bullet dashes.
	rest, err := parseCommand(fs, args, os.Getenv("EXCALIBUR_EXEC_LEADING_DASH") != "0")
	if err != nil {
		return err
	}
	fromFile := *instructionFile != "" || os.Getenv(cli.InstructionFileEnv) != ""
	if !fromFile && len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur exec \"<request>\" [flags]")}
	}
	var request string
	if fromFile {
		text, err := cli.ConsumeInstructionFile(*instructionFile)
		if err != nil {
			return &cli.CodedError{Code: cli.ExitError, Err: err}
		}
		request = text
	} else {
		request = rest[len(rest)-1]
		if requestReexec && argvReexecSupported() && cli.HideInstructionArgvEnabled() {
			// Item 22: the request leaves argv before any other work, so
			// process listings inside the task never show the task text.
			// The re-executed process reads it back from the file.
			argv, env, path, err := cli.PrepareInstructionArgv(os.Args, os.Environ(), request)
			if err != nil {
				return &cli.CodedError{Code: cli.ExitError, Err: err}
			}
			if path != "" {
				if err := platform.Reexec(argv, env); err != nil {
					_ = os.Remove(path)
					return fmt.Errorf("hide instruction argv: %w", err)
				}
				return nil
			}
		}
	}
	// Exec scope (items 93 and 62 and 70): the boundary refusals and the
	// autonomous session defaults apply to this process only.
	restoreScope := tools.SetExecScope(tools.ExecScope{Active: true, HarnessPaths: cli.ExecScopePaths(*logsDir)})
	defer restoreScope()
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
	if wall := cli.EffectiveBudgetsFor(opts, opts.Profile).HardWallSecs; wall > 0 {
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
