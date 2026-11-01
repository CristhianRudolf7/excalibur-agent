package tools

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
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

// Exec result switches. All default on; setting one to 0/off restores
// the previous behaviour of the part it governs (see docs/development/
// agent-quality.md, exec result contract).
const (
	// ExecErrorStatusSwitch governs exit status reporting. A non-zero
	// exit is a tool error (is_error, trial exit_code), signal exits are
	// named, hints fire only on a failed exit, and probe commands carry
	// the "not a failure" note.
	ExecErrorStatusSwitch = "EXCALIBUR_EXEC_ERROR_STATUS"
	// Added warnings, notes and hint wording: exit-0 failure text, the
	// identical re-run note, the python test-only note, the missing
	// directory listing, running-session lines and owner handling.
	ExecWarningsSwitch = "EXCALIBUR_EXEC_WARNINGS"
	// Clipped output: pip notice lines are dropped, the middle of long
	// output is stored and the elision marker names the retrieval ref.
	ExecElideRefSwitch = "EXCALIBUR_EXEC_ELIDE_REF"
)

const (
	// execInlineBudget is the body size kept inline (bytes).
	execInlineBudget = 8000
	// execTailKeep is the tail kept after an elision: errors live at the end.
	execTailKeep = 2000
	// execMarkerReserve leaves room for the elision marker and its ref.
	execMarkerReserve = 256
	// execListCap caps the parent-directory listing of a missing path.
	execListCap = 20
	// execScanCap bounds the walk that checks for file changes; past it
	// the answer is "changed", so the identical-call note never misleads.
	execScanCap = 5000
	// execClockSlack widens the change check: file timestamps come from a
	// coarse kernel clock, so a write just before a run ended can carry an
	// earlier mtime. Erring towards "changed" only suppresses the note.
	execClockSlack = 20 * time.Millisecond
	// execLedgerCap bounds the number of distinct commands remembered.
	execLedgerCap = 256
	// execScriptLimit bounds the python script read for the test-only note.
	execScriptLimit = 512 << 10
)

// execErrorStatusEnabled reports whether exit status drives the result status.
func execErrorStatusEnabled() bool { return !off(ExecErrorStatusSwitch) }

// execWarningsEnabled reports whether the added warnings and notes are shown.
func execWarningsEnabled() bool { return !off(ExecWarningsSwitch) }

// execElideRefEnabled reports whether elided output is stored and referenced.
func execElideRefEnabled() bool { return !off(ExecElideRefSwitch) }

// execIOVisible reports whether exec_io is part of the current tool
// surface. The eval surface (and the default surface, which reuses its
// tool list) adds exec_io only while friction repair is on.
func execIOVisible() bool { return FrictionRepairEnabled() }

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
	// Workdir is the run's project directory: commands without a
	// workdir (or with a relative one) run there, like the file tools
	// resolve relative paths (EXCALIBUR_RUN_WORKDIR).
	Workdir string
	// Outputs stores the full text of elided output so the ref printed
	// in the elision marker resolves through get_tool_output. It must be
	// the store get_tool_output reads. Nil prints the session id instead.
	Outputs OutputStore

	ledger execLedger
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
	// Unknown target values never fail the call: embedded runs are always
	// local (the 17%-of-exec-calls failure class). The value is ignored
	// and the result says so.
	ignored := ""
	if a.Target != "" && a.Target != "local" {
		ignored = "target ignored: commands run locally\n"
	}
	// command is the text the model wrote; a.Command may be rewritten
	// below by the command-deadline wrapper.
	command := a.Command
	if t.Workdir != "" && RunWorkdirEnabled() {
		if a.Workdir == "" {
			a.Workdir = t.Workdir
		} else if !filepath.IsAbs(a.Workdir) {
			a.Workdir = filepath.Join(t.Workdir, a.Workdir)
		}
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
	call := t.ledger.begin()
	if bound, ok := ctx.Value(commandDeadlineKey{}).(time.Duration); ok && bound > 0 {
		// Quote the entire original command as one shell argument.
		a.Command = fmt.Sprintf("timeout --signal=TERM --kill-after=1s %.3fs '%s' -c '%s'", max(bound.Seconds(), 0.001), strings.ReplaceAll(DetectShell(), "'", "'\"'\"'"), strings.ReplaceAll(a.Command, "'", "'\"'\"'"))
		if yield == 0 || time.Duration(yield)*time.Millisecond > bound {
			yield = int(bound / time.Millisecond)
		}
	}
	start := time.Now()
	waitCtx := context.Background()
	if ctx.Value(commandDeadlineKey{}) != nil {
		waitCtx = ctx
	}
	startRun := func() (*Session, string, bool) {
		if a.Background && !a.TTY && DetachBackgroundEnabled() {
			// Run-end services (W10): background sessions detach into
			// their own session so they survive the agent's exit.
			return t.sessions().StartDetached(owner, a.Command, a.Workdir, env)
		}
		return t.sessions().StartContext(waitCtx, owner, a.Command, a.Workdir, a.TTY, yield, env)
	}
	s, out, alive := startRun()
	dur := time.Since(start)
	// Provisioning runs only on exec output, bounded by this call's
	// timeout_s (autoprovision.go, ProvisionBudget). A verified install
	// re-runs the original command once; that result replaces the first
	// run, which is kept as one line.
	var prov ProvisionOutcome
	if p := t.provisioner(); p != nil {
		prov = p.Provision(ctx, a.Command, out, a.Workdir, ProvisionBudget(a.TimeoutS))
	}
	firstExit, firstExited := s.Exit()
	rerun := prov.Rerun && firstExited && !alive
	if rerun {
		rerunStart := time.Now()
		s, out, alive = startRun()
		dur = time.Since(rerunStart)
	}
	code, signal, exited := sessionExit(s)
	if !execErrorStatusEnabled() {
		// Previous reporting: os/exec's -1 for signal exits, no names.
		code, _ = s.Exit()
		signal = ""
	}

	var b strings.Builder
	status := StatusOK
	var exitCode *int
	// nonZero is true when the header exit is non-zero or unknown: only
	// then do the failure hints apply.
	nonZero := false
	switch {
	case exited && !alive:
		label := strconv.Itoa(code)
		if signal != "" {
			label += " (" + signal + ")"
		}
		fmt.Fprintf(&b, "exit: %s  duration: %.1fs  session: %s  alive: false\n", label, dur.Seconds(), s.ID)
		nonZero = code != 0
		if execErrorStatusEnabled() {
			c := code
			exitCode = &c
		}
	case alive:
		next := len(s.SnapshotFull())
		fmt.Fprintf(&b, "exit: running  duration: %.1fs  session: %s  alive: true  next_offset: %d\n", dur.Seconds(), s.ID, next)
		b.WriteString(runningSessionLine(s, next))
		if s.Detached {
			fmt.Fprintf(&b, "detached service: pid %d survives run end; log %s\n", s.Pid, s.LogPath)
		}
	default:
		fmt.Fprintf(&b, "exit: unknown  duration: %.1fs  session: %s  alive: false\n", dur.Seconds(), s.ID)
		nonZero = true
	}
	if nonZero && execErrorStatusEnabled() {
		status = StatusToolError
	}

	if exited && !alive && code == 0 && execWarningsEnabled() {
		b.WriteString(zeroExitWarning(out))
	}
	if ignored != "" {
		b.WriteString(ignored)
	}
	if exited && !alive && execErrorStatusEnabled() {
		b.WriteString(probeExitNote(command, code))
	}
	if exited && !alive {
		// Identical re-run: the same command in the same workdir with
		// byte-identical output and no file change since the last run.
		cur := execCall{seq: call, exit: code, sum: sha256.Sum256([]byte(out)), finished: time.Now()}
		prev, seen := t.ledger.swap(a.Workdir+"\x00"+command, cur)
		if seen && execWarningsEnabled() && prev.exit == cur.exit && prev.sum == cur.sum &&
			!workdirChangedSince(dirOrDot(a.Workdir), prev.finished) {
			fmt.Fprintf(&b, "note: identical to call #%d (no files changed since)\n", prev.seq)
		}
	}

	if prov.Timing != "" {
		b.WriteString(prov.Timing + "\n")
	}
	if rerun {
		b.WriteString(prov.FirstRunLine(firstExit) + "\n")
	} else {
		b.WriteString(prov.Report)
	}

	var body, blobRef string
	if execElideRefEnabled() {
		body, blobRef = clipExecOutput(stripPipNotices(out), t.Outputs, string(s.ID))
	} else {
		body = headTailInline(out, execInlineBudget)
	}
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	if nonZero || !execErrorStatusEnabled() {
		if hint := execFailureHint(command, a.Workdir, out); hint != "" {
			b.WriteString(hint)
		}
	}
	if exited && !alive && code == 0 && execWarningsEnabled() {
		b.WriteString(testOnlyScriptNote(command, a.Workdir, out))
	}
	res := Result{Status: status, Output: b.String(), ExitCode: exitCode}
	if t.Outputs != nil {
		res.BlobRef = blobRef
	}
	return res, nil
}

// runningSessionLine tells the model how to read a session that is
// still alive after the yield. exec_io is the poll handle when the
// surface exposes it; otherwise the line points at status.
func runningSessionLine(s *Session, next int) string {
	id := string(s.ID)
	if !execWarningsEnabled() {
		return fmt.Sprintf("to poll: exec_io {\"action\":\"read\",\"session\":%q,\"offset\":%d}\n", id, next)
	}
	if execIOVisible() {
		return fmt.Sprintf("to poll: exec_io {\"op\":\"read\",\"session\":%q}\n", id)
	}
	return fmt.Sprintf("still running in session %s after %.1fs. exec takes no session argument; check it with status.\n", id, time.Since(s.Started).Seconds())
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

// ExecIOTool implements exec_io session operations. Read paths never
// provision: a read reports what the session printed, nothing more.
type ExecIOTool struct {
	Provisioner *Provisioner
	Sessions    *SessionManager
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
	// Read paths only name missing dependencies; installs run from exec.
	report := func(cmd, out string) string {
		prov := (&ExecTool{Provisioner: t.Provisioner}).provisioner()
		if prov == nil {
			return ""
		}
		return prov.ReportOnly(cmd, out)
	}
	sessionReport := func(out string) string {
		session, ok := m.Get(SessionID(a.Session))
		if !ok {
			return ""
		}
		return report(session.Cmd, out)
	}
	switch a.Action {
	case "list":
		owner := a.Owner
		var note string
		// An owner no session carries is ignored instead of hiding
		// every session behind an empty list.
		if owner != "" && execWarningsEnabled() && len(m.List(owner)) == 0 {
			note = fmt.Sprintf("owner ignored: no session has owner %q; listing all sessions\n", owner)
			owner = ""
		}
		sums := m.Summaries(owner)
		var b strings.Builder
		fmt.Fprintf(&b, "%d session(s)\n", len(sums))
		b.WriteString(note)
		for _, s := range sums {
			fmt.Fprintf(&b, "- %s alive=%v owner=%s cmd=%q\n", s.ID, s.Alive, s.Owner, s.Cmd)
			b.WriteString(report(s.Cmd, s.Tail))
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
		return Result{Status: StatusOK, Output: fmt.Sprintf("exited: %v  elapsed: %.1fs  next_offset: %d\n%s%s", exited, sessionElapsed(m, a.Session), next, sessionReport(out), out)}, nil
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
		return Result{Status: StatusOK, Output: fmt.Sprintf("matched; exited: %v  next_offset: %d\n%s%s", exited, next, sessionReport(out), out)}, nil
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

// sessionElapsed returns the seconds since the session started (0 when
// the session is unknown).
func sessionElapsed(m *SessionManager, id string) float64 {
	if s, ok := m.Get(SessionID(id)); ok {
		return time.Since(s.Started).Seconds()
	}
	return 0
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

// signalNames names the Linux signals a shell reports as 128+n. The
// exec tool is Linux-only (Definition.Platforms).
var signalNames = map[int]string{
	1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT",
	7: "SIGBUS", 8: "SIGFPE", 9: "SIGKILL", 10: "SIGUSR1", 11: "SIGSEGV", 12: "SIGUSR2",
	13: "SIGPIPE", 14: "SIGALRM", 15: "SIGTERM", 24: "SIGXCPU", 25: "SIGXFSZ", 31: "SIGSYS",
}

// sessionExit returns the shell-style exit status of a finished session.
// A process killed by a signal reports 128+n and the signal name; the
// -1 that os/exec reports for such a process never reaches the model.
func sessionExit(s *Session) (code int, signal string, exited bool) {
	code, exited = s.Exit()
	if !exited {
		return 0, "", false
	}
	s.mu.Lock()
	err := s.exitErr
	s.mu.Unlock()
	if ee, ok := err.(*exec.ExitError); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			n := int(ws.Signal())
			return 128 + n, signalNames[n], true
		}
	}
	// A shell that reports 128+n itself (the usual case under sh -c).
	if code > 128 {
		return code, signalNames[code-128], true
	}
	return code, "", true
}

// execCall is the last finished run of one command in one workdir.
type execCall struct {
	seq      int
	exit     int
	sum      [32]byte
	finished time.Time
}

// execLedger numbers exec calls and remembers the last finished run of
// each command, so an identical re-run can be labelled. It is shared by
// every call through one ExecTool.
type execLedger struct {
	mu    sync.Mutex
	calls int
	last  map[string]execCall
}

// begin numbers a new call.
func (l *execLedger) begin() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	return l.calls
}

// swap stores cur as the latest run of key and returns the run it replaces.
func (l *execLedger) swap(key string, cur execCall) (execCall, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil || len(l.last) >= execLedgerCap {
		l.last = map[string]execCall{}
	}
	prev, ok := l.last[key]
	l.last[key] = cur
	return prev, ok
}

// dirOrDot maps the empty workdir (the process directory) to ".".
func dirOrDot(workdir string) string {
	if workdir == "" {
		return "."
	}
	return workdir
}

// workdirChangedSince reports whether any file or directory under dir
// was modified after t. The walk is bounded: past execScanCap entries
// or on an unreadable entry the answer is "changed".
func workdirChangedSince(dir string, t time.Time) bool {
	errStop := errors.New("stop")
	count := 0
	changed := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			changed = true
			return errStop
		}
		count++
		if count > execScanCap {
			changed = true
			return errStop
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			changed = true
			return errStop
		}
		if info.ModTime().After(t.Add(-execClockSlack)) {
			changed = true
			return errStop
		}
		return nil
	})
	return changed
}

// execStageSplit separates the stages of a shell command at unquoted
// ;, &&, ||, |, & and newlines. Redirections such as 2>&1 stay inside
// their stage. It is a heuristic for classification, not a parser.
func execStageSplit(cmd string) []string {
	var stages []string
	var cur strings.Builder
	var quote rune
	runes := []rune(cmd)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			stages = append(stages, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
			cur.WriteRune(r)
		case r == '\\' && i+1 < len(runes):
			cur.WriteRune(r)
			i++
			cur.WriteRune(runes[i])
		case r == '\'' || r == '"':
			quote = r
			cur.WriteRune(r)
		case r == '&' && i > 0 && (runes[i-1] == '>' || runes[i-1] == '<'):
			cur.WriteRune(r)
		case r == '&' && i+1 < len(runes) && runes[i+1] == '>':
			cur.WriteRune(r)
		case r == '\n' || r == ';' || r == '|' || r == '&':
			flush()
			if (r == '|' || r == '&') && i+1 < len(runes) && runes[i+1] == r {
				i++
			}
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return stages
}

// stageWords returns the words of one stage after leading environment
// assignments and the "!" prefix.
func stageWords(stage string) []string {
	words := strings.Fields(stage)
	for len(words) > 0 {
		w := words[0]
		if w == "!" || w == "(" || w == "{" {
			words = words[1:]
			continue
		}
		if i := strings.IndexByte(w, '='); i > 0 && isEnvName(w[:i]) {
			words = words[1:]
			continue
		}
		break
	}
	return words
}

func isEnvName(s string) bool {
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return s != ""
}

// probeCommandNames are commands whose non-zero exit is an answer
// (no match, not found, differs, no process) rather than a failure.
var probeCommandNames = map[string]bool{
	"test": true, "[": true, "[[": true, "grep": true, "egrep": true, "fgrep": true,
	"rg": true, "which": true, "type": true, "ls": true, "diff": true, "pgrep": true,
}

// benignStageNames print or set the exit status without doing work
// that can fail; a list made only of probes and these is a probe list.
var benignStageNames = map[string]bool{
	"echo": true, "printf": true, "true": true, "false": true, ":": true, "cd": true,
}

// stageName returns the base name of a stage's command and its words.
func stageName(stage string) (string, []string) {
	words := stageWords(stage)
	if len(words) == 0 {
		return "", nil
	}
	return path.Base(words[0]), words
}

// IsProbeCommand reports whether a command is a probe: a check whose
// non-zero exit is a normal answer ("no match", "not found", "files
// differ") rather than a failure. Every stage must be a probe or a
// benign output stage, and at least one must be a probe. The finish
// gate uses it to ignore probe failures.
func IsProbeCommand(cmd string) bool {
	probe := false
	for _, stage := range execStageSplit(cmd) {
		name, words := stageName(stage)
		switch {
		case name == "":
		case probeCommandNames[name]:
			probe = true
		case name == "command" && len(words) > 1 && (words[1] == "-v" || words[1] == "-V"):
			probe = true
		case benignStageNames[name]:
		default:
			return false
		}
	}
	return probe
}

// probeNoteText is the model-facing reading of a benign probe exit.
const probeNoteText = `here usually means "not found / no match", not a failure`

// IsProbeFailureOutput reports whether an exec result is a probe whose
// non-zero exit carries the probe note. The engine's "last command
// errored" finish refusal skips these results.
func IsProbeFailureOutput(out string) bool {
	return strings.Contains(out, probeNoteText)
}

// probeExitNote adds the neutral reading for a probe that exited 1 (or
// 2 for ls of a missing path). Other exits are real errors.
func probeExitNote(command string, code int) string {
	if !IsProbeCommand(command) {
		return ""
	}
	switch {
	case code == 1:
	case code == 2 && containsStageName(command, "ls"):
	default:
		return ""
	}
	return fmt.Sprintf("note: exit %d %s\n", code, probeNoteText)
}

func containsStageName(command, name string) bool {
	for _, stage := range execStageSplit(command) {
		if n, _ := stageName(stage); n == name {
			return true
		}
	}
	return false
}

// headTailInline keeps the first and last quarters of out within a
// small inline budget (the pre-elision-ref behaviour, EXCALIBUR_EXEC_ELIDE_REF=0).
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

// clipExecOutput keeps the head and the last execTailKeep bytes of full
// within execInlineBudget. The elided middle is stored (store.Put) and
// the marker names its ref and offset, so get_tool_output pages it.
// Without a store the marker names fallback (the session id). The
// returned ref is "" when nothing was elided.
func clipExecOutput(full string, store OutputStore, fallback string) (string, string) {
	if len(full) <= execInlineBudget {
		return full, ""
	}
	ref := fallback
	if store != nil {
		ref = store.Put(full)
	}
	headLen := execInlineBudget - execTailKeep - execMarkerReserve
	if headLen < 0 {
		headLen = 0
	}
	for headLen > 0 && !utf8.RuneStart(full[headLen]) {
		headLen--
	}
	tailStart := len(full) - execTailKeep
	for tailStart < len(full) && !utf8.RuneStart(full[tailStart]) {
		tailStart++
	}
	marker := fmt.Sprintf("\n…[middle %d bytes elided; full output: get_tool_output ref=%s offset=%d]…\n", tailStart-headLen, ref, headLen)
	return full[:headLen] + marker + full[tailStart:], ref
}

// stripPipNotices drops pip's upgrade notices. They fill the output
// budget and carry no task signal.
func stripPipNotices(out string) string {
	if !strings.Contains(out, "[notice]") {
		return out
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(out, "\n") {
		if strings.HasPrefix(line, "[notice]") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

var (
	// shellNoFileRE matches a shell builtin's own "No such file or
	// directory" (cd, redirections), not a program's message.
	shellNoFileRE = regexp.MustCompile(`(?m)^(?:[^\s:]*/)?(?:bash|sh|zsh|dash|ksh): .*No such file or directory`)
	// shellNotFoundRE matches dash's "sh: 1: foo: not found".
	shellNotFoundRE = regexp.MustCompile(`(?m)^(?:[^\s:]*/)?(?:bash|sh|zsh|dash|ksh): (?:line \d+: |\d+: )?[^:\n]+: not found$`)
	// heredocRE matches bash's unterminated here-document warning.
	heredocRE = regexp.MustCompile(`here-document at line \d+ delimited by end-of-file`)
	// pythonBinRE matches python, python3 and python3.12.
	pythonBinRE = regexp.MustCompile(`^python[0-9.]*$`)
	// testDefRE matches a test function definition.
	testDefRE = regexp.MustCompile(`(?m)^\s*def test_`)
)

// zeroExitWarning names failure text in a command that exited 0: the
// header then says that a step may have failed (for example a
// `|| true` guard around a missing tool). Empty when there is none.
func zeroExitWarning(out string) string {
	phrase := ""
	switch {
	case strings.Contains(out, "command not found") || shellNotFoundRE.MatchString(out):
		phrase = "command not found"
	case shellNoFileRE.MatchString(out):
		phrase = "No such file or directory"
	case strings.Contains(out, "Traceback (most recent call last)"):
		phrase = "Traceback (most recent call last)"
	case strings.Contains(out, "Segmentation fault"):
		phrase = "Segmentation fault"
	}
	if m := heredocRE.FindString(out); m != "" {
		phrase = m
	}
	if phrase == "" {
		return ""
	}
	return fmt.Sprintf("warning: exit 0 but the output contains %q\n", phrase)
}

// actionableExecHint is execFailureHint without a workdir: missing
// paths are resolved against the process directory.
func actionableExecHint(command, out string) string {
	return execFailureHint(command, "", out)
}

// execFailureHint turns a failed command into a next action: missing
// binaries name the package managers and the tools present, missing
// paths list the nearest existing parent. Empty when there is nothing
// actionable. Callers apply it only to a failed exit.
func execFailureHint(command, workdir, out string) string {
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
		if !execWarningsEnabled() {
			return hint + " Install it (one exec: install && verify with `command -v <bin>`), or rework the step to use a present tool.\n"
		}
		return hint + " Provision the tool through one of the package managers above, or use a present alternative.\n"
	}
	if strings.Contains(lower, "no such file or directory") {
		if !execWarningsEnabled() {
			return "hint: no such file or directory. Check the path with `ls` on its parent (use workdir for the task directory); create it with mkdir -p or write_file before retrying.\n"
		}
		return missingDirHint(out, workdir)
	}
	if strings.Contains(lower, "permission denied") {
		return "hint: permission denied. Check ownership with `ls -la`; prefer user-local paths over system directories.\n"
	}
	return ""
}

// missingDirHint lists the nearest existing parent of the path that
// failed (at most execListCap entries) and asks the model to check the path.
func missingDirHint(out, workdir string) string {
	base := dirOrDot(workdir)
	p := base
	if target := missingPathIn(out); target != "" {
		if filepath.IsAbs(target) {
			p = target
		} else {
			p = filepath.Join(base, target)
		}
	}
	parent := nearestExistingDir(filepath.Dir(p))
	if parent == "" {
		return "hint: no such file or directory. Check the path.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "hint: no such file or directory. Check the path; nearest existing parent: %s\n", parent)
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) == 0 {
		b.WriteString("  (empty)\n")
		return b.String()
	}
	for i, e := range entries {
		if i == execListCap {
			fmt.Fprintf(&b, "  ... %d more entries\n", len(entries)-execListCap)
			break
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		if info.IsDir() {
			name += "/"
		}
		fmt.Fprintf(&b, "  %s %8d %s\n", info.Mode().String(), info.Size(), name)
	}
	return b.String()
}

// missingPathIn extracts the path named by a "No such file or directory"
// line ("cd: /x/y: No such file", "ls: cannot access 'x': No such file").
func missingPathIn(out string) string {
	for _, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, ": No such file or directory")
		if idx < 0 {
			continue
		}
		before := strings.TrimSpace(line[:idx])
		if strings.HasSuffix(before, "'") {
			if j := strings.LastIndex(before[:len(before)-1], "'"); j >= 0 {
				return before[j+1 : len(before)-1]
			}
		}
		if j := strings.LastIndex(before, ": "); j >= 0 {
			before = before[j+2:]
		}
		if p := strings.Trim(before, "`'\" "); p != "" {
			return p
		}
	}
	return ""
}

// nearestExistingDir returns d, or the nearest ancestor that exists as a
// directory ("" when none does).
func nearestExistingDir(d string) string {
	for i := 0; i < 64; i++ {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
		next := filepath.Dir(d)
		if next == d {
			return ""
		}
		d = next
	}
	return ""
}

// testOnlyScriptNote names a python script that defines test_ functions
// and has no __main__ block: running it silently executes zero tests.
// It fires only for a silent exit 0.
func testOnlyScriptNote(command, workdir, out string) string {
	if strings.TrimSpace(out) != "" {
		return ""
	}
	for _, stage := range execStageSplit(command) {
		name, words := stageName(stage)
		if len(words) < 2 || !pythonBinRE.MatchString(name) {
			continue
		}
		script := words[1]
		if !strings.HasSuffix(script, ".py") {
			continue
		}
		p := script
		if !filepath.IsAbs(p) && workdir != "" {
			p = filepath.Join(workdir, script)
		}
		if definesOnlyTests(p) {
			return fmt.Sprintf("note: 0 tests ran: %s defines test_ functions and has no __main__ block, so python does not call them\n", script)
		}
	}
	return ""
}

// definesOnlyTests reports whether the script at p defines test_
// functions and has no __main__ block.
func definesOnlyTests(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > execScriptLimit {
		return false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	src := string(data)
	return testDefRE.MatchString(src) && !strings.Contains(src, "__main__")
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
