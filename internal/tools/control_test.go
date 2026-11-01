package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClockNowConvertAdd(t *testing.T) {
	fixed := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	c := &ClockTool{Now: func() time.Time { return fixed }}
	res, _ := c.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"action":"now","zone":"Asia/Tokyo"}`)})
	if res.Status != StatusOK || !strings.Contains(res.Output, "2026-10-31") {
		t.Fatalf("now: %+v", res)
	}
	raw, _ := json.Marshal(map[string]string{"action": "convert", "time": "2026-10-31T12:00:00Z", "to": "America/Lima"})
	res2, _ := c.Invoke(context.Background(), Invocation{Arguments: raw})
	if res2.Status != StatusOK || !strings.Contains(res2.Output, "07:00:00") {
		t.Fatalf("convert UTC->Lima is -5: %+v", res2)
	}
	raw3, _ := json.Marshal(map[string]string{"action": "add", "time": "2026-10-31T12:00:00Z", "add": "26h30m", "zone": "UTC"})
	res3, _ := c.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusOK || !strings.Contains(res3.Output, "2026-11-01") {
		t.Fatalf("add: %+v", res3)
	}
	bad, _ := c.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"action":"now","zone":"Mars/Olympus"}`)})
	if bad.Status != StatusInputError {
		t.Fatalf("bad zone: %+v", bad)
	}
}

func TestGetToolOutputPaging(t *testing.T) {
	store := NewMemoryOutputStore()
	ref := store.Put(strings.Repeat("x", 5000))
	g := &GetToolOutputTool{Outputs: store}
	raw, _ := json.Marshal(map[string]any{"ref": ref, "offset": 100, "limit": 50})
	res, _ := g.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK || !strings.Contains(res.Output, "bytes 100..150 of 5000") {
		t.Fatalf("paging: %+v", res)
	}
	raw2, _ := json.Marshal(map[string]string{"ref": "blob-sha256:nope"})
	res2, _ := g.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusInputError {
		t.Fatalf("unknown ref: %+v", res2)
	}
}

type mapSnapshot struct{ data map[string]any }

func (m mapSnapshot) Snapshot() map[string]any { return m.data }

func TestStatusRendersSnapshotAndSessions(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("e1", "echo status-live", "", false, 3000, nil)
	time.Sleep(300 * time.Millisecond)
	st := &StatusTool{Provider: mapSnapshot{map[string]any{"runs": 2}}, Sessions: m}
	res, _ := st.Invoke(context.Background(), Invocation{})
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "runs") || !strings.Contains(res.Output, string(s.ID)) {
		t.Fatalf("snapshot+sessions: %q", res.Output)
	}
	_ = m.Close(s.ID)
	empty := &StatusTool{}
	res2, _ := empty.Invoke(context.Background(), Invocation{})
	if res2.Status != StatusOK || !strings.Contains(res2.Output, "no runs") || strings.Contains(res2.Output, "not wired") {
		t.Fatalf("empty status must say there are no runs: %+v", res2)
	}
}

func TestCatalogNamesAndManifest(t *testing.T) {
	// The base fast/read-only contract applies with provisioning disabled.
	// Auto status effects are checked in TestProvisionStatusSessionTail.
	t.Setenv(AutoProvisionEnv, "off")
	reg := NewMapRegistry()
	deps := DefaultBuiltinDeps()
	if err := RegisterBuiltins(reg, deps); err != nil {
		t.Fatal(err)
	}
	names := reg.Names()
	seen := map[string]bool{}
	for _, n := range names {
		if err := ValidateName(n); err != nil {
			t.Fatalf("name %q: %v", n, err)
		}
		if len(n) > 64 {
			t.Fatalf("name too long: %q", n)
		}
		if seen[n] {
			t.Fatalf("duplicate %q", n)
		}
		seen[n] = true
	}
	// Deterministic order for the input cache.
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatal("names must be sorted")
		}
	}
	m := BuiltinManifest(deps)
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if m.Kind != ExtensionBuiltin || m.Contract != ExtensionContract {
		t.Fatalf("manifest kind/contract: %+v", m)
	}
	// Master-fast tools must be flagged.
	for _, want := range []string{"status", "clock", "get_tool_output"} {
		tool, err := reg.Get(want)
		if err != nil {
			t.Fatal(err)
		}
		if !tool.Definition().Fast {
			t.Fatalf("%s must be fast", want)
		}
	}
	for _, notfast := range []string{"read_file", "exec", "edit_file"} {
		tool, err := reg.Get(notfast)
		if err != nil {
			t.Fatal(err)
		}
		if tool.Definition().Fast {
			t.Fatalf("%s must not be fast", notfast)
		}
	}
}
