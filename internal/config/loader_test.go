package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTOML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaultsWhenNoLayers(t *testing.T) {
	cfg, err := Load(LoadOptions{SkipFiles: true, NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatal("empty layers must equal defaults")
	}
}

func TestLayerPrecedence(t *testing.T) {
	root := t.TempDir()
	system := filepath.Join(root, "system.toml")
	user := filepath.Join(root, "user.toml")
	proj := filepath.Join(root, "proj", ".excalibur", "config.toml")
	scope := filepath.Join(root, "scope.toml")
	writeTOML(t, system, "[porter]\ngroup_commit_max_ops = 10\n")
	writeTOML(t, user, "[porter]\ngroup_commit_max_ops = 20\nread_pool_size = 2\n")
	writeTOML(t, proj, "[porter]\ngroup_commit_max_ops = 30\n")
	writeTOML(t, scope, "[porter]\ngroup_commit_max_ops = 40\n")

	cfg, err := Load(LoadOptions{
		SystemPath: system, UserPath: user, ProjectFile: proj,
		ScopeFile: scope, NoEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Porter.GroupCommitMaxOps != 40 {
		t.Fatalf("scope wins: got %d", cfg.Porter.GroupCommitMaxOps)
	}
	if cfg.Porter.ReadPoolSize != 2 {
		t.Fatalf("user value survives: got %d", cfg.Porter.ReadPoolSize)
	}
	if cfg.Porter.BusyTimeout != 5*time.Second {
		t.Fatalf("untouched keys keep defaults: got %v", cfg.Porter.BusyTimeout)
	}
}

func TestEnvAndOverridesBeatFiles(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user.toml")
	writeTOML(t, user, "[porter]\ngroup_commit_max_ops = 20\n[engine]\nchild_max_steps = 60\n")
	cfg, err := Load(LoadOptions{
		UserPath: user, NoEnv: false,
		Env: []string{
			"EXCALIBUR_PORTER__GROUP_COMMIT_MAX_OPS=77",
			"EXCALIBUR_ENGINE__CHILD_MAX_STEPS=61",
		},
		Overrides: map[string]any{"engine.child_max_steps": 62},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Porter.GroupCommitMaxOps != 77 {
		t.Fatalf("env beats file: got %d", cfg.Porter.GroupCommitMaxOps)
	}
	if cfg.Engine.ChildMaxSteps != 62 {
		t.Fatalf("flags beat env: got %d", cfg.Engine.ChildMaxSteps)
	}
}

func TestDurationsParseFromTOMLAndEnv(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user.toml")
	writeTOML(t, user, "[porter]\nbusy_timeout = \"30s\"\n[groupX]\n")
	_ = os.Remove(user)
	writeTOML(t, user, "[porter]\nbusy_timeout = \"30s\"\n")
	cfg, err := Load(LoadOptions{
		UserPath: user, NoEnv: false,
		Env: []string{"EXCALIBUR_PORTER__GROUP_COMMIT_WAIT=250ms"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Porter.BusyTimeout != 30*time.Second {
		t.Fatalf("toml duration: got %v", cfg.Porter.BusyTimeout)
	}
	if cfg.Porter.GroupCommitWait != 250*time.Millisecond {
		t.Fatalf("env duration: got %v", cfg.Porter.GroupCommitWait)
	}
}

func TestFindProjectFile(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTOML(t, filepath.Join(root, "a", ".excalibur", "config.toml"), "[master]\nmax_steps = 7\n")
	found, err := FindProjectFile(deep)
	if err != nil {
		t.Fatal(err)
	}
	if found != filepath.Join(root, "a", ".excalibur", "config.toml") {
		t.Fatalf("discovery: got %q", found)
	}
	cfg, err := Load(LoadOptions{StartDir: deep, NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Master.MaxSteps != 7 {
		t.Fatalf("project layer applied: got %d", cfg.Master.MaxSteps)
	}
}

func TestInvalidLayerRejected(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bad.toml")
	writeTOML(t, bad, "[porter]\ngroup_commit_max_ops = -5\n")
	if _, err := Load(LoadOptions{UserPath: bad, NoEnv: true}); err == nil {
		t.Fatal("expected validation error")
	}
	broken := filepath.Join(root, "broken.toml")
	writeTOML(t, broken, "[porter\nunclosed")
	if _, err := Load(LoadOptions{UserPath: broken, NoEnv: true}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestMissingFilesAreSkipped(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SystemPath: "/nonexistent/system.toml",
		UserPath:   "/nonexistent/user.toml",
		StartDir:   t.TempDir(),
		NoEnv:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatal("missing layers must equal defaults")
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := Default()
	want.Porter.GroupCommitMaxOps = 33
	if err := WriteFile(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(LoadOptions{UserPath: path, NoEnv: true, StartDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Porter.GroupCommitMaxOps != 33 {
		t.Fatalf("round trip: got %d", got.Porter.GroupCommitMaxOps)
	}
	if got != want {
		t.Fatal("round trip must preserve every field")
	}
}
