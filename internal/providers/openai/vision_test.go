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
