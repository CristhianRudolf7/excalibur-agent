package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
)

func TestWhiteboardScriptedAssistantTurn(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	calls := 0
	script := []string{`{"action":"open"}`, `{"action":"add","animate":true,"elements":[{"id":"request","type":"rect","x":10,"y":10,"width":100,"height":50}]}`, `{"action":"highlight","ids":["request"],"style":"pulse"}`, `{"action":"read"}`}
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		if calls < len(script) {
			args := script[calls]
			calls++
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: string(rune('a' + calls)), Name: "whiteboard", Arguments: json.RawMessage(args)}}}}, nil
		}
		found := false
		for _, m := range req.Messages {
			if m.ToolResult != nil && strings.Contains(m.ToolResult.Output, `"elements"`) && strings.Contains(m.ToolResult.Output, `"id":"request"`) {
				found = true
			}
		}
		if !found {
			t.Error("model did not receive the current board from read")
		}
		return textReply("The request flows through this box."), nil
	})
	bus := telemetry.NewBus(nil)
	s.Whiteboard.Bus = bus
	b.Services.Whiteboard = s.Whiteboard
	_, _, err := b.ConversationSend(context.Background(), "test-client", "board-message", "cnv_lesson", "Explain the request visually.")
	if err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, "cnv_lesson", 1)
	seen := map[types.EventType]bool{}
	for _, ev := range bus.Replay(types.Cursor{}) {
		seen[ev.Type] = true
		var p map[string]any
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p["conversation_id"] != "cnv_lesson" {
			t.Fatalf("wrong conversation: %s", ev.Payload)
		}
	}
	for _, name := range []types.EventType{"whiteboard.opened", "whiteboard.ops", "whiteboard.highlight"} {
		if !seen[name] {
			t.Fatalf("missing %s; events=%+v", name, bus.Replay(types.Cursor{}))
		}
	}
	boards, err := s.Whiteboard.Store.List(context.Background(), "cnv_lesson")
	if err != nil || len(boards) != 1 {
		t.Fatalf("boards=%+v %v", boards, err)
	}
	board, err := s.Whiteboard.Store.Get(context.Background(), boards[0].ID)
	if err != nil || len(board.Elements) != 1 || board.Elements[0].ID != "request" {
		t.Fatalf("board=%+v %v", board, err)
	}
	if calls != 4 {
		t.Fatalf("model script made %d tool calls", calls)
	}
}
func TestWhiteboardGatewayRouting(t *testing.T) {
	bus := telemetry.NewBus(nil)
	svc := whiteboard.NewService(nil, bus)
	board, err := svc.Open(context.Background(), "cnv_rpc", "", "RPC", "panel")
	if err != nil {
		t.Fatal(err)
	}
	b := NewDaemonBackend(nil, nil, bus)
	b.AttachServices(BackendServices{Whiteboard: svc})
	server := httptest.NewServer(gateway.New(gateway.Config{}, b, gateway.NewMemoryBlobs(), bus))
	defer server.Close()
	call := func(method, params string) api.Response {
		t.Helper()
		req := []byte(`{"jsonrpc":"2.0","id":1,"method":"` + method + `","params":` + params + `}`)
		res, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(req))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out api.Response
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, method := range []string{api.MethodWhiteboardList, api.MethodWhiteboardGet} {
		res := call(method, `{"board_id":"`+board.ID+`"}`)
		if res.Error != nil {
			t.Fatalf("%s: %+v", method, res.Error)
		}
	}
	params := `{"board_id":"` + board.ID + `","base_version":1,"client_op_id":"ui-edit","ops":[{"op":"add","elements":[{"id":"u","type":"text","x":10,"y":10,"text":"User"}]}]}`
	res := call(api.MethodWhiteboardApply, params)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	res = call(api.MethodWhiteboardApply, params)
	if res.Error != nil {
		t.Fatal(res.Error)
	}
	res = call(api.MethodWhiteboardApply, `{"board_id":"`+board.ID+`","base_version":1,"client_op_id":"stale","ops":[{"op":"clear"}]}`)
	if res.Error == nil || res.Error.Code != api.CodeConflict {
		t.Fatalf("stale: %+v", res)
	}
	res = call(api.MethodWhiteboardSnapshot, `{"board_id":"`+board.ID+`","image_blob":"missing"}`)
	if res.Error == nil || res.Error.Code != api.CodeInvalidParams {
		t.Fatal(res)
	}
	res = call(api.MethodWhiteboardGet, `{}`)
	if res.Error == nil || res.Error.Code != api.CodeInvalidParams {
		t.Fatal(res)
	}
	res = call(api.MethodWhiteboardClose, `{"board_id":"`+board.ID+`"}`)
	if res.Error != nil {
		t.Fatal(res.Error)
	}

}
