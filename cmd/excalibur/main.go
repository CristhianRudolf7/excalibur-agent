// Command excalibur is the single Excalibur binary: daemon, CLI and
// non-interactive exec. This wave-0 skeleton exposes the full command
// surface of Sections 3.5 and 3.6 as thin dispatchers; every command
// that has no backend yet fails with a stable "not implemented yet"
// error and ExitNotImplemented. No agent or provider logic lives here:
// commands will call the shared core in internal/ (H4 for exec, H8 for
// task/mcp, H9 for the daemon commands).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

// Exit codes. The 0/2/3/4/5/130 mapping mirrors the exec final-state
// codes of Section 6.5; 69 (EX_UNAVAILABLE) marks commands whose
// backend has not landed yet.
const (
	ExitOK             = 0
	ExitError          = 1
	ExitPending        = 2
	ExitBlocked        = 3
	ExitPaused         = 4
	ExitFailed         = 5
	ExitNotImplemented = 69
	ExitCanceled       = 130
)

// errNotImplemented is returned by every command without a backend.
var errNotImplemented = errors.New("not implemented yet")

// command is one CLI verb.
type command struct {
	summary string
	sub     map[string]command
	run     func(args []string) error
}

// notImpl returns a run func that always fails as not implemented.
func notImpl(name string) func([]string) error {
	return func([]string) error {
		return fmt.Errorf("%s: %w", name, errNotImplemented)
	}
}

func commands() map[string]command {
	taskSub := map[string]command{
		"start":  {summary: "Create a task and return immediately", run: taskStart},
		"status": {summary: "Task status snapshot", run: taskStatus},
		"wait":   {summary: "Wait for completion or a pending answer", run: taskWait},
		"result": {summary: "Delivery report", run: taskResult},
		"reply":  {summary: "Answer a question or add a directive", run: taskReply},
		"cancel": {summary: "Cancel a task", run: func(a []string) error { return taskControl("cancel", cli.Cancel, a) }},
		"pause":  {summary: "Pause a task", run: func(a []string) error { return taskControl("pause", cli.Pause, a) }},
		"resume": {summary: "Resume a task", run: func(a []string) error { return taskControl("resume", cli.Resume, a) }},
		"list":   {summary: "List tasks", run: taskList},
		"logs":   {summary: "Live event stream as JSONL", run: taskLogs},
	}
	mcpSub := map[string]command{
		"install": {summary: "Install MCP client fragments or delegate skill", run: installMCP},
	}
	daemonSub := map[string]command{
		"start": {summary: "Start the daemon", run: notImpl("daemon start")},
		"stop":  {summary: "Stop the daemon", run: notImpl("daemon stop")},
	}
	vaultSub := map[string]command{
		"set":      {summary: "Create or update a secret (value via tty/stdin only)", run: notImpl("vault set")},
		"get":      {summary: "Reveal a secret (explicit request only)", run: notImpl("vault get")},
		"list":     {summary: "List secret metadata, never values", run: notImpl("vault list")},
		"show":     {summary: "Show one entry's metadata", run: notImpl("vault show")},
		"edit":     {summary: "Change scopes, tools or domains", run: notImpl("vault edit")},
		"rm":       {summary: "Delete a secret (confirmed, irreversible)", run: notImpl("vault rm")},
		"generate": {summary: "Generate a random value", run: notImpl("vault generate")},
		"lock":     {summary: "Lock the vault", run: notImpl("vault lock")},
		"unlock":   {summary: "Unlock the vault", run: notImpl("vault unlock")},
		"rotate":   {summary: "Rotate the data-encryption key", run: notImpl("vault rotate")},
		"export":   {summary: "Export an encrypted backup", run: notImpl("vault export")},
		"import":   {summary: "Import an encrypted backup", run: notImpl("vault import")},
		"usage":    {summary: "Show where a secret was used", run: notImpl("vault usage")},
	}
	extSub := map[string]command{
		"new":     {summary: "Scaffold an extension", run: runExt("new")},
		"list":    {summary: "List extensions", run: runExt("list")},
		"install": {summary: "Install an extension", run: runExt("install")},
		"enable":  {summary: "Enable an extension", run: runExt("enable")},
		"disable": {summary: "Disable an extension", run: runExt("disable")},
		"remove":  {summary: "Remove an extension", run: runExt("remove")},
		"logs":    {summary: "Show extension logs", run: runExt("logs")},
		"test":    {summary: "Run the contract conformance suite", run: runExt("test")},
		"doctor":  {summary: "Report validation, health and versions", run: runExt("doctor")},
	}
	evalSub := map[string]command{
		"smoke":  {summary: "Run the smoke evaluation (Section 23.2)", run: runEvalSmoke},
		"report": {summary: "Render an evaluation report (Section 23.7)", run: runEvalReport},
	}
	planSub := map[string]command{
		"approve": {summary: "Approve a pending macro plan", run: notImpl("plan approve")},
		"deny":    {summary: "Reject a pending macro plan", run: notImpl("plan deny")},
	}
	return map[string]command{
		"chat":     {summary: "Open a conversation turn with the master", run: notImpl("chat")},
		"exec":     {summary: "Run a request non-interactively (Section 6.5)", run: runExecCmd},
		"status":   {summary: "Deterministic status snapshot", run: notImpl("status")},
		"tasks":    {summary: "List tasks", run: notImpl("tasks")},
		"projects": {summary: "Manage registered projects", run: notImpl("projects")},
		"memory":   {summary: "Query memory", run: notImpl("memory")},
		"skills":   {summary: "Search and load skills", run: notImpl("skills")},
		"ext":      {summary: "Manage extensions (Section 14.12)", sub: extSub},
		"vault":    {summary: "Manage the secrets vault (Section 20.1)", sub: vaultSub},
		"eval":     {summary: "Run evaluations (Section 23)", sub: evalSub},
		"daemon":   {summary: "Control the daemon", sub: daemonSub},
		"install":  {summary: "Install the daemon and clients", run: runInstall},
		"update":   {summary: "Update the binary", run: runUpdate},
		"task":     {summary: "Async delegated work (Section 3.6)", sub: taskSub},
		"mcp":      {summary: "Run the MCP delegation server over stdio", sub: mcpSub, run: serveMCP},
		"acp":      {summary: "Run the ACP adapter over stdio", run: serveACP},
		"plan":     {summary: "Approve or deny macro plans", sub: planSub},
	}
}

// topUsage prints the global help.
func topUsage(cmds map[string]command) string {
	var b strings.Builder
	b.WriteString("Usage: excalibur [--json] <command> [args]\n\nCommands:\n")
	names := []string{
		"chat", "exec", "status", "tasks", "projects", "memory", "skills",
		"ext", "vault", "eval", "daemon", "install", "update", "task", "mcp", "acp", "plan",
	}
	for _, n := range names {
		fmt.Fprintf(&b, "  %-10s %s\n", n, cmds[n].summary)
	}
	b.WriteString("\nRun 'excalibur <command> --help' for subcommands.\n")
	return b.String()
}

func main() { os.Exit(run(os.Args[1:])) }

func run(argv []string) int {
	if len(argv) > 0 && argv[0] == "__run-task" {
		if len(argv) != 2 {
			fmt.Fprintln(os.Stderr, "excalibur __run-task: need a task file")
			return ExitError
		}
		if err := cli.RunTaskFile(argv[1]); err != nil {
			fmt.Fprintf(os.Stderr, "excalibur __run-task: %v\n", err)
			return cli.ExitCode(err)
		}
		return ExitOK
	}
	fs := flag.NewFlagSet("excalibur", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitError
	}
	globalJSON = *asJSON
	rest := fs.Args()
	cmds := commands()
	if len(rest) == 0 || rest[0] == "--help" || rest[0] == "-h" || rest[0] == "help" {
		fmt.Fprint(os.Stdout, topUsage(cmds))
		return ExitOK
	}
	name, args := rest[0], rest[1:]
	cmd, ok := cmds[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "excalibur: unknown command %q\n", name)
		fmt.Fprint(os.Stderr, topUsage(cmds))
		return ExitError
	}
	if len(cmd.sub) > 0 {
		if len(args) == 0 {
			// Bare `excalibur mcp` starts the server; other families
			// need a subcommand.
			if cmd.run != nil {
				if err := cmd.run(nil); err != nil {
					return report(name, err)
				}
				return ExitOK
			}
			fmt.Fprintf(os.Stdout, "Usage: excalibur %s <subcommand>\n\nSubcommands:\n", name)
			for subName, sub := range cmd.sub {
				fmt.Fprintf(os.Stdout, "  %-10s %s\n", subName, sub.summary)
			}
			return ExitOK
		}
		if args[0] == "--help" || args[0] == "-h" {
			fmt.Fprintf(os.Stdout, "Usage: excalibur %s <subcommand>\n\nSubcommands:\n", name)
			for subName, sub := range cmd.sub {
				fmt.Fprintf(os.Stdout, "  %-10s %s\n", subName, sub.summary)
			}
			return ExitOK
		}
		sub, ok := cmd.sub[args[0]]
		if !ok {
			fmt.Fprintf(os.Stderr, "excalibur %s: unknown subcommand %q\n", name, args[0])
			return ExitError
		}
		if err := sub.run(args[1:]); err != nil {
			return report(name+" "+args[0], err)
		}
		return ExitOK
	}
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprintf(os.Stdout, "Usage: excalibur %s [flags]\n\n%s\n", name, cmd.summary)
			return ExitOK
		}
	}
	if cmd.run == nil {
		fmt.Fprintf(os.Stderr, "excalibur %s: %v\n", name, errNotImplemented)
		return ExitNotImplemented
	}
	if err := cmd.run(args); err != nil {
		return report(name, err)
	}
	return ExitOK
}

// runExt runs one excalibur ext subcommand through the thin CLI layer
// over the shared loader and conformance suite (Section 14.12). The
// ext family is local-first: it works without a daemon on the local
// extensions root.
func runExt(sub string) func([]string) error {
	return func(args []string) error {
		out, code := cli.RunExt(append([]string{sub}, args...))
		if out != "" {
			fmt.Fprintln(os.Stdout, out)
		}
		if code != 0 {
			return fmt.Errorf("ext %s failed", sub)
		}
		return nil
	}
}

// report prints the failure and maps it to a stable exit code.
func report(what string, err error) int {
	if errors.Is(err, errNotImplemented) {
		fmt.Fprintf(os.Stderr, "excalibur %s: %v\n", what, err)
		return ExitNotImplemented
	}
	code := cli.ExitCode(err)
	if code == ExitOK {
		code = ExitError
	}
	fmt.Fprintf(os.Stderr, "excalibur %s: %v\n", what, err)
	return code
}
