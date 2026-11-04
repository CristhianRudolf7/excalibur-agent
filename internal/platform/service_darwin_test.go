//go:build darwin

package platform

import (
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDarwinLaunchAgentGolden(t *testing.T) {
	body, err := renderLaunchAgent("com.excalibur.daemon", "/Applications/A & B/excalibur", "/data/logs", map[string]string{"PATH": "/bin:/usr/bin", "TMPDIR": "/private/tmp/user"})
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/launch_agent_darwin.plist")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(golden) {
		t.Fatalf("unexpected plist:\n%s", body)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	var doc any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	escaped, err := renderLaunchAgent("label<>", "/a\"b'c<&>", "/l&g", map[string]string{"a<": "b&"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(escaped), "<string>/a\"b'c<&>") || !strings.Contains(string(escaped), "&amp;") {
		t.Fatal("unescaped XML")
	}
	if err := xml.Unmarshal(escaped, &doc); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinServiceLifecycle(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(base, "runtime"))
	t.Setenv("TMPDIR", filepath.Join(base, "tmp"))
	var calls [][]string
	running := false
	m := DarwinServiceManager{UnitDir: filepath.Join(base, "agents"), ExecPath: "/Applications/Excalibur App/excalibur", Run: func(name string, args ...string) ([]byte, error) {
		if name != "/bin/launchctl" {
			t.Fatalf("unexpected command %q", name)
		}
		calls = append(calls, append([]string(nil), args...))
		switch args[0] {
		case "bootout":
			if !running {
				return []byte("Could not find service"), errors.New("not loaded")
			}
			running = false
		case "bootstrap":
			running = true
		case "print":
			if !running {
				return []byte("Could not find service"), errors.New("not loaded")
			}
			return []byte("gui/501/com.excalibur.daemon = {\n state = running\n pid = 123\n}\n"), nil
		}
		return nil, nil
	}}
	if status, err := m.Status(); err != nil || status != "absent" {
		t.Fatalf("before install: %s %v", status, err)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(m.UnitDir, defaultLaunchAgentLabel+".plist")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("plist mode: %v %v", fi, err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{os.TempDir(), filepath.Join(base, "runtime"), filepath.Join(base, "data", "excalibur", "logs")} {
		if !strings.Contains(string(body), value) {
			t.Fatalf("plist missing %s", value)
		}
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	want := [][]string{{"bootout", domain + "/" + defaultLaunchAgentLabel}, {"bootstrap", domain, path}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("commands = %v", calls)
	}
	if status, err := m.Status(); err != nil || status != "active (pid 123)" {
		t.Fatalf("running: %s %v", status, err)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if status, err := m.Status(); err != nil || status != "absent" {
		t.Fatalf("after uninstall: %s %v", status, err)
	}
}

func TestDarwinLaunchctlStatus(t *testing.T) {
	for _, tc := range []struct {
		raw, state string
		pid        int
	}{
		{"job = {\n state = running\n pid = 456\n child = {\n state = ignored\n pid = 999\n }\n}", "running", 456},
		{"job = {\n state = not running\n}", "not running", 0},
		{"job = {\n state = running\n environment = {\n PATH => /opt/{tools/bin\n }\n pid = 789\n}", "running", 789},
		{"job = {\n state = spawn scheduled\n}", "spawn scheduled", 0},
	} {
		state, pid, err := parseLaunchAgentStatus(tc.raw)
		if err != nil || state != tc.state || pid != tc.pid {
			t.Fatalf("state=%s pid=%d err=%v", state, pid, err)
		}
	}
	for _, raw := range []string{"", "job = {\n pid = 2\n}", "job = {\n state = running\n pid = no\n}", "job = {\n state = running\n pid = -1\n}"} {
		if _, _, err := parseLaunchAgentStatus(raw); err == nil {
			t.Fatal("malformed status accepted")
		}
	}
}

func TestDarwinServiceErrors(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	fail := errors.New("permission denied")
	m := DarwinServiceManager{UnitDir: filepath.Join(base, "agents"), ExecPath: "/bin/excalibur", Run: func(_ string, args ...string) ([]byte, error) { return nil, fail }}
	if err := m.Install(); !errors.Is(err, fail) {
		t.Fatalf("bootout error: %v", err)
	}
	if err := m.Uninstall(); !errors.Is(err, fail) {
		t.Fatalf("uninstall error: %v", err)
	}
	if _, err := m.Status(); !errors.Is(err, fail) {
		t.Fatalf("print error: %v", err)
	}
	m.Run = func(_ string, args ...string) ([]byte, error) {
		if args[0] == "bootout" {
			return nil, nil
		}
		return nil, fail
	}
	if err := m.Install(); !errors.Is(err, fail) {
		t.Fatalf("bootstrap error: %v", err)
	}
	m.Run = func(_ string, _ ...string) ([]byte, error) { return []byte("Could not find service"), fail }
	if status, err := m.Status(); err != nil || status != "installed" {
		t.Fatalf("unloaded: %s %v", status, err)
	}
	m.Run = func(_ string, _ ...string) ([]byte, error) { return []byte("job = {\n state = not running\n}"), nil }
	if status, err := m.Status(); err != nil || status != "inactive" {
		t.Fatalf("inactive: %s %v", status, err)
	}
	m.Label = "../outside"
	if err := m.Install(); err == nil {
		t.Fatal("unsafe label accepted")
	}
}

func TestDarwinServiceDefaultPathAndLabel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, label, err := (DarwinServiceManager{}).plistPath()
	if err != nil || label != "com.excalibur.daemon" || path != filepath.Join(home, "Library", "LaunchAgents", "com.excalibur.daemon.plist") {
		t.Fatalf("default path %q label %q: %v", path, label, err)
	}
	path, label, err = (DarwinServiceManager{Label: "com.excalibur.daemon.work"}).plistPath()
	if err != nil || label != "com.excalibur.daemon.work" || filepath.Base(path) != "com.excalibur.daemon.work.plist" {
		t.Fatalf("custom label %q: %v", label, err)
	}
}

func TestDarwinLaunchAgentAtomicRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents", "com.excalibur.daemon.plist")
	if err := writeLaunchAgent(path, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := writeLaunchAgent(path, []byte("second")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "second" {
		t.Fatal("plist replacement failed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary plist was left behind")
	}
}
