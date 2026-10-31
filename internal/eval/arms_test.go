package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSwitches(t *testing.T) {
	m, err := ParseSwitches([]string{"EXCALIBUR_SELF_VERIFY=off", "dotted.key=v", "n=1"})
	if err != nil {
		t.Fatal(err)
	}
	if m["EXCALIBUR_SELF_VERIFY"] != "off" || m["dotted.key"] != "v" || m["n"] != "1" {
		t.Fatalf("switches = %v", m)
	}
	for _, bad := range [][]string{
		{"noequals"},
		{"=v"},
		{"bad key=v"},
		{"k="},
		{"k=a\nb"},
		{"k=v", "k=w"},
	} {
		if _, err := ParseSwitches(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestValidateArm(t *testing.T) {
	if err := ValidateArm(""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateArm("self-verify-off"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"has space", "semi;colon", strings.Repeat("a", 65), "sla/sh"} {
		if err := ValidateArm(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestArmProfileHash(t *testing.T) {
	base := DefaultEvalProfile().Hash()
	// Default arm reproduces the frozen hash exactly: pre-arm runs
	// stay comparable.
	if got := ArmProfileHash(base, "", nil); got != base {
		t.Fatalf("default arm hash moved: %s vs %s", got, base)
	}
	if got := ArmProfileHash(base, "", map[string]string{}); got != base {
		t.Fatalf("empty switches hash moved: %s", got)
	}
	a := ArmProfileHash(base, "no-verify", map[string]string{"EXCALIBUR_SELF_VERIFY": "off"})
	b := ArmProfileHash(base, "no-verify", map[string]string{"EXCALIBUR_SELF_VERIFY": "off"})
	if a == base || a != b {
		t.Fatalf("arm hash not stable/distinct: %s", a)
	}
	c := ArmProfileHash(base, "no-verify", map[string]string{"EXCALIBUR_SELF_VERIFY": "on"})
	if c == a {
		t.Fatal("switch value must move the arm hash")
	}
	// Key order is irrelevant.
	d := ArmProfileHash(base, "mix", map[string]string{"a": "1", "b": "2"})
	e := ArmProfileHash(base, "mix", map[string]string{"b": "2", "a": "1"})
	if d != e {
		t.Fatal("arm hash depends on switch order")
	}
}

func TestRedactSwitchValue(t *testing.T) {
	if got := RedactSwitchValue("EXCALIBUR_API_KEY", "sk-secret-value"); got == "sk-secret-value" {
		t.Fatal("key not redacted")
	}
	if got := RedactSwitchValue("EXCALIBUR_SELF_VERIFY", "off"); got != "off" {
		t.Fatalf("mechanism switch must stay verbatim: %q", got)
	}
}

func TestManifestArmRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := RunManifest{
		Dataset: "quick/local", Arm: "no-verify",
		Switches: map[string]string{"EXCALIBUR_SELF_VERIFY": "off"},
	}
	m.ProfileHash = DefaultEvalProfile().Hash()
	m.ArmProfileHash = ArmProfileHash(m.ProfileHash, m.Arm, m.Switches)
	if err := WriteManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "no-verify") || !strings.Contains(string(raw), "arm_profile_hash") {
		t.Fatalf("manifest missing arm fields: %s", raw)
	}
	got := LoadRunManifest(dir)
	if got.Arm != "no-verify" || got.Switches["EXCALIBUR_SELF_VERIFY"] != "off" {
		t.Fatalf("manifest round trip lost arm: %+v", got)
	}
	if got.ArmProfileHash != m.ArmProfileHash {
		t.Fatal("arm profile hash lost")
	}
}
