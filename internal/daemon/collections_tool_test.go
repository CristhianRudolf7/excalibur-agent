package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestMyDataToolConfirmationAndActions(t *testing.T) {
	b, s := assistantTestWiring(t, false, nil)
	routes, _ := collectionRoutesTest(t)
	b.Services.Collections = routes
	tl, err := s.Tools.Get("my_data")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Catalog.IsDeferred("my_data") || !slices.Equal(tl.Definition().Profiles, []string{"default"}) {
		t.Fatal(tl.Definition())
	}
	hash := s.Catalog.PrefixHash()
	if err = wireCollectionTool(b, s); err != nil || s.Catalog.PrefixHash() != hash {
		t.Fatal("repeat wiring changed prefix", err)
	}
	invoke := func(args map[string]any, want tools.ResultStatus) map[string]json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(args)
		result, err := tl.Invoke(context.Background(), tools.Invocation{Arguments: raw})
		if err != nil || result.Status != want {
			t.Fatalf("%v: %+v %v", args, result, err)
		}
		var out map[string]json.RawMessage
		if err = json.Unmarshal([]byte(result.Output), &out); err != nil {
			t.Fatal(result, err)
		}
		return out
	}
	args := map[string]any{"action": "propose", "name": "Grades", "fields": []api.CollectionField{{ID: "name", Name: "Course", Type: "text", Required: true}, {ID: "grade", Name: "Grade", Type: "number"}}}
	proposal := invoke(args, tools.StatusOK)
	if len(proposal["example_rows"]) == 0 || !strings.Contains(string(proposal["schema_table"]), "Course") {
		t.Fatal(proposal)
	}
	list, _ := routes.Engine.List(context.Background())
	if len(list) != 0 {
		t.Fatal("proposal created collection")
	}
	args["action"] = "create"
	invoke(args, tools.StatusDenied)
	list, _ = routes.Engine.List(context.Background())
	if len(list) != 0 {
		t.Fatal("unconfirmed create wrote collection")
	}
	args["confirmed"] = true
	out := invoke(args, tools.StatusOK)
	var created api.Collection
	_ = json.Unmarshal(out["collection"], &created)
	if created.ID == "" || !strings.Contains(string(out["notice"]), "Mis datos") {
		t.Fatal(out)
	}
	id := created.ID
	invoke(map[string]any{"action": "list"}, tools.StatusOK)
	out = invoke(map[string]any{"action": "describe", "id": id}, tools.StatusOK)
	if out["schema_versions"] == nil {
		t.Fatal(out)
	}
	out = invoke(map[string]any{"action": "insert", "collection": id, "values": map[string]any{"name": "Math", "grade": 15}}, tools.StatusOK)
	var record api.RecordRow
	_ = json.Unmarshal(out["record"], &record)
	invoke(map[string]any{"action": "update", "collection": id, "id": record.ID, "values": map[string]any{"grade": 18}}, tools.StatusOK)
	invoke(map[string]any{"action": "query", "collection": id}, tools.StatusOK)
	invoke(map[string]any{"action": "summary", "id": id}, tools.StatusOK)
	alter := map[string]any{"action": "alter_fields", "id": id, "remove": []string{"grade"}}
	invoke(alter, tools.StatusDenied)
	alter["dry_run"] = true
	invoke(alter, tools.StatusOK)
	after, _ := routes.Engine.Get(context.Background(), id)
	if len(after.Fields) != 2 {
		t.Fatal("dry run removed field")
	}
	alter["dry_run"] = false
	alter["confirmed"] = true
	invoke(alter, tools.StatusOK)
	invoke(map[string]any{"action": "restore", "id": id, "version": 1}, tools.StatusDenied)
	invoke(map[string]any{"action": "restore", "id": id, "version": 1, "confirmed": true}, tools.StatusOK)
	out = invoke(map[string]any{"action": "export", "id": id, "format": "csv"}, tools.StatusOK)
	if !strings.Contains(string(out["url"]), "/blobs/") {
		t.Fatal(out)
	}
	var digest string
	_ = json.Unmarshal(out["blob_digest"], &digest)
	imported := invoke(map[string]any{"action": "import", "collection": id, "blob_digest": digest}, tools.StatusOK)
	var report api.ImportReport
	_ = json.Unmarshal(imported["report"], &report)
	if !report.DryRun {
		t.Fatal("tool import default committed")
	}
	invoke(map[string]any{"action": "import", "blob_digest": digest, "create_schema": true, "commit": true}, tools.StatusDenied)
	invoke(map[string]any{"action": "import", "blob_digest": digest, "create_schema": true, "commit": true, "confirmed": true}, tools.StatusOK)
	invoke(map[string]any{"action": "delete_record", "collection": id, "id": record.ID}, tools.StatusOK)
	invoke(map[string]any{"action": "delete_collection", "id": id}, tools.StatusDenied)
	invoke(map[string]any{"action": "delete_collection", "id": id, "confirmed": true}, tools.StatusOK)
	out = invoke(map[string]any{"action": "list", "include_trash": true}, tools.StatusOK)
	if !strings.Contains(string(out["trash"]), id) {
		t.Fatal(out)
	}
	invoke(map[string]any{"action": "restore", "id": id}, tools.StatusOK)
	invoke(map[string]any{"action": "invalid"}, tools.StatusInputError)
}
func TestMyDataVisibleAndExecutableInChat(t *testing.T) {
	calls := 0
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if !slices.ContainsFunc(req.Tools, func(d providers.ToolDefinition) bool { return d.Name == "my_data" }) {
			t.Error("chat omitted my_data")
		}
		if calls == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "my-data-list", Name: "my_data", Arguments: json.RawMessage(`{"action":"list"}`)}}}}, nil
		}
		found := false
		for _, m := range req.Messages {
			if m.ToolResult != nil && m.ToolResult.CallID == "my-data-list" {
				found = strings.Contains(m.ToolResult.Output, "collections")
			}
		}
		if !found {
			t.Error("tool result absent from chat")
		}
		return textReply("I can propose a collection for your grades."), nil
	})
	routes, _ := collectionRoutesTest(t)
	b.Services.Collections = routes
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	if _, _, err := a.CompleteTurn(context.Background(), "cnv_mydata", master.Normalized{Text: "Track my grades from the attached CSV"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	description := (&collectionTool{}).Definition().Description
	for _, term := range []string{"FIRST call propose", "Excel/CSV", "dry run", "Never invent", "Mis datos", "My data"} {
		if !strings.Contains(description, term) {
			t.Fatal("missing behavior", term)
		}
	}
}
