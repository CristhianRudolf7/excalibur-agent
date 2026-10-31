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
		"command":  map[string]any{"type": "string", "minLength": 1},
		"workdir":  map[string]any{"type": "string"},
		"tty":      map[string]any{"type": "boolean"},
		"yield_ms": map[string]any{"type": "integer", "minimum": 250, "maximum": 30000},
		"target":   map[string]any{"type": "string"},
		"env":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"owner":    map[string]any{"type": "string"},
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
}

func (t *ExecTool) sessions() *SessionManager {
	if t.Sessions != nil {
		return t.Sessions
	}
	return NewSessionManager(TerminalDeps{})
}

func (t *ExecTool) Definition() Definition {
	return Definition{
		Name: "exec", Description: "Run a shell command by pipe or PTY; returns a session id while alive",
		InputSchema: execSchema, Effect: "exec", Idempotency: "non_idempotent",
		Cancel: "graceful", Timeout: 300_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Probe: "exit status of a probe command", DryRun: "print command without running",
		Group: "exec", Tags: []string{"exec", "terminal"}, Keywords: []string{"exec", "run", "command", "shell", "terminal"},
	}
}

func (t *ExecTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Command string   `json:"command"`
		Workdir string   `json:"workdir"`
		TTY     bool     `json:"tty"`
		YieldMs int      `json:"yield_ms"`
		Target  string   `json:"target"`
		Env     []string `json:"env"`
		Owner   string   `json:"owner"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if a.Target != "" && a.Target != "local" {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown target %q (remote nodes land in H13)", a.Target)}, nil
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
	if yield == 0 {
		yield = defaultYieldMs
	}
	owner := a.Owner
	if owner == "" {
		owner = "default"
	}
	s, out, alive := t.sessions().Start(owner, a.Command, a.Workdir, a.TTY, yield, env)
	var b strings.Builder
	fmt.Fprintf(&b, "session: %s  alive: %v\n", s.ID, alive)
	b.WriteString(out)
	if out != "" && !strings.HasSuffix(out, "\n") {
		b.WriteString("\n")
	}
	return Result{Status: StatusOK, Output: b.String()}, nil
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
