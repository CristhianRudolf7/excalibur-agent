package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newFileTestDeps() *FileDeps {
	return &FileDeps{
		Fingerprints: NewFingerprintStore(),
		Leases:       NewMemoryLeases(),
		History:      NewMemoryHistory(),
		RunID:        "run_test",
		Now:          time.Now,
	}
}

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFileWindowAndFP(t *testing.T) {
	var lines []string
	for i := 1; i <= 500; i++ {
		lines = append(lines, "line")
	}
	p := writeTmp(t, strings.Join(lines, "\n")+"\n")
	deps := newFileTestDeps()
	tool := &ReadFileTool{Deps: deps}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + quote(p) + `,"offset":10,"limit":5}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	if !strings.Contains(res.Output, "fp:") || !strings.Contains(res.Output, "10  ") {
		t.Fatalf("numbered window with fp: %q", res.Output)
	}
	fp, ok := deps.Fingerprints.LastRead(p)
	if !ok || fp == "" {
		t.Fatal("read must record fingerprint")
	}
	keys, _ := tool.ResourceKeys(json.RawMessage(`{"path":` + quote(p) + `}`))
	if len(keys) != 1 || !strings.HasPrefix(keys[0], "file:") {
		t.Fatalf("resource keys: %v", keys)
	}
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func TestWriteFileAtomicAndHistory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.txt")
	deps := newFileTestDeps()
	tool := &WriteFileTool{Deps: deps}
	raw, _ := json.Marshal(map[string]string{"path": p, "content": "hello"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "hello" {
		t.Fatalf("content: %q", data)
	}
	vers := deps.History.Versions(p)
	if len(vers) != 1 || vers[0].Preimage == "" || vers[0].Postimage == "" {
		t.Fatalf("history must record pre/postimage: %+v", vers)
	}
	if _, ok := deps.History.Blob(vers[0].Preimage); !ok {
		t.Fatal("preimage blob must exist for crash recovery")
	}
}

func TestWriteFileFPMismatch(t *testing.T) {
	p := writeTmp(t, "v1")
	deps := newFileTestDeps()
	rd := &ReadFileTool{Deps: deps}
	if _, err := rd.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":` + quote(p) + `}`)}); err != nil {
		t.Fatal(err)
	}
	// External change between read and write.
	if err := os.WriteFile(p, []byte("v2-external"), 0o644); err != nil {
		t.Fatal(err)
	}
	wr := &WriteFileTool{Deps: deps}
	raw, _ := json.Marshal(map[string]string{"path": p, "content": "mine"})
	res, _ := wr.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "changed since read") {
		t.Fatalf("must refuse with diff, got %+v", res)
	}
	if !strings.Contains(res.Output, "current") {
		t.Fatalf("must include current region diff, got %q", res.Output)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "v2-external" {
		t.Fatal("refused edit must not touch the file")
	}
}

func TestMismatchEscalationAfter3(t *testing.T) {
	p := writeTmp(t, "v1")
	store := NewFingerprintStore()
	store.NoteRead(p, "stale")
	for i := 0; i < 2; i++ {
		mi := store.Check(p, "stale")
		if mi.ShouldScale {
			t.Fatal("must escalate only after 3")
		}
	}
	mi := store.Check(p, "stale")
	if !mi.ShouldScale || mi.Mismatches != 3 {
		t.Fatalf("third mismatch escalates: %+v", mi)
	}
}

func TestEditThreeLevels(t *testing.T) {
	deps := newFileTestDeps()
	// Level 2: typographic quotes.
	p := writeTmp(t, "it\u2019s here\n")
	tool := &EditFileTool{Deps: deps}
	raw, _ := json.Marshal(map[string]string{"path": p, "old_text": "it's here", "new_text": "done"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if res.Status != StatusOK {
		t.Fatalf("normalized match must apply: %+v", res)
	}
	// Level 3: trailing whitespace.
	p2 := writeTmp(t, "key: value   \n")
	raw2, _ := json.Marshal(map[string]string{"path": p2, "old_text": "key: value", "new_text": "key: other"})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw2})
	if res2.Status != StatusOK {
		t.Fatalf("trimmed match must apply: %+v", res2)
	}
	// Ambiguous: multiple matches report lines.
	p3 := writeTmp(t, "dup\nmid\ndup\n")
	raw3, _ := json.Marshal(map[string]string{"path": p3, "old_text": "dup", "new_text": "x"})
	res3, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw3})
	if res3.Status != StatusToolError || !strings.Contains(res3.Output, "matches 2 places") || !strings.Contains(res3.Output, "(lines 1, 3)") {
		t.Fatalf("must report line numbers: %+v", res3)
	}
}

func TestEditDiagnosticsAppended(t *testing.T) {
	p := writeTmp(t, "a\n")
	deps := newFileTestDeps()
	deps.Diagnostics = scriptedDiagnostics{msg: "line 1: unused"}
	tool := &EditFileTool{Deps: deps}
	raw, _ := json.Marshal(map[string]string{"path": p, "old_text": "a", "new_text": "b"})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if !strings.Contains(res.Output, "diagnostics") || !strings.Contains(res.Output, "unused") {
		t.Fatalf("LSP diagnostics must appear in result: %q", res.Output)
	}
}

func TestApplyPatchMultiFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(a, []byte("hello world\n"), 0o644)
	deps := newFileTestDeps()
	tool := &ApplyPatchTool{Deps: deps}
	args, _ := json.Marshal(map[string]any{
		"ops": []any{
			map[string]any{"path": a, "op": "update", "old_text": "world", "new_text": "there"},
			map[string]any{"path": b, "op": "add", "new_text": "new file"},
		},
	})
	res, _ := tool.Invoke(context.Background(), Invocation{Arguments: args})
	if res.Status != StatusOK {
		t.Fatalf("got %+v", res)
	}
	da, _ := os.ReadFile(a)
	db, _ := os.ReadFile(b)
	if string(da) != "hello there\n" || string(db) != "new file" {
		t.Fatalf("patched: %q %q", da, db)
	}
	// Add over existing must fail without partial apply.
	args2, _ := json.Marshal(map[string]any{"ops": []any{map[string]any{"path": a, "op": "add", "new_text": "x"}}})
	res2, _ := tool.Invoke(context.Background(), Invocation{Arguments: args2})
	if res2.Status == StatusOK {
		t.Fatal("add over existing must fail")
	}
}

func TestFindMatchLevelsDirect(t *testing.T) {
	if off, lvl, _ := FindMatch("it\u2019s", "it's"); off < 0 || lvl != MatchNormalized {
		t.Fatalf("level2: %d %d", off, lvl)
	}
	if off, lvl, _ := FindMatch("a   \n", "a"); off < 0 || (lvl != MatchExact && lvl != MatchTrimmed) {
		t.Fatalf("levels: %d %d", off, lvl)
	}
	if _, _, amb := FindMatch("x\nx\n", "x"); len(amb) != 2 {
		t.Fatalf("ambiguous lines: %v", amb)
	}
}

func TestLeasesSerializeAndExpire(t *testing.T) {
	m := NewMemoryLeases()
	m.waitMax = 50 * time.Millisecond
	rel, err := m.Acquire("/f", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Acquire("/f", "b"); err == nil {
		t.Fatal("second holder must wait then fail")
	} else if !strings.Contains(err.Error(), "leased by") {
		t.Fatalf("must name holder: %v", err)
	}
	rel()
	if _, err := m.Acquire("/f", "b"); err != nil {
		t.Fatalf("released lease must grant: %v", err)
	}
}

func TestCompactDiffShowsRegion(t *testing.T) {
	d := CompactDiff("a\nb\nc\n", "a\nB\nc\n", "f")
	if !strings.Contains(d, "-") || !strings.Contains(d, "+") {
		t.Fatalf("diff: %q", d)
	}
}

func TestFingerprintCheckNilExpected(t *testing.T) {
	p := writeTmp(t, "x")
	s := NewFingerprintStore()
	if mi := s.Check(p, ""); mi != nil {
		t.Fatal("empty expected means no constraint")
	}
}
