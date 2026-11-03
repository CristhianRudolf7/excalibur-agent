package openai

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"net/http"
	"testing"
)

func TestResponsesImageInput(t *testing.T) {
	const data = "data:image/png;base64,aGVsbG8="
	srv := respServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		input := body["input"].([]any)[0].(map[string]any)
		parts := input["content"].([]any)
		if input["role"] != "user" || len(parts) != 2 {
			t.Errorf("input = %#v", input)
		}
		img := parts[1].(map[string]any)
		if len(img) != 2 || img["type"] != "input_image" || img["image_url"] != data {
			t.Errorf("image = %#v", img)
		}
		writeJSON(t, w, map[string]any{"status": "completed", "model": "vision", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "hello"}}}}})
	})
	defer srv.Close()
	a := newRespAdapter(t, srv)
	resp, err := a.Chat(context.Background(), providers.ChatRequest{Model: "vision", Messages: []providers.Message{{Role: providers.RoleUser, Content: "transcribe", Images: []providers.ImageInput{{URL: data}}}}})
	if err != nil || resp.Message.Content != "hello" {
		t.Fatalf("response=%+v err=%v", resp, err)
	}
}

func TestResponsesToolResultImage(t *testing.T) {
	const data = "data:image/png;base64,aGVsbG8="
	srv := respServer(t, func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		input := body["input"].([]any)
		item := input[1].(map[string]any)
		if item["type"] != "function_call_output" || item["call_id"] != "look-1" {
			t.Errorf("item=%+v", item)
		}
		parts := item["output"].([]any)
		if len(parts) != 2 || parts[0].(map[string]any)["text"] != "arm state" || parts[1].(map[string]any)["type"] != "input_image" || parts[1].(map[string]any)["image_url"] != data {
			t.Errorf("parts=%+v", parts)
		}
		writeJSON(t, w, map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": "I see the gripper"}}}}})
	})
	defer srv.Close()
	a := newRespAdapter(t, srv)
	response, err := a.Chat(context.Background(), providers.ChatRequest{Model: "existing-model", Messages: []providers.Message{
		{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "look-1", Name: "robot", Arguments: []byte(`{"action":"look"}`)}}},
		{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "look-1", Name: "robot", Output: "arm state", Images: []providers.ImageInput{{URL: data}}}},
	}})
	if err != nil || response.Message.Content != "I see the gripper" {
		t.Fatalf("%+v %v", response, err)
	}
}

func TestResponsesRejectsEmptyToolImage(t *testing.T) {
	_, err := buildInput([]providers.Message{{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "look", Images: []providers.ImageInput{{}}}}})
	if err == nil {
		t.Fatal("empty image accepted")
	}
}

func TestChatCompletionsRejectsToolImages(t *testing.T) {
	a, err := New(Options{APIKey: "fake", BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	req := providers.ChatRequest{Model: "test", Messages: []providers.Message{{Role: providers.RoleTool, ToolResult: &providers.ToolResult{CallID: "look", Images: []providers.ImageInput{{URL: "data:image/png;base64,aA=="}}}}}}
	if _, err = a.Chat(context.Background(), req); err == nil {
		t.Fatal("image silently discarded")
	}
	if err = a.Stream(context.Background(), req, func(providers.StreamEvent) error { return nil }); err == nil {
		t.Fatal("stream image silently discarded")
	}
}
