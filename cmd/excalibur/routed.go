package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

// Thin CLI families routed through the daemon API when it is running
// (Sections 3.5, 3.6). Every command prints the gate result as JSON;
// without a daemon they fail with a stable hint instead of silent
// local behavior, because these families have no embedded backend.

func needDaemon() error {
	if cli.DaemonReachable() {
		return nil
	}
	return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("no daemon running; start one with `excalibur daemon start`")}
}

func printGateResult(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(buf))
	return nil
}

func gateRun(method string, params any) error {
	if err := needDaemon(); err != nil {
		return err
	}
	raw, err := cli.GateJSON(method, params)
	if err != nil {
		return err
	}
	return printGateResult(raw)
}

func runChat(args []string) error {
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	conv := fs.String("conversation", "default", "conversation id")
	clientMsg := fs.String("client-msg-id", "", "idempotency id (default: generated)")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur chat \"<message>\" [--conversation ID]")}
	}
	msgID := *clientMsg
	if msgID == "" {
		msgID = "cli-" + fmt.Sprint(os.Getpid())
	}
	return gateRun(api.MethodConversationSend, map[string]any{
		"conversation_id": *conv, "client_msg_id": msgID, "text": rest[0],
	})
}

func runStatusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	runID := fs.String("run", "", "run id (default: list all runs)")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	if *runID == "" {
		return gateRun(api.MethodRunList, map[string]any{})
	}
	return gateRun(api.MethodRunStatus, map[string]any{"run_id": *runID})
}

func runTasksCmd(args []string) error {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	return gateRun(api.MethodTaskList, map[string]any{})
}

func runProjectsCmd(args []string) error {
	fs := flag.NewFlagSet("projects", flag.ContinueOnError)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	return gateRun(api.MethodProjectList, map[string]any{})
}

func runMemoryCmd(args []string) error {
	fs := flag.NewFlagSet("memory", flag.ContinueOnError)
	scopes := fs.String("scopes", "general", "comma-separated scope set")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur memory \"<query>\" [--scopes general]")}
	}
	return gateRun(api.MethodMemoryQuery, map[string]any{"query": rest[0], "scopes": splitCSV(*scopes)})
}

func runSkillsCmd(args []string) error {
	fs := flag.NewFlagSet("skills", flag.ContinueOnError)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur skills \"<query>\"")}
	}
	return gateRun(api.MethodSkillsSearch, map[string]any{"query": rest[0]})
}

func runPlanApprove(args []string) error {
	fs := flag.NewFlagSet("plan approve", flag.ContinueOnError)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur plan approve <plan-id>")}
	}
	return gateRun(api.MethodPlanApprove, map[string]any{"plan_id": rest[0]})
}

func runPlanDeny(args []string) error {
	fs := flag.NewFlagSet("plan deny", flag.ContinueOnError)
	reason := fs.String("reason", "", "rejection reason")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur plan deny <plan-id> [--reason R]")}
	}
	return gateRun(api.MethodPlanDeny, map[string]any{"plan_id": rest[0], "reason": *reason})
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if part := s[start:i]; part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
