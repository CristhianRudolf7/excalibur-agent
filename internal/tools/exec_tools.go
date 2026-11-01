package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

var execSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"command":    map[string]any{"type": "string", "minLength": 1, "description": "Shell command; chain steps with && and pipes in one call"},
		"workdir":    map[string]any{"type": "string", "description": "Working directory; defaults to the task directory"},
		"timeout_s":  map[string]any{"type": "number", "minimum": 0.25, "maximum": 300, "description": "Wait budget in seconds (yield_ms is the same knob in milliseconds)"},
		"yield_ms":   map[string]any{"type": "integer", "minimum": 250, "maximum": 30000},
		"tty":        map[string]any{"type": "boolean", "description": "Allocate a PTY for interactive programs"},
		"background": map[string]any{"type": "boolean", "description": "Return immediately with the session id; poll with exec_io"},
		// env stays valid for vault secret:// resolution in tool
		// processes; prefer shell VAR=value syntax for plain values.
		"env": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required":             []string{"command"},
	"additionalProperties": false,
})

var execIOSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action":    map[string]any{"type": "string", "enum": []string{"list", "read", "write", "keys", "wait_for", "signal", "close"}},
		"session":   map[string]any{"type": "string"},
		"text":      map[string]any{"type": "string"},
		"keys":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"pattern":   map[string]any{"type": "string"},
		"signal":    map[string]any{"type": "string"},
		"timeout_s": map[string]any{"type": "number", "minimum": 0.25, "maximum": 300},
		"offset":    map[string]any{"type": "integer", "minimum": 0},
		"owner":     map[string]any{"type": "string"},
	},
	"required":             []string{"action"},
	"additionalProperties": false,
})

// ExecTool implements exec: pipe or PTY launch with yield.
type ExecTool struct {
	Sessions *SessionManager
	// SecretResolver resolves secret:// references in env entries
	// inside the tool process (Section 20.1: env destination). It
	// returns the resolved value or an error; nil skips resolution
	// and passes env through unchanged. Resolved values are never
	// logged: only the reference markers reach the journal.
	SecretResolver func(ctx context.Context, ref string) (string, error)
	// RunID, ToolName and Project scope the secret-policy check.
	RunID   string
	Project string
	// Provisioner runs automatic dependency provisioning (W3). Nil
	// means the default provisioner honoring EXCALIBUR_AUTOPROVISION
	// (default auto). Tests inject fakes here.
	Provisioner *Provisioner
}

func (t *ExecTool) sessions() *SessionManager {
	if t.Sessions != nil {
		return t.Sessions
	}
	return NewSessionManager(TerminalDeps{})
}

func (t *ExecTool) Definition() Definition {
	return Definition{
		Name: "exec", Description: "Run a shell command and return its output. Shell-first: chain steps with && and pipes in one call instead of many calls; use workdir for the task directory and exec_io for live sessions",
		InputSchema: execSchema, Effect: "exec", Idempotency: "non_idempotent",
		Cancel: "graceful", Timeout: 300_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Probe: "exit status of a probe command", DryRun: "print command without running",
		Group: "exec", Tags: []string{"exec", "terminal"}, Keywords: []string{"exec", "run", "command", "shell", "terminal"},
	}
}

func (t *ExecTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Command    string   `json:"command"`
		Workdir    string   `json:"workdir"`
		TTY        bool     `json:"tty"`
		YieldMs    int      `json:"yield_ms"`
		TimeoutS   float64  `json:"timeout_s"`
		Background bool     `json:"background"`
		Target     string   `json:"target"`
		Env        []string `json:"env"`
		Owner      string   `json:"owner"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Command) == "" {
		return Result{Status: StatusInputError, Output: "command is required and must not be empty"}, nil
	}
	// Legacy internal keys stay internal defaults: embedded runs are
	// always local, so any target is accepted and ignored instead of
	// failing the call (the 17%-of-exec-calls failure class).
	ignored := ""
	if a.Target != "" && a.Target != "local" {
		ignored = fmt.Sprintf("note: target %q ignored (embedded runs locally)\n", a.Target)
	}
	env := a.Env
	if t.SecretResolver != nil {
		resolved, err := resolveEnvSecrets(ctx, t.SecretResolver, env)
		if err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		env = resolved
	}
	yield := a.YieldMs
	if a.TimeoutS > 0 {
		yield = int(a.TimeoutS * 1000)
	}
	if yield == 0 {
		yield = defaultYieldMs
	}
	if a.Background {
		yield = minYieldMs
	}
	owner := a.Owner
	if owner == "" {
		owner = "default"
	}
	start := time.Now()
	s, out, alive := t.sessions().Start(owner, a.Command, a.Workdir, a.TTY, yield, env)
	dur := time.Since(start)
	var b strings.Builder
	if code, exited := s.Exit(); exited && !alive {
		fmt.Fprintf(&b, "exit: %d  duration: %.1fs  session: %s  alive: false\n", code, dur.Seconds(), s.ID)
	} else if alive {
		next := len(s.SnapshotFull())
		fmt.Fprintf(&b, "exit: running  duration: %.1fs  session: %s  alive: true  next_offset: %d\n", dur.Seconds(), s.ID, next)
		fmt.Fprintf(&b, "to poll: exec_io {\"action\":\"read\",\"session\":%q,\"offset\":%d}\n", string(s.ID), next)
	} else {
		fmt.Fprintf(&b, "exit: unknown  duration: %.1fs  session: %s  alive: false\n", dur.Seconds(), s.ID)
	}
	if ignored != "" {
		b.WriteString(ignored)
	}
	headTail := headTailInline(out, 8000)
	b.WriteString(headTail)
	if out == "" || !strings.HasSuffix(headTail, "\n") {
		b.WriteString("\n")
	}
	if hint := actionableExecHint(a.Command, out); hint != "" {
		b.WriteString(hint)
	}
	if prov := t.provisioner(); prov != nil {
		if report := prov.MaybeProvision(ctx, a.Command, out, a.Workdir); report != "" {
			b.WriteString(report)
		}
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
}

// provisioner returns the effective provisioner: the injected fake in
// tests, otherwise a default honoring EXCALIBUR_AUTOPROVISION. It
// returns nil when provisioning is switched off so the result keeps
// the pre-provisioning bytes.
func (t *ExecTool) provisioner() *Provisioner {
	if t.Provisioner != nil {
		if t.Provisioner.Mode == ProvisionOff {
			return nil
		}
		return t.Provisioner
	}
	if ProvisionModeFromEnv() == ProvisionOff {
		return nil
	}
	return &Provisioner{}
}

func (t *ExecTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Workdir string `json:"workdir"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Workdir != "" {
		return []string{"dir:" + a.Workdir}, nil
	}
	return []string{"exec:default"}, nil
}

// ExecIOTool implements exec_io session operations.
type ExecIOTool struct {
	Sessions *SessionManager
}

func (t *ExecIOTool) sessions() *SessionManager {
	if t.Sessions != nil {
		return t.Sessions
	}
	return NewSessionManager(TerminalDeps{})
}

func (t *ExecIOTool) Definition() Definition {
	return Definition{
		Name: "exec_io", Description: "Operate terminal sessions: list, read, write, keys, wait_for, signal, close",
		InputSchema: execIOSchema, Effect: "exec", Idempotency: "non_idempotent",
		Cancel: "graceful", Timeout: 300_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "exec", Tags: []string{"exec", "terminal"}, Keywords: []string{"session", "terminal", "wait", "signal", "input"},
	}
}

func (t *ExecIOTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Action   string   `json:"action"`
		Session  string   `json:"session"`
		Text     string   `json:"text"`
		Keys     []string `json:"keys"`
		Pattern  string   `json:"pattern"`
		Signal   string   `json:"signal"`
		TimeoutS float64  `json:"timeout_s"`
		Offset   int      `json:"offset"`
		Owner    string   `json:"owner"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	m := t.sessions()
	switch a.Action {
	case "list":
		sums := m.Summaries(a.Owner)
		var b strings.Builder
		fmt.Fprintf(&b, "%d session(s)\n", len(sums))
		for _, s := range sums {
			fmt.Fprintf(&b, "- %s alive=%v owner=%s cmd=%q\n", s.ID, s.Alive, s.Owner, s.Cmd)
			if s.Tail != "" {
				b.WriteString("  tail:\n")
				for _, l := range strings.Split(s.Tail, "\n") {
					b.WriteString("  | " + l + "\n")
				}
			}
		}
		return Result{Status: StatusOK, Output: b.String()}, nil
	case "read":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for read"}, nil
		}
		out, next, exited, err := m.Read(SessionID(a.Session), a.Offset)
		if err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("exited: %v  next_offset: %d\n%s", exited, next, out)}, nil
	case "write":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for write"}, nil
		}
		if err := m.Write(SessionID(a.Session), a.Text); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("wrote %d bytes to %s", len(a.Text), a.Session)}, nil
	case "keys":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for keys"}, nil
		}
		if len(a.Keys) == 0 {
			return Result{Status: StatusInputError, Output: "keys must not be empty"}, nil
		}
		if err := m.Keys(SessionID(a.Session), a.Keys); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("sent keys %v to %s", a.Keys, a.Session)}, nil
	case "wait_for":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for wait_for"}, nil
		}
		timeout := time.Duration(a.TimeoutS * float64(time.Second))
		if a.TimeoutS == 0 {
			timeout = 30 * time.Second
		}
		wctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
		defer cancel()
		out, next, exited, err := m.WaitFor(wctx, SessionID(a.Session), a.Pattern, timeout, a.Offset)
		if err != nil {
			return Result{Status: StatusToolError, Output: fmt.Sprintf("wait_for: %v\noutput so far (next_offset %d):\n%s", err, next, out)}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("matched; exited: %v  next_offset: %d\n%s", exited, next, out)}, nil
	case "signal":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for signal"}, nil
		}
		sig := a.Signal
		if sig == "" {
			sig = "interrupt"
		}
		if err := m.Signal(SessionID(a.Session), sig); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("sent %s to %s", sig, a.Session)}, nil
	case "close":
		if a.Session == "" {
			return Result{Status: StatusInputError, Output: "session is required for close"}, nil
		}
		if err := m.Close(SessionID(a.Session)); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("closed %s", a.Session)}, nil
	default:
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown action %q", a.Action)}, nil
	}
}

func (t *ExecIOTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Session string `json:"session"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if a.Session != "" {
		return []string{"session:" + a.Session}, nil
	}
	return []string{"session:*"}, nil
}

// headTailInline keeps the first and last quarters of out within a
// small inline budget so the model sees the command head (what ran)
// and tail (the freshest lines) without paying the full 40 KB
// pipeline budget on every call. Larger outputs still page through
// the pipeline blob (get_tool_output) and the live session (exec_io).
func headTailInline(out string, budget int) string {
	if len(out) <= budget || budget <= 0 {
		return out
	}
	marker := fmt.Sprintf("\n…[middle %d bytes elided; full output above the pipeline budget pages via get_tool_output]…\n", len(out)-budget)
	keep := budget - len(marker)
	if keep <= 0 {
		return out[:budget]
	}
	head := keep / 2
	return out[:head] + marker + out[len(out)-(keep-head):]
}

// actionableExecHint turns a failed command into a next action:
// missing binaries name the available package managers and detected
// tools, missing directories name the fix. Empty when there is
// nothing actionable to add.
func actionableExecHint(command, out string) string {
	lower := strings.ToLower(out)
	if strings.Contains(lower, "command not found") || strings.Contains(lower, ": not found") {
		missing := guessMissingCommand(command, out)
		prof := DetectEnv()
		managers := strings.Join(prof.PackageManagers, ", ")
		if managers == "" {
			managers = "none detected"
		}
		var have []string
		for _, tool := range []string{"git", "python3", "node", "go", "rustc", "docker", "perl"} {
			if prof.Tools[tool] {
				have = append(have, tool)
			}
		}
		hint := fmt.Sprintf("hint: command not found%s. Available package managers: %s.", missing, managers)
		if len(have) > 0 {
			hint += " Present: " + strings.Join(have, ", ") + "."
		}
		return hint + " Install it (one exec: install && verify with `command -v <bin>`), or rework the step to use a present tool.\n"
	}
	if strings.Contains(lower, "no such file or directory") {
		return "hint: no such file or directory. Check the path with `ls` on its parent (use workdir for the task directory); create it with mkdir -p or write_file before retrying.\n"
	}
	if strings.Contains(lower, "permission denied") {
		return "hint: permission denied. Check ownership with `ls -la`; prefer user-local paths over system directories.\n"
	}
	return ""
}

// guessMissingCommand extracts the missing binary name from common
// shell diagnostics ("sh: 1: foo: not found", "bash: foo: command
// not found").
func guessMissingCommand(command, out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		// Forms: "<shell>: [lineno:] <bin>: command not found",
		// "<shell>: [lineno:] <bin>: not found".
		if idx := strings.Index(line, ": not found"); idx >= 0 {
			before := strings.TrimSpace(line[:idx])
			if i := strings.LastIndex(before, ":"); i >= 0 {
				cand := strings.TrimSpace(before[i+1:])
				if cand != "" && !strings.Contains(cand, " ") {
					return ": " + cand
				}
			}
		}
		if idx := strings.Index(line, ": command not found"); idx >= 0 {
			before := strings.TrimSpace(line[:idx])
			if i := strings.LastIndex(before, ":"); i >= 0 {
				cand := strings.TrimSpace(before[i+1:])
				if cand != "" && !strings.Contains(cand, " ") {
					return ": " + cand
				}
			}
		}
	}
	// Fall back to the command's own first word.
	if fields := strings.Fields(command); len(fields) > 0 {
		base := fields[0]
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		return ": " + base
	}
	return ""
}

// resolveEnvSecrets resolves secret:// values in KEY=VALUE env entries
// inside the tool process. Entries without a secret reference pass
// through unchanged; the journal keeps the references, never values.
func resolveEnvSecrets(ctx context.Context, resolve func(context.Context, string) (string, error), env []string) ([]string, error) {
	if len(env) == 0 {
		return env, nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, val, hasEq := splitEnv(entry)
		if !hasEq || !isSecretRef(val) {
			out = append(out, entry)
			continue
		}
		resolved, err := resolve(ctx, val)
		if err != nil {
			return nil, fmt.Errorf("exec: cannot resolve secret for %s: %w", key, err)
		}
		out = append(out, key+"="+resolved)
	}
	return out, nil
}

func splitEnv(entry string) (key, val string, hasEq bool) {
	for i := 0; i < len(entry); i++ {
		if entry[i] == '=' {
			return entry[:i], entry[i+1:], true
		}
	}
	return entry, "", false
}

// isSecretRef reports whether s is a vault secret reference.
func isSecretRef(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "secret://") || (strings.HasPrefix(t, "secret:") && len(t) > len("secret:"))
}

var _ = sort.Strings
var _ = time.Second
