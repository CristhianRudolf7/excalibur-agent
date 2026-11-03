package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestVisionAskImageGateway(t *testing.T) {
	image := base64.StdEncoding.EncodeToString(visionPNG(t))
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		parts := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if parts[0].(map[string]any)["text"] != "Look for a red cube" || parts[1].(map[string]any)["image_url"] != "data:image/png;base64,"+image || body["model"] != "vision-model" {
			t.Error(body)
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"{\"verified\":true}"}]}],"usage":{"input_tokens":7,"output_tokens":3}}`))
	}))
	defer server.Close()
	p, err := openai.New(openai.Options{APIKey: "fake", BaseURL: server.URL, UseResponses: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleVision: {Primary: models.ModelRef{Provider: "openai", Model: "vision-model"}}}}, VisionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Robot vision is independent of the document OCR feature switch.
	t.Setenv("EXCALIBUR_VISION_OCR", "off")
	answer, model, err := b.AskImage(context.Background(), "Look for a red cube", image)
	if err != nil || answer != `{"verified":true}` || model != "vision-model" {
		t.Fatalf("%s %s %v", answer, model, err)
	}
	if b.Usage().Attempts != 1 || b.Usage().Tokens.Input != 7 {
		t.Fatal(b.Usage())
	}
	for _, input := range []string{"bad-base64", base64.StdEncoding.EncodeToString([]byte("not a PNG"))} {
		if _, _, err := b.AskImage(context.Background(), "prompt", input); err == nil {
			t.Fatal("invalid image admitted")
		}
	}
	if _, _, err := b.AskImage(context.Background(), strings.Repeat("x", 16001), image); err == nil {
		t.Fatal("oversized prompt admitted")
	}
	if calls != 1 {
		t.Fatal("invalid inputs made model calls")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := b.AskImage(ctx, "prompt", image); err == nil {
		t.Fatal("cancel ignored")
	}
}
