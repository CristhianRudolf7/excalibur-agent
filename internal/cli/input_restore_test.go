package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestExecManifestRestoreWiring(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "provided.txt")
	if err := os.WriteFile(original, []byte("original bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	fake := providerfake.New("input-restore")
	call := func(id, name string, args any) providers.ChatResponse {
		raw, _ := json.Marshal(args)
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: id, Name: name, Arguments: raw}}}}
	}
	fake.Responses = []providers.ChatResponse{
		call("damage", "write_file", map[string]string{"path": original, "content": "damaged"}),
		call("restore", "restore_input", map[string]string{"path": "provided.txt"}),
		call("check", "exec", map[string]string{"command": "test \"$(cat provided.txt)\" = 'original bytes'"}),
	}
	fake.Responses = append(fake.Responses, finishScript("restored", "checked original bytes")...)
	out, err := RunExec(context.Background(), ExecOptions{MaxSteps: 12, TimeoutSecs: 10, Request: "Inspect supplied data", LogsDir: t.TempDir(), Profile: ProfileEval, Provider: fake, ProjectDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatal(out.State)
	}
	data, err := os.ReadFile(original)
	if err != nil || string(data) != "original bytes" {
		t.Fatalf("restored %q: %v", data, err)
	}
	found := false
	for _, def := range fake.ChatRequests[0].Tools {
		if def.Name == "restore_input" {
			found = true
		}
	}
	if !found {
		t.Fatal("restore tool not advertised")
	}
}
