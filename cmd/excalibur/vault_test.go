package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// runVault runs `excalibur vault ...` with stdin and stdout redirected
// to regular files, so the value path (--stdin) and the output checks
// are exact without pipes.
func runVault(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	inPath := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(inPath, []byte(stdin), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	outPath := filepath.Join(t.TempDir(), "stdout")
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = in, out
	code := run(append([]string{"vault"}, args...))
	os.Stdin, os.Stdout = oldIn, oldOut
	_ = out.Close()
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data), code
}

// writeTestPassphrase creates an owner-only unattended unlock fixture.
func writeTestPassphrase(t *testing.T, name, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := platform.RestrictToOwner(path); err != nil {
		t.Fatal(err)
	}
	return path
}

// isolateVaultEnv keeps the runtime rendezvous and the data directory
// out of the developer's real state, so no running daemon is detected.
func isolateVaultEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// TestVaultCLIRoundTrip drives set, list, show, get, edit, generate,
// usage and rm through the CLI on a temp data directory. Only `get`
// may print a value, and listings never do.
func TestVaultCLIRoundTrip(t *testing.T) {
	isolateVaultEnv(t)
	dir := t.TempDir()
	pf := writeTestPassphrase(t, "passphrase", "cli round trip passphrase")
	const value = "ghp_cli_round_trip_value_42"
	base := []string{"--data-dir", dir, "--passphrase-file", pf}
	with := func(args ...string) []string { return append(args, base...) }

	out, code := runVault(t, value+"\n", with("set", "github-token", "--type", "token", "--stdin")...)
	if code != 0 {
		t.Fatalf("set: exit %d, output %q", code, out)
	}
	if strings.Contains(out, value) {
		t.Fatal("set printed the secret value")
	}

	out, code = runVault(t, "", with("list")...)
	if code != 0 || !strings.Contains(out, "github-token") || strings.Contains(out, value) {
		t.Fatalf("list: exit %d, output %q", code, out)
	}
	out, code = runVault(t, "", with("show", "github-token")...)
	if code != 0 || !strings.Contains(out, "github-token") || strings.Contains(out, value) {
		t.Fatalf("show: exit %d, output %q", code, out)
	}

	out, code = runVault(t, "", with("get", "github-token")...)
	if code != 0 || out != value+"\n" {
		t.Fatalf("get: exit %d, output %q; want the value on request", code, out)
	}

	if _, code = runVault(t, "", with("edit", "github-token", "--scopes", "general,proj-a")...); code != 0 {
		t.Fatalf("edit: exit %d", code)
	}
	out, _ = runVault(t, "", with("show", "github-token")...)
	if !strings.Contains(out, "proj-a") {
		t.Fatalf("edit did not change scopes: %q", out)
	}

	if _, code = runVault(t, "", with("generate", "gen-pw", "--length", "24")...); code != 0 {
		t.Fatalf("generate: exit %d", code)
	}
	out, _ = runVault(t, "", with("get", "gen-pw")...)
	if got := strings.TrimSpace(out); len(got) != 24 {
		t.Fatalf("generated value length = %d, want 24", len(got))
	}

	if _, code = runVault(t, "", with("usage", "github-token")...); code != 0 {
		t.Fatalf("usage: exit %d", code)
	}

	if _, code = runVault(t, "", with("rm", "github-token")...); code == 0 {
		t.Fatal("rm without --yes must refuse")
	}
	out, _ = runVault(t, "", with("list")...)
	if !strings.Contains(out, "github-token") {
		t.Fatal("rm without --yes deleted the entry")
	}
	if _, code = runVault(t, "", with("rm", "github-token", "--yes")...); code != 0 {
		t.Fatalf("rm --yes: exit %d", code)
	}
	out, _ = runVault(t, "", with("list")...)
	if strings.Contains(out, "github-token") {
		t.Fatal("rm --yes did not delete the entry")
	}
}

// TestVaultCLIExportImport moves entries to a fresh installation through
// an encrypted backup. The backup file must not hold the plain value.
func TestVaultCLIExportImport(t *testing.T) {
	isolateVaultEnv(t)
	const value = "sk-export-value-88"
	src := t.TempDir()
	srcPF := writeTestPassphrase(t, "src", "source passphrase")
	if _, code := runVault(t, value+"\n", "set", "api-key", "--type", "api_key", "--stdin",
		"--data-dir", src, "--passphrase-file", srcPF); code != 0 {
		t.Fatal("set on source failed")
	}
	backupPF := writeTestPassphrase(t, "backup", "backup passphrase")
	backup := filepath.Join(t.TempDir(), "vault-backup.enc")
	if _, code := runVault(t, "", "export", backup, "--data-dir", src,
		"--passphrase-file", srcPF, "--backup-passphrase-file", backupPF); code != 0 {
		t.Fatal("export failed")
	}
	raw, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), value) {
		t.Fatal("encrypted backup contains the plain value")
	}

	dst := t.TempDir()
	dstPF := writeTestPassphrase(t, "dst", "new installation passphrase")
	if _, code := runVault(t, "", "import", backup, "--data-dir", dst,
		"--passphrase-file", dstPF, "--backup-passphrase-file", backupPF); code != 0 {
		t.Fatal("import failed")
	}
	out, code := runVault(t, "", "get", "api-key", "--data-dir", dst, "--passphrase-file", dstPF)
	if code != 0 || out != value+"\n" {
		t.Fatalf("imported value = %q (exit %d), want the source value", out, code)
	}
}

// TestVaultCLILockWithoutDaemon checks the daemon-less lock and unlock
// paths: nothing stays unlocked between commands, and the unlock
// reports that it lasts for this command only.
func TestVaultCLILockWithoutDaemon(t *testing.T) {
	isolateVaultEnv(t)
	dir := t.TempDir()
	pf := writeTestPassphrase(t, "passphrase", "lock test passphrase")
	out, code := runVault(t, "", "lock")
	if code != 0 || !strings.Contains(out, "no daemon running") {
		t.Fatalf("lock without daemon: exit %d, output %q", code, out)
	}
	out, code = runVault(t, "", "unlock", "--data-dir", dir, "--passphrase-file", pf)
	if code != 0 || !strings.Contains(out, "for this command") {
		t.Fatalf("unlock without daemon: exit %d, output %q", code, out)
	}
}
