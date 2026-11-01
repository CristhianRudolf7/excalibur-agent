package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
)

func TestScopeGuardDefaultsToCurrentDirectory(t *testing.T) {
	t.Setenv("EXCALIBUR_SCOPE_GUARD", "on")
	fake := providerfake.New("scope-default-root")
	fake.Responses = finishScript("done", "verified")
	out, err := RunExec(context.Background(), ExecOptions{Request: "hi", LogsDir: t.TempDir(), Profile: ProfileEval, Provider: fake})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out.Dir, "scope_guard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Enabled bool   `json:"enabled"`
		Root    string `json:"root"`
	}
	if err = json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Enabled || rec.Root != cwd {
		t.Fatalf("guard without --project: %+v", rec)
	}
}
