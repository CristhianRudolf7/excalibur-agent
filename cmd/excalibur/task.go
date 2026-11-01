package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// noSignalContext returns a context canceled by SIGINT/SIGTERM so a
// user interrupt maps to exit 130; SIGTERM carries the external-timeout cause.
func noSignalContext() context.Context {
	if !engine.DeadlineEnabled() {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		_ = stop
		return ctx
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	safego.Go("cli-signals", nil, func() {
		sig := <-signals
		signal.Stop(signals)
		if sig == syscall.SIGTERM {
			cancel(cli.ErrTermination)
		} else {
			cancel(context.Canceled)
		}
	})
	return ctx
}

func projectFlag(fs *flag.FlagSet) *string {
	return fs.String("project", "", "project directory (default: user state)")
}

func timeoutFlag(fs *flag.FlagSet) *int64 {
	return fs.Int64("timeout", 300, "wait bound in seconds")
}

// taskStart implements `excalibur task start`.
func taskStart(args []string) error {
	fs := flag.NewFlagSet("task start", flag.ContinueOnError)
	project := projectFlag(fs)
	profile := fs.String("profile", "", "execution profile (default: delegate)")
	noMemory := fs.Bool("no-memory", false, "disable memory entirely")
	noWorktree := fs.Bool("no-worktree", false, "do not use a git worktree")
	worktree := fs.Bool("worktree", false, "force a git worktree")
	budget := fs.Float64("budget-usd", -1, "spend cap in USD (negative means unset)")
	maxSteps := fs.Int("max-steps", -1, "step cap (negative means unset)")
	timeout := fs.Int64("timeout", -1, "wall-time bound in seconds (negative means unset)")
	neverAsk := fs.Bool("never-ask", false, "assume instead of parking a question")
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	rest = append(rest[:0], rest...)
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task start \"<request>\" [flags]")}
	}
	opts := cli.StartOptions{Request: rest[0], ProjectDir: *project, NeverAsk: *neverAsk}
	if *profile != "" {
		opts.Profile = *profile
	} else {
		opts.Profile = string(cli.ProfileDelegate)
	}
	opts.NoMemory = *noMemory
	switch {
	case *worktree:
		t := true
		opts.Worktree = &t
	case *noWorktree:
		t := false
		opts.Worktree = &t
	}
	if *budget >= 0 {
		opts.BudgetUSD = budget
	}
	if *maxSteps >= 0 {
		opts.MaxSteps = maxSteps
	}
	if *timeout >= 0 {
		opts.TimeoutS = timeout
	}
	res, err := cli.Start(opts)
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		return printJSON(map[string]any{
			"task_id": string(res.TaskID), "short": res.Short, "status": string(res.Status),
		})
	}
	fmt.Fprintf(os.Stdout, "{\"task_id\": %q, \"status\": %q}\n", res.TaskID, res.Status)
	return nil
}

// taskStatus implements `excalibur task status`.
func taskStatus(args []string) error {
	fs := flag.NewFlagSet("task status", flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task status <id>")}
	}
	st, err := cli.Status(*project, rest[0])
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		return printJSON(st)
	}
	fmt.Fprintf(os.Stdout, "%s\n", taskLine(st))
	return nil
}

// taskWait implements `excalibur task wait` with the exec exit codes.
func taskWait(args []string) error {
	fs := flag.NewFlagSet("task wait", flag.ContinueOnError)
	project := projectFlag(fs)
	timeout := timeoutFlag(fs)
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task wait <id> [--timeout S]")}
	}
	st, err := cli.Wait(*project, rest[0], *timeout)
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		if err := printJSON(st); err != nil {
			return err
		}
		if code := cli.CodeForTaskState(st.State); code != 0 && terminalForWait(st) {
			return &cli.CodedError{Code: code, Err: fmt.Errorf("%s", st.Summary)}
		}
		return nil
	}
	fmt.Fprintf(os.Stdout, "%s\n", taskLine(st))
	if code := cli.CodeForTaskState(st.State); code != 0 && terminalForWait(st) {
		return &cli.CodedError{Code: code, Err: fmt.Errorf("%s", st.Summary)}
	}
	return nil
}

func terminalForWait(st *cli.TaskStatus) bool {
	switch st.State {
	case "completed", "completed_with_pending", "blocked", "paused", "failed", "canceled":
		return true
	default:
		return false
	}
}

// taskResult implements `excalibur task result`.
func taskResult(args []string) error {
	fs := flag.NewFlagSet("task result", flag.ContinueOnError)
	project := projectFlag(fs)
	format := fs.String("format", "json", "output format: json|md")
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task result <id> [--format md]")}
	}
	res, err := cli.Result(*project, rest[0])
	if err != nil {
		return err
	}
	if *format == "md" {
		fmt.Fprintf(os.Stdout, "# Task %s (%s)\n\n%s\n", res.Short, res.State, res.Summary)
		if res.Branch != "" {
			fmt.Fprintf(os.Stdout, "\nBranch: %s\n", res.Branch)
		}
		if res.DiffStat != "" {
			fmt.Fprintf(os.Stdout, "\n```\n%s\n```\n", res.DiffStat)
		}
		return nil
	}
	if jsonEnabled(*asJSON) || *format == "json" {
		return printJSON(res)
	}
	return printJSON(res)
}

// taskReply implements `excalibur task reply`.
func taskReply(args []string) error {
	fs := flag.NewFlagSet("task reply", flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task reply <id> \"<text>\"")}
	}
	st, err := cli.Reply(*project, rest[0], rest[1])
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		return printJSON(st)
	}
	fmt.Fprintf(os.Stdout, "%s\n", taskLine(st))
	return nil
}

func taskControl(name string, fn func(string, string) (*cli.TaskStatus, error), args []string) error {
	fs := flag.NewFlagSet("task "+name, flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := wantJSON(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task %s <id>", name)}
	}
	st, err := fn(*project, rest[0])
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		return printJSON(st)
	}
	fmt.Fprintf(os.Stdout, "%s\n", taskLine(st))
	return nil
}

// taskList implements `excalibur task list`.
func taskList(args []string) error {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	project := projectFlag(fs)
	asJSON := wantJSON(fs)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	items, err := cli.ListTasks(*project)
	if err != nil {
		return err
	}
	if jsonEnabled(*asJSON) {
		return printJSON(items)
	}
	for _, st := range items {
		fmt.Fprintf(os.Stdout, "%s\n", taskLine(st))
	}
	return nil
}

// taskLogs implements `excalibur task logs`.
func taskLogs(args []string) error {
	fs := flag.NewFlagSet("task logs", flag.ContinueOnError)
	project := projectFlag(fs)
	follow := fs.Bool("follow", false, "follow new events")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur task logs <id> [--follow]")}
	}
	events, err := cli.TaskLogs(*project, rest[0])
	if err != nil {
		return err
	}
	for _, ev := range events {
		buf, err := marshalLine(ev)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "%s\n", buf)
	}
	_ = follow
	return nil
}

func taskLine(st *cli.TaskStatus) string {
	if st.Question != "" {
		return fmt.Sprintf("%s %s question=%q", st.Short, st.State, st.Question)
	}
	if st.Summary != "" {
		return fmt.Sprintf("%s %s %s", st.Short, st.State, st.Summary)
	}
	return fmt.Sprintf("%s %s", st.Short, st.State)
}

func marshalLine(v any) (string, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
