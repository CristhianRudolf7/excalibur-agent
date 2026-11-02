package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

func catalogIndex(t *testing.T) *skills.Index {
	t.Helper()
	raw, err := fs.ReadFile(skills.BuiltinFS(), "planning/task-breakdown/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	ix := skills.NewIndex(skills.Roots{User: t.TempDir()}, fstest.MapFS{"planning/task-breakdown/SKILL.md": &fstest.MapFile{Data: raw}})
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestCatalogListGroupsAndContent(t *testing.T) {
	ix := catalogIndex(t)
	result, err := SkillsOp(ix, api.MethodSkillsList, nil)
	if err != nil {
		t.Fatal(err)
	}
	list := result.(map[string]any)["skills"].([]api.SkillSummary)
	if len(list) != len(ix.All()) {
		t.Fatal("list count")
	}
	for _, s := range list {
		if s.Source != "builtin" || !s.Editable || s.Deletable || s.Tools == nil {
			t.Fatalf("summary: %+v", s)
		}
	}
	result, err = SkillsOp(ix, api.MethodSkillsGroups, nil)
	if err != nil {
		t.Fatal(err)
	}
	groups := result.(map[string]any)["groups"].([]api.SkillGroup)
	total := 0
	for _, g := range groups {
		if g.NameEN == "" || g.NameES == "" || g.DescriptionEN == "" || g.DescriptionES == "" {
			t.Fatal("group localization missing")
		}
		total += g.Count
	}
	if total != len(list) {
		t.Fatal("group counts")
	}
	result, err = SkillsOp(ix, api.MethodSkillsList, json.RawMessage(`{"group":"planning","text":"breakdown","source":"builtin"}`))
	if err != nil {
		t.Fatal(err)
	}
	filtered := result.(map[string]any)["skills"].([]api.SkillSummary)
	if len(filtered) != 1 || filtered[0].Name != "task-breakdown" {
		t.Fatalf("filters: %+v", filtered)
	}
	result, err = SkillsOp(ix, api.MethodSkillsGet, json.RawMessage(`{"name":"task-breakdown"}`))
	if err != nil {
		t.Fatal(err)
	}
	detail := result.(api.SkillDetail)
	raw, _ := ix.Content("task-breakdown")
	if detail.Content != raw || detail.PathKind != "builtin" || detail.Diagnostics == nil {
		t.Fatalf("detail: %+v", detail)
	}
}

func TestCatalogMutationsAndErrors(t *testing.T) {
	ix := catalogIndex(t)
	call := func(method string, p any) (any, error) { raw, _ := json.Marshal(p); return SkillsOp(ix, method, raw) }
	result, err := call(api.MethodSkillsSetState, map[string]string{"name": "task-breakdown", "state": "disabled"})
	if err != nil || result.(api.SkillSummary).State != "disabled" {
		t.Fatalf("state: %v %v", result, err)
	}
	if _, err := ix.Load("task-breakdown"); err == nil {
		t.Fatal("disabled builtin still loads")
	}
	_, err = call(api.MethodSkillsUpdate, map[string]string{"name": "task-breakdown", "content": "invalid"})
	apiErr, ok := err.(*api.Error)
	if !ok || apiErr.Code != api.CodeInvalidParams || apiErr.Data == nil {
		t.Fatalf("validation: %v", err)
	}
	raw, _ := ix.Content("task-breakdown")
	result, err = call(api.MethodSkillsUpdate, map[string]string{"name": "task-breakdown", "content": raw + "\nUpdate.\n"})
	if err != nil || result.(api.SkillSummary).Source != "user" {
		t.Fatalf("update: %v %v", result, err)
	}
	_, err = call(api.MethodSkillsDelete, map[string]string{"name": "task-breakdown"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = call(api.MethodSkillsDelete, map[string]string{"name": "task-breakdown"})
	if err == nil || err.(*api.Error).Message != "builtin skills can be disabled, not deleted" {
		t.Fatalf("builtin delete: %v", err)
	}
	for _, tc := range []struct {
		method, params string
		code           int
	}{
		{api.MethodSkillsGet, `{}`, api.CodeInvalidParams},
		{api.MethodSkillsList, `[]`, api.CodeInvalidParams},
		{api.MethodSkillsGroups, `null`, api.CodeInvalidParams},
		{api.MethodSkillsList, `{"source":"invalid"}`, api.CodeInvalidParams},
		{api.MethodSkillsGet, `{"name":"missing"}`, api.CodeUnknownTarget},
		{api.MethodSkillsSetState, `{"name":"task-breakdown","state":"bad"}`, api.CodeInvalidParams},
	} {
		_, err := SkillsOp(ix, tc.method, json.RawMessage(tc.params))
		if e, ok := err.(*api.Error); !ok || e.Code != tc.code {
			t.Fatalf("%s %s: %v", tc.method, tc.params, err)
		}
	}
}

func TestUsageSummaryBackend(t *testing.T) {
	ctx := context.Background()
	opts := store.TelemetryOptions()
	opts.BlobDir = t.TempDir()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "telemetry.db"), opts, migrations.Telemetry())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	durable := telemetry.OpenTelemetryStore(db)
	snap := telemetry.Snapshot{TaskID: "task", RunID: "run", Model: "model", StartedAt: time.Now().UTC(), Usage: providers.Usage{Input: 12, Output: 3}, CostUSD: 0.2, CallsByTool: map[string]int64{"read": 2}}
	if err := durable.SaveMetrics(snap); err != nil {
		t.Fatal(err)
	}
	metrics := &MetricsStore{}
	metrics.Record("task", "", snap)
	result, err := usageSummaryOp(ctx, metrics, durable, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := result.(api.UsageSummaryResult)
	if summary.Totals.Runs != 1 || summary.Totals.InputTokens != 12 || summary.Totals.ToolCalls != 2 {
		t.Fatalf("summary: %+v", summary)
	}
	for _, raw := range []string{`{"bucket":"month"}`, `{"since":"bad"}`, `{"since":"2026-01-02T00:00:00Z","until":"2026-01-01T00:00:00Z"}`, `[]`, `null`} {
		_, err := usageSummaryOp(ctx, nil, nil, json.RawMessage(raw))
		if e, ok := err.(*api.Error); !ok || e.Code != api.CodeInvalidParams {
			t.Fatalf("invalid %s: %v", raw, err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := usageSummaryOp(cancelled, nil, nil, nil); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestCatalogAndUsageGatewayDispatch(t *testing.T) {
	backend := NewDaemonBackend(nil, nil, nil)
	backend.AttachServices(BackendServices{Skills: catalogIndex(t), Metrics: &MetricsStore{}})
	server := gateway.New(gateway.DefaultConfig(), backend, nil, nil)
	for _, method := range []string{api.MethodSkillsGroups, api.MethodSkillsList, api.MethodSkillsGet, api.MethodSkillsUpdate, api.MethodSkillsSetState, api.MethodSkillsDelete, api.MethodUsageSummary} {
		params := map[string]string{"name": "task-breakdown", "state": "enabled"}
		if method == api.MethodSkillsUpdate {
			params["content"], _ = backend.Services.Skills.Content("task-breakdown")
		}
		if method == api.MethodUsageSummary {
			params = map[string]string{}
		}
		req, _ := api.NewRequest("1", method, params)
		raw, _ := json.Marshal(req)
		request := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		server.ServeHTTP(w, request)
		var response api.Response
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error != nil {
			t.Fatalf("%s: %s %v", method, w.Body.String(), err)
		}
	}
}

func TestCatalogUsageSwitches(t *testing.T) {
	ix := catalogIndex(t)
	t.Setenv("EXCALIBUR_SKILLS_MANAGEMENT", "off")
	if _, err := SkillsOp(ix, api.MethodSkillsList, nil); err == nil {
		t.Fatal("skills switch ignored")
	}
	// The existing model-facing API is unaffected by the client-management switch.
	if _, err := SkillsOp(ix, api.MethodSkillsSearch, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_USAGE_SUMMARY", "0")
	if _, err := usageSummaryOp(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("usage switch ignored")
	}
}
