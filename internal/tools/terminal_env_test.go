package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// unsetEnvForTest removes the variables for the duration of the test.
func unsetEnvForTest(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		prev, had := os.LookupEnv(k)
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
		k := k
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(k, prev)
			} else {
				_ = os.Unsetenv(k)
			}
		})
	}
}

// isolateGitConfig stops git from reading the operator's configuration.
func isolateGitConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	unsetEnvForTest(t, "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL")
}

func envMap(env []string) map[string]string {
	out := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}

func TestAutonomousDefaultsOnlyInExecScope(t *testing.T) {
	isolateGitConfig(t)
	found := envMap(pipeEnvIn("", nil))
	if _, ok := found["PIP_BREAK_SYSTEM_PACKAGES"]; ok {
		t.Fatal("pip defaults applied outside an exec scope")
	}
	restore := SetExecScope(ExecScope{Active: true})
	defer restore()
	found = envMap(pipeEnvIn("", nil))
	want := map[string]string{
		"PIP_DISABLE_PIP_VERSION_CHECK": "1",
		"PIP_BREAK_SYSTEM_PACKAGES":     "1",
		"PIP_ROOT_USER_ACTION":          "ignore",
		"DEBIAN_FRONTEND":               "noninteractive",
		"PAGER":                         "cat",
		"GIT_PAGER":                     "cat",
	}
	for k, v := range want {
		if found[k] != v {
			t.Errorf("%s = %q, want %q", k, found[k], v)
		}
	}
}

func TestAutonomousGitIdentityDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitConfig(t)
	restore := SetExecScope(ExecScope{Active: true})
	defer restore()

	found := envMap(pipeEnvIn(t.TempDir(), nil))
	if found["GIT_AUTHOR_NAME"] != agentGitName || found["GIT_COMMITTER_EMAIL"] != agentGitEmail {
		t.Fatalf("default identity missing: %v", found)
	}

	// An identity already in the environment wins over the default.
	t.Setenv("GIT_AUTHOR_NAME", "Operator")
	found = envMap(pipeEnvIn("", nil))
	if found["GIT_AUTHOR_NAME"] != "Operator" || found["GIT_COMMITTER_NAME"] != agentGitName {
		t.Fatalf("environment identity overridden: %v", found)
	}
}

func TestAutonomousGitIdentityFromRepositoryConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitConfig(t)
	restore := SetExecScope(ExecScope{Active: true})
	defer restore()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.name", "Repository Owner"},
		{"config", "user.email", "owner@example.invalid"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	found := envMap(pipeEnvIn(repo, nil))
	for _, key := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if _, ok := found[key]; ok {
			t.Errorf("%s set although the repository configures an identity", key)
		}
	}
}

func TestAutonomousEnvSwitchOff(t *testing.T) {
	isolateGitConfig(t)
	t.Setenv(AutonomousEnvSwitch, "0")
	restore := SetExecScope(ExecScope{Active: true})
	defer restore()
	found := envMap(pipeEnvIn("", nil))
	if _, ok := found["PIP_BREAK_SYSTEM_PACKAGES"]; ok {
		t.Fatal("pip defaults applied with the switch off")
	}
	if _, ok := found["GIT_AUTHOR_NAME"]; ok {
		t.Fatal("git defaults applied with the switch off")
	}
}
