package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/openai"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func visionPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestModelVisionPayloadUsageAndFallback(t *testing.T) {
	data := visionPNG(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["max_output_tokens"] != float64(4096) {
			t.Errorf("output token cap = %v", body["max_output_tokens"])
		}
		if body["model"] != "default-model" {
			t.Errorf("model=%v", body["model"])
		}
		parts := body["input"].([]any)[0].(map[string]any)["content"].([]any)
		if got := parts[1].(map[string]any)["image_url"]; got != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(data) {
			t.Errorf("incorrect image payload")
		}
		if !strings.Contains(parts[0].(map[string]any)["text"].(string), "Page 7") {
			t.Error("page missing")
		}
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"# Heading\n\nText"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer srv.Close()
	p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
	defer p.Close()
	warned := false
	b, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleExecutor: {Primary: models.ModelRef{Provider: "openai", Model: "default-model"}}}, Prices: map[string]models.Price{"default-model": {Input: 1, Output: 2}}}, VisionOptions{Warn: func(string) { warned = true }})
	if err != nil {
		t.Fatal(err)
	}
	got, err := b.ExtractPNG(context.Background(), data, 7)
	if err != nil || got.Model != "default-model" || got.Pages[0].Page != 7 || !warned {
		t.Fatalf("got=%+v err=%v warned=%v", got, err, warned)
	}
	usage := b.Usage()
	if usage.Pages != 1 || usage.Attempts != 1 || usage.Tokens.Input != 10 || usage.Tokens.Output != 5 || usage.CostUSD <= 0 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestModelVisionRetriesAndHonestErrors(t *testing.T) {
	t.Setenv("EXCALIBUR_PROVIDER_RETRY_V2", "0")
	for _, status := range []int{429, 500, 401, 400, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				if status == 200 {
					_, _ = w.Write([]byte(`{"status":"completed","output":[]}`))
				} else {
					_, _ = w.Write([]byte(`{"error":{"message":"failure"}}`))
				}
			}))
			defer srv.Close()
			p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
			defer p.Close()
			b, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleVision: {Primary: models.ModelRef{Provider: "openai", Model: "vision"}}}, BaseDelay: time.Millisecond}, VisionOptions{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := b.ExtractPNG(context.Background(), visionPNG(t), 1)
			if err == nil || len(result.Pages) != 0 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			want := 1
			if status == 429 || status == 500 {
				want = 3
			}
			if calls.Load() != int32(want) {
				t.Fatalf("calls=%d want=%d", calls.Load(), want)
			}
		})
	}
}

func TestModelVisionBoundsModesAndCancellation(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"visible"}]}]}`))
	}))
	defer srv.Close()
	p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
	defer p.Close()
	b, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleVision: {Primary: models.ModelRef{Provider: "openai", Model: "vision"}}}}, VisionOptions{MaxPages: 1})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "page.png")
	if err := os.WriteFile(path, visionPNG(t), 0600); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"text", "layout", "describe"} {
		result, err := b.Extract(context.Background(), path, mode, nil)
		if err != nil || result.Model != "vision" {
			t.Fatalf("%s: %+v %v", mode, result, err)
		}
		if mode == "describe" && result.Pages[0].Description == "" {
			t.Fatal("missing description")
		}
	}
	for _, mode := range []string{"bad"} {
		if _, err := b.Extract(context.Background(), path, mode, nil); err == nil {
			t.Fatal("accepted invalid mode")
		}
	}
	if _, err := b.Extract(context.Background(), path, "text", []int{1, 2}); err == nil {
		t.Fatal("page budget ignored")
	}
	if _, err := b.ExtractPNG(context.Background(), []byte("not png"), 1); err == nil {
		t.Fatal("invalid image accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.ExtractPNG(ctx, visionPNG(t), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	t.Setenv("EXCALIBUR_VISION_OCR", "0")
	if _, err := b.Extract(context.Background(), path, "text", nil); err == nil {
		t.Fatal("disabled vision called provider")
	}
	if calls.Load() != 3 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestModelVisionTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer srv.Close()
	p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
	defer p.Close()
	b, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleVision: {Primary: models.ModelRef{Provider: "openai", Model: "vision"}}}}, VisionOptions{Timeout: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.ExtractPNG(context.Background(), visionPNG(t), 1); err == nil {
		t.Fatal("timeout ignored")
	}
}

func TestModelVisionRenderedPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"page text"}]}]}`))
	}))
	defer srv.Close()
	p, _ := openai.New(openai.Options{APIKey: "fake", BaseURL: srv.URL, UseResponses: true})
	defer p.Close()
	var rendered []int
	imageData := visionPNG(t)
	backend, err := NewModelVisionBackend(p, models.GatewayConfig{Roles: map[types.ModelRole]models.RoleEntry{types.RoleVision: {Primary: models.ModelRef{Provider: "openai", Model: "vision"}}}}, VisionOptions{RenderPage: func(ctx context.Context, path string, page int) ([]byte, error) {
		rendered = append(rendered, page)
		return imageData, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "scanned.pdf")
	if err := os.WriteFile(path, []byte("%PDF-test"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := backend.Extract(context.Background(), path, "text", []int{3, 8})
	if err != nil || len(result.Pages) != 2 || result.Pages[0].Page != 3 || result.Pages[1].Page != 8 || len(rendered) != 2 {
		t.Fatalf("result=%+v rendered=%v err=%v", result, rendered, err)
	}
	if backend.Usage().Pages != 2 {
		t.Fatalf("usage=%+v", backend.Usage())
	}
}
