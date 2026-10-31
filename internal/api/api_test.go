package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contractsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "packages", "contracts"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestMethodsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Methods {
		if m.Name == "" || m.Family == "" || m.Summary == "" {
			t.Fatalf("method entry has empty field: %+v", m)
		}
		if !strings.Contains(m.Name, ".") {
			t.Fatalf("method %q is not family.name shaped", m.Name)
		}
		if seen[m.Name] {
			t.Fatalf("duplicate method %q", m.Name)
		}
		seen[m.Name] = true
	}
	if len(Methods) == 0 {
		t.Fatal("no methods registered")
	}
}

func TestEventTypesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range EventTypes {
		if e.Type == "" || e.Payload == "" || e.Summary == "" {
			t.Fatalf("event entry has empty field: %+v", e)
		}
		if !strings.Contains(string(e.Type), ".") {
			t.Fatalf("event %q is not family.name shaped", e.Type)
		}
		if seen[string(e.Type)] {
			t.Fatalf("duplicate event %q", e.Type)
		}
		seen[string(e.Type)] = true
	}
}

func TestContractsParity(t *testing.T) {
	dir := contractsDir(t)

	var methodsDoc struct {
		Methods []MethodInfo `json:"methods"`
	}
	raw, err := os.ReadFile(filepath.Join(dir, "methods.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &methodsDoc); err != nil {
		t.Fatal(err)
	}
	if len(methodsDoc.Methods) != len(Methods) {
		t.Fatalf("methods.json has %d entries, registry has %d",
			len(methodsDoc.Methods), len(Methods))
	}
	want := map[string]MethodInfo{}
	for _, m := range Methods {
		want[m.Name] = m
	}
	for _, m := range methodsDoc.Methods {
		w, ok := want[m.Name]
		if !ok {
			t.Fatalf("methods.json lists unknown method %q", m.Name)
		}
		if w != m {
			t.Fatalf("methods.json entry %+v differs from registry %+v", m, w)
		}
	}

	var eventsDoc struct {
		Events []EventTypeInfo `json:"events"`
	}
	raw, err = os.ReadFile(filepath.Join(dir, "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &eventsDoc); err != nil {
		t.Fatal(err)
	}
	if len(eventsDoc.Events) != len(EventTypes) {
		t.Fatalf("events.json has %d entries, registry has %d",
			len(eventsDoc.Events), len(EventTypes))
	}
	wantE := map[string]EventTypeInfo{}
	for _, e := range EventTypes {
		wantE[string(e.Type)] = e
	}
	for _, e := range eventsDoc.Events {
		w, ok := wantE[string(e.Type)]
		if !ok {
			t.Fatalf("events.json lists unknown event %q", e.Type)
		}
		if w != e {
			t.Fatalf("events.json entry %+v differs from registry %+v", e, w)
		}
	}

	for _, name := range []string{"jsonrpc.json", "methods.json", "events.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
	}
}

func TestEnvelopeRoundtrip(t *testing.T) {
	req, err := NewRequest("1", MethodConversationSend, ConversationSendParams{
		ConversationID: "cnv_x",
		ClientMsgID:    "client-1",
		Text:           "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := decoded.Validate(); err != nil {
		t.Fatal(err)
	}
	id, err := decoded.IDString()
	if err != nil || id != "1" {
		t.Fatalf("id roundtrip: %q %v", id, err)
	}
	var params ConversationSendParams
	if err := json.Unmarshal(decoded.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params.ClientMsgID != "client-1" {
		t.Fatal("params roundtrip")
	}

	resp, err := NewResult(decoded.ID, ConversationSendResult{MsgID: "msg_1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil || resp.Result == nil {
		t.Fatal("result response shape")
	}
	errResp := NewError(decoded.ID, &Error{Code: CodeUnknownTarget, Message: "no such run"})
	if errResp.Result != nil || errResp.Error == nil {
		t.Fatal("error response shape")
	}
	if errResp.Error.Error() == "" {
		t.Fatal("Error() should describe the error")
	}

	bad := &Request{JSONRPC: "1.0", Method: MethodRunList}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected version validation error")
	}
	empty := &Request{JSONRPC: JSONRPCVersion}
	if err := empty.Validate(); err == nil {
		t.Fatal("expected missing-method error")
	}
}
