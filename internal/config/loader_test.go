package config

import (
	"os"
	"path/filepath"
	"reflect"
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
	if !reflect.DeepEqual(cfg, Default()) {
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

// TestTopLevelAliasesSkipped covers the Section 19.2 alias rule:
// single-segment EXCALIBUR_* names (EXCALIBUR_PROVIDER,
// EXCALIBUR_MODEL, ...) are not config keys and must not collide
// with the same-named section tables; the model wiring resolves
// them explicitly.
func TestTopLevelAliasesSkipped(t *testing.T) {
	cfg, err := Load(LoadOptions{
		SkipFiles: true,
		Env: []string{
			"EXCALIBUR_PROVIDER=stub",
			"EXCALIBUR_MODEL=gizmo-1",
			"EXCALIBUR_API_KEY=secret",
			"EXCALIBUR_MODELS__MODEL=file-model",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Model != "file-model" {
		t.Fatalf("nested env applies: %q", cfg.Models.Model)
	}
}

// TestModelsSectionLayers covers the Section 19.2 wiring source:
// TOML file values merge, EXCALIBUR_MODELS__ env beats files, and
// secrets stay opaque strings (never logged or validated here).
func TestModelsSectionLayers(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user.toml")
	writeTOML(t, user, "[models]\nprovider = \"openai\"\nmodel = \"gpt-6-luna\"\napi_key_secret = \"secret://provider-key\"\n")
	cfg, err := Load(LoadOptions{
		UserPath: user, NoEnv: false,
		Env: []string{"EXCALIBUR_MODELS__MODEL=gizmo-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Provider != "openai" {
		t.Fatalf("file provider: got %q", cfg.Models.Provider)
	}
	if cfg.Models.Model != "gizmo-1" {
		t.Fatalf("env beats file: got %q", cfg.Models.Model)
	}
	if cfg.Models.APIKeySecret != "secret://provider-key" {
		t.Fatalf("secret ref: got %q", cfg.Models.APIKeySecret)
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
	if !reflect.DeepEqual(cfg, Default()) {
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
	if !reflect.DeepEqual(got, want) {
		t.Fatal("round trip must preserve every field")
	}
}
