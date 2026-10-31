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
		{"exec", "--profile", "eval"},
		{"task", "start", "do it"},
		{"task", "status", "T1"},
		{"daemon", "start"},
		{"vault", "lock"},
		{"ext", "doctor"},
		{"mcp"},
		{"status"},
	}
	for _, args := range cases {
		if code := run(args); code != ExitNotImplemented {
			t.Fatalf("run(%v) exit = %d, want %d", args, code, ExitNotImplemented)
		}
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
