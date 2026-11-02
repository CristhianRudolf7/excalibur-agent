//go:build live

package daemon

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestLiveDaemonTavilySearch(t *testing.T) {
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1")
	}
	path := os.Getenv("EXCALIBUR_TAVILY_HEADER_FILE")
	if path == "" {
		t.Skip("EXCALIBUR_TAVILY_HEADER_FILE required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("credential file unavailable")
	}
	key, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "Authorization: Bearer ")
	if !ok || key == "" || strings.ContainsAny(key, "\r\n") {
		t.Fatal("invalid credential file")
	}
	t.Setenv("BR3_LIVE_TAVILY_KEY", key)
	cfg := config.Default()
	cfg.Browser.Enabled = false
	cfg.Search.Order = []string{"tavily"}
	cfg.Search.Providers["tavily"] = config.SearchProviderConfig{KeyRef: "env:BR3_LIVE_TAVILY_KEY"}
	svc, err := Assemble(AssembleOptions{BrowserWeb: &wireengine.BrowserWebOptions{Config: cfg, DataDir: t.TempDir(), Warn: func(string) {}}})
	if err != nil {
		t.Fatal("assembly failed")
	}
	defer svc.BrowserWeb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := svc.Exec.Execute(ctx, "web_search", json.RawMessage(`{"query":"Go programming language official documentation","k":1}`), tools.ExecuteOptions{})
	if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, `"results"`) {
		t.Fatal("daemon Tavily search failed")
	}
	t.Log("daemon Tavily tool execution passed; response contents omitted")
}
