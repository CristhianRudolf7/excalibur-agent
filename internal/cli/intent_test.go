package cli

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestExecIntentToolsReachFirstModelRequest(t *testing.T) {
	// Completion gates have separate suites; these runs inspect discovery before
	// any tool call so the provider cannot recover through search_tools.
	t.Setenv("EXCALIBUR_EXPLICIT_FINISH", "off")
	t.Setenv("EXCALIBUR_SELFVERIFY", "0")
	t.Setenv("EXCALIBUR_ARTIFACT_GATE", "0")
	t.Setenv("EXCALIBUR_SKILLS", "cards")
	var evalTools []providers.ToolDefinition
	for _, tc := range []struct {
		profile ProfileName
		request string
		want    bool
	}{
		{ProfileDefault, "draw an architecture diagram of this repo", true},
		{ProfileDefault, "haz un diagrama de flujo", true},
		{ProfileDelegate, "draw an architecture diagram of this repo", true},
		{ProfileDefault, "explain this failing test", false},
		{ProfileEval, "draw an architecture diagram of this repo", false},
		{ProfileEval, "explain this failing test", false},
	} {
		t.Run(string(tc.profile)+"/"+tc.request, func(t *testing.T) {
			fake := providerfake.New("intent-discovery")
			fake.Responses = []providers.ChatResponse{{Message: providers.Message{Role: providers.RoleAssistant, Content: "Finished."}}}
			store := journal.NewMemStore()
			out, err := RunExec(context.Background(), ExecOptions{Request: tc.request, Profile: tc.profile, LogsDir: t.TempDir(), ProjectDir: t.TempDir(), Provider: fake, JournalStore: store})
			if err != nil {
				t.Fatal(err)
			}
			if len(fake.ChatRequests) == 0 {
				t.Fatal("model was not called")
			}
			req := fake.ChatRequests[0]
			var names []string
			for _, def := range req.Tools {
				names = append(names, def.Name)
			}
			for _, name := range []string{"diagram_render", "code_diagram"} {
				if slices.Contains(names, name) != tc.want {
					t.Errorf("%s presence=%v want=%v", name, slices.Contains(names, name), tc.want)
				}
			}
			entries, err := store.List(context.Background(), string(out.RunID))
			if err != nil {
				t.Fatal(err)
			}
			count, modelSeen := 0, false
			var activation wireengine.ToolActivation
			for _, entry := range entries {
				if entry.Type == journal.TypeModelRequest {
					modelSeen = true
				}
				if entry.Type != "tools_activated" {
					continue
				}
				if modelSeen {
					t.Error("activation recorded after model request")
				}
				count++
				if err := json.Unmarshal(entry.Payload, &activation); err != nil {
					t.Fatal(err)
				}
			}
			if tc.want {
				if count != 1 || len(activation.Skills) != 1 {
					t.Fatalf("activation count=%d hints=%v", count, activation.Skills)
				}
				var prompt strings.Builder
				for _, msg := range req.Messages {
					prompt.WriteString(msg.Content)
				}
				if !strings.Contains(prompt.String(), "load_skill with name="+activation.Skills[0].Name) {
					t.Fatal("first request lacks skill hint")
				}
			} else if tc.profile == ProfileEval && count != 0 {
				t.Fatal("eval recorded activation")
			}
			if tc.profile == ProfileEval {
				if evalTools == nil {
					evalTools = req.Tools
				} else if !reflect.DeepEqual(evalTools, req.Tools) {
					t.Fatal("diagram intent changed eval tools")
				}
			}
		})
	}
}
