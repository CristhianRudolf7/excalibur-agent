package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

func TestChatWorkspaceUniqueContainedAndGitOverridesIgnored(t *testing.T) {
	data, outside := t.TempDir(), t.TempDir()
	t.Setenv("GIT_DIR", filepath.Join(outside, "redirected.git"))
	t.Setenv("GIT_WORK_TREE", outside)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "init.templateDir")
	t.Setenv("GIT_CONFIG_VALUE_0", outside)
	s := &Services{workspaceDataDir: data, Projects: workspace.NewRegistry()}
	seen := map[string]bool{}
	for _, name := range []string{"../../outside", "../../outside", "", "🚀", strings.Repeat("Long", 100)} {
		p, err := s.createChatWorkspace(context.Background(), name, "Implement the approved brief")
		if err != nil {
			t.Fatal(err)
		}
		root := p.Roots[0]
		// Managed workspace paths resolve symlinks, including macOS /var.
		expectedParent, err := filepath.EvalSymlinks(filepath.Join(data, "workspaces"))
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Dir(root) != expectedParent || seen[root] {
			t.Fatalf("nonunique or uncontained workspace: %s", root)
		}
		seen[root] = true
		if len(filepath.Base(root)) > 90 {
			t.Fatalf("unbounded slug: %s", root)
		}
		if _, err := os.Stat(filepath.Join(root, ".git", "HEAD")); err != nil {
			t.Fatal(err)
		}
		registered, err := s.Projects.Get(p.ID)
		if err != nil || registered.Roots[0] != root {
			t.Fatalf("project not registered: %+v %v", registered, err)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Git overrides escaped managed directory: %v %v", entries, err)
	}
}

func TestChatWorkspaceFailuresDoNotRegisterProjects(t *testing.T) {
	for _, mode := range []string{"missing-data", "symlink", "file", "canceled", "git-missing"} {
		t.Run(mode, func(t *testing.T) {
			data, outside := t.TempDir(), t.TempDir()
			s := &Services{workspaceDataDir: data, Projects: workspace.NewRegistry()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "missing-data":
				s.workspaceDataDir = ""
			case "symlink":
				if err := os.Symlink(outside, filepath.Join(data, "workspaces")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "file":
				if err := os.WriteFile(filepath.Join(data, "workspaces"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				cancel()
			case "git-missing":
				t.Setenv("PATH", t.TempDir())
			}
			if _, err := s.createChatWorkspace(ctx, "New app", "Build the app"); err == nil {
				t.Fatal("workspace provisioning unexpectedly succeeded")
			}
			if len(s.Projects.List()) != 0 {
				t.Fatal("failed provisioning registered a project")
			}
			if mode == "git-missing" {
				entries, err := os.ReadDir(filepath.Join(data, "workspaces"))
				if err != nil || len(entries) != 0 {
					t.Fatalf("partial workspace retained: %v %v", entries, err)
				}
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("write escaped managed root: %v %v", entries, err)
			}
		})
	}
}

func TestChatWorkspacePreparationPreservesScopesAndControls(t *testing.T) {
	s := &Services{workspaceDataDir: t.TempDir(), Projects: workspace.NewRegistry()}
	prepare := s.chatWorkspacePreparer()
	for _, args := range []string{
		`{"action":"status","run":"run_example"}`,
		`{"action":"spawn","card":{"scope":"project:prj_example"}}`,
		`{"action":"spawn","card":{"scope":"task:tsk_example"}}`,
	} {
		out, err := prepare(context.Background(), json.RawMessage(args), "")
		if err != nil || string(out) != args {
			t.Fatalf("explicit scope or control changed: %s %v", out, err)
		}
	}
	for _, args := range []string{`{"action":"spawn","card":{"goal":""}}`, `{"action":"spawn","card":{"scope":123}}`} {
		if _, err := prepare(context.Background(), json.RawMessage(args), ""); err == nil {
			t.Fatal("malformed workspace request accepted")
		}
	}
	if len(s.Projects.List()) != 0 {
		t.Fatal("scope/control/malformed requests provisioned workspaces")
	}
	args := json.RawMessage(`{"action":"spawn","caller":"agt_master","card":{"name":"New app","goal":"Build an app","inputs":["document"],"done_criteria":["tests pass"]}}`)
	first, err := prepare(context.Background(), args, "retry-key")
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepare(context.Background(), args, "retry-key")
	if err != nil || string(first) != string(second) || len(s.Projects.List()) != 1 {
		t.Fatalf("keyed retry created another workspace: %s %v", second, err)
	}
	var out struct {
		Caller string `json:"caller"`
		Card   struct {
			Scope  string   `json:"scope"`
			Inputs []string `json:"inputs"`
			Done   []string `json:"done_criteria"`
		} `json:"card"`
	}
	if err := json.Unmarshal(first, &out); err != nil {
		t.Fatal(err)
	}
	if out.Caller != "agt_master" || out.Card.Scope != s.Projects.List()[0].Scope().String() || len(out.Card.Inputs) != 1 || len(out.Card.Done) != 1 {
		t.Fatalf("preparation lost card fields: %+v", out)
	}
}
