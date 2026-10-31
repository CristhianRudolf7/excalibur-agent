package main

import "testing"

func TestRunHelp(t *testing.T) {
	if code := run(nil); code != ExitOK {
		t.Fatalf("help exit = %d", code)
	}
	if code := run([]string{"--help"}); code != ExitOK {
		t.Fatalf("help exit = %d", code)
	}
}

func TestRunUnknown(t *testing.T) {
	if code := run([]string{"frobnicate"}); code != ExitError {
		t.Fatalf("unknown exit = %d", code)
	}
	if code := run([]string{"task", "frobnicate"}); code != ExitError {
		t.Fatalf("unknown subcommand exit = %d", code)
	}
}

func TestRunNotImplemented(t *testing.T) {
	cases := [][]string{
		{"daemon", "start"},
		{"vault", "lock"},
		{"status"},
	}
	for _, args := range cases {
		if code := run(args); code != ExitNotImplemented {
			t.Fatalf("run(%v) exit = %d, want %d", args, code, ExitNotImplemented)
		}
	}
}

func TestRunExtImplemented(t *testing.T) {
	// The ext family landed in H13 (Section 14.12): doctor on an empty
	// root succeeds, test on a missing dir fails as a plain error.
	t.Setenv("EXCALIBUR_EXTENSIONS_DIR", t.TempDir())
	if code := run([]string{"ext", "doctor"}); code != ExitOK {
		t.Fatalf("run(ext doctor) exit = %d, want %d", code, ExitOK)
	}
	if code := run([]string{"ext", "test", "/nonexistent-dir"}); code != ExitError {
		t.Fatalf("run(ext test missing) exit = %d, want %d", code, ExitError)
	}
	if code := run([]string{"ext", "frobnicate"}); code != ExitError {
		t.Fatalf("run(ext frobnicate) exit = %d, want %d", code, ExitError)
	}
}

func TestImplementedCommandsFailClean(t *testing.T) {
	// Implemented commands reject bad usage with ExitError (1),
	// never ExitNotImplemented (69): exec needs a request, task
	// status needs a known id, mcp install needs a client.
	cases := [][]string{
		{"exec"},
		{"exec", "--profile", "nope", "do it"},
		{"task", "status", "T99999"},
		{"task", "reply", "T99999", "hi"},
		{"mcp", "install"},
		{"mcp", "install", "--client", "nope"},
		{"eval", "report"},
	}
	for _, args := range cases {
		if code := run(args); code == ExitNotImplemented {
			t.Fatalf("run(%v) still reports not-implemented", args)
		} else if code != ExitError {
			t.Fatalf("run(%v) exit = %d, want %d", args, code, ExitError)
		}
	}
}

func TestExecExitCodes(t *testing.T) {
	// Empty request: blocked with the Section 6.5 code.
	if code := run([]string{"exec", "--profile", "eval", "--logs-dir", t.TempDir()}); code != ExitError {
		t.Fatalf("exec without request exit = %d, want %d", code, ExitError)
	}
}

func TestExitCodesStable(t *testing.T) {
	codes := map[string]int{
		"ok": ExitOK, "error": ExitError, "pending": ExitPending,
		"blocked": ExitBlocked, "paused": ExitPaused, "failed": ExitFailed,
		"canceled": ExitCanceled,
	}
	seen := map[int]string{}
	for name, code := range codes {
		if prev, ok := seen[code]; ok {
			t.Fatalf("exit codes %s and %s collide at %d", name, prev, code)
		}
		seen[code] = name
	}
	if ExitNotImplemented == ExitError {
		t.Fatal("not-implemented must differ from generic error")
	}
}

func TestMixedFlagOrder(t *testing.T) {
	dir := t.TempDir()
	// Flags after the positional id must parse (skill examples use
	// this order); unknown task fails as a domain error, not usage.
	if code := run([]string{"task", "status", "T999", "--project", dir}); code != ExitError {
		t.Fatalf("mixed-order status exit = %d, want %d", code, ExitError)
	}
	if code := run([]string{"task", "list", "--project", dir}); code != ExitOK {
		t.Fatalf("mixed-order list exit = %d", code)
	}
	// A petition starting with a dash works behind --.
	logs := t.TempDir()
	if code := run([]string{"exec", "--project", dir, "--logs-dir", logs, "--", "--weird"}); code != ExitOK {
		t.Fatalf("dash petition exit = %d, want %d", code, ExitError)
	}
}
