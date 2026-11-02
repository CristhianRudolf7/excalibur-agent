package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestVisionWiredInDaemonAndEmbedded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		parts := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if parts[1].(map[string]any)["type"] != "input_image" {
			t.Error("missing image")
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Actual page text"}]}]}`))
	}))
	defer srv.Close()
	p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
	defer p.Close()
	media, err := wireengine.ModelVisionMedia(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleExecutor: {Primary: models.ModelRef{Provider: "openai", Model: "default-model"}}}}, "vision-model")
	if err != nil {
		t.Fatal(err)
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{Media: media})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "memory.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := memory.EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	memSvc := memory.NewService(db, wireengine.VisionMemoryOption(media))
	svc, err := Assemble(AssembleOptions{Media: media, Memory: memSvc})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Memory.Ingest(context.Background(), []string{"project:test"}, "page.png", "image/png", buf.Bytes(), memory.IngestOptions{Origin: path}); err != nil {
		t.Fatalf("daemon memory vision: %v", err)
	}
	args, _ := json.Marshal(map[string]any{"input": path, "mode": "text", "model": "must-not-spoof"})
	for name, reg := range map[string]*tools.MapRegistry{"daemon": svc.Tools, "embedded": emb.Registry} {
		t.Run(name, func(t *testing.T) {
			tool, err := reg.Get("vision_extract")
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: args})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			var got tools.VisionResult
			if err := json.Unmarshal([]byte(result.Output), &got); err != nil {
				t.Fatal(err)
			}
			if got.Model != "vision-model" || len(got.Pages) != 1 || got.Pages[0].Text != "Actual page text" {
				t.Fatalf("got=%+v", got)
			}
		})
	}
}
