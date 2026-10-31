// CLI install/update tests: 25.5 subcommand rows against temp
// directories, never touching the real home or systemd.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func cliEnv(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "run"))
	t.Setenv("EXCALIBUR_SYSTEMD_DIR", filepath.Join(root, "systemd"))
}

func TestCLIInstallClean(t *testing.T) {
	cliEnv(t)
	if err := runInstall([]string{"--timezone", "America/Lima", "--provider", "openai", "--autostart"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(os.Getenv("XDG_DATA_HOME"), "excalibur"),
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "excalibur"),
		filepath.Join(os.Getenv("XDG_CACHE_HOME"), "excalibur"),
		os.Getenv("XDG_RUNTIME_DIR"),
	} {
		if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
			t.Errorf("missing dir %q: %v", dir, serr)
		}
	}
	unit := filepath.Join(os.Getenv("EXCALIBUR_SYSTEMD_DIR"), "excalibur.service")
	raw, err := os.ReadFile(unit)
	if err != nil {
		t.Fatalf("systemd unit missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{"Restart=on-failure", "WatchdogSec=", "WantedBy=default.target"} {
		if !containsCLI(body, want) {
			t.Errorf("unit lacks %q:\n%s", want, body)
		}
	}
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "excalibur", "config.toml")
	if _, err := os.Stat(cfg); err != nil {
		t.Errorf("layered config missing: %v", err)
	}
}

func containsCLI(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func writeManifest(t *testing.T, version string, priv ed25519.PrivateKey, payload []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sum := sha256.Sum256(payload)
	m := daemon.Manifest{Channel: daemon.ChannelStable, Version: version, SHA256: hex.EncodeToString(sum[:]), Notes: "cli test"}
	daemon.SignManifest(&m, priv)
	mraw, _ := json.Marshal(m)
	mp := filepath.Join(dir, "manifest.json")
	pp := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(mp, mraw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pp, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return mp, pp
}

func TestCLIUpdateCheckAndReject(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("fake-binary")
	mp, pp := writeManifest(t, "2.0", priv, payload)
	pubHex := hex.EncodeToString(pub)
	if err := runUpdate([]string{"--manifest", mp, "--payload", pp, "--pubkey", pubHex, "--check"}); err != nil {
		t.Fatalf("check: %v", err)
	}
	// Tampered payload must reject before anything changes.
	bad := pp + ".bad"
	if err := os.WriteFile(bad, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runUpdate([]string{"--manifest", mp, "--payload", bad, "--pubkey", pubHex, "--check"}); err == nil {
		t.Fatal("tampered payload must reject")
	}
	// Unknown channel must reject.
	if err := runUpdate([]string{"--manifest", mp, "--payload", pp, "--pubkey", pubHex, "--channel", "beta", "--check"}); err == nil {
		t.Fatal("channel mismatch must reject")
	}
}

func TestCLIUpdateNeedsFiles(t *testing.T) {
	if err := runUpdate(nil); err == nil {
		t.Fatal("update without manifest/payload must fail")
	}
}
