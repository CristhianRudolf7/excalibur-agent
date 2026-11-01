package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// wiredNames returns the registry names whose provider is wired: a
// StubTool with no provider reports "not wired" on every call.
func wiredNames(reg *tools.MapRegistry) map[string]bool {
	out := map[string]bool{}
	for _, name := range reg.Names() {
		t, err := reg.Get(name)
		if err != nil {
			continue
		}
		if st, ok := t.(*tools.StubTool); ok && st.Provider == nil {
			continue
		}
		out[name] = true
	}
	return out
}

// daemonCoreWired lists the Section 13.2 core tools the audit found
// unwired on the daemon path. Each must answer with its real provider.
var daemonCoreWired = []string{
	"leave_continuation_note", "task_db", "memory_search", "memory_write",
	"memory_propose", "history_search", "library_search", "doc_read",
	"doc_edit", "search_skills", "load_skill", "search_tools", "call_tool",
	"subagent", "ack_directive", "pin_observation",
}

// TestDaemonToolSurfaceMatchesEmbedded checks the daemon tool surface
// against the embedded surface: same names, same deferred split, and
// every tool wired on the embedded path is wired on the daemon.
func TestDaemonToolSurfaceMatchesEmbedded(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "parity.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	svc, err := Assemble(AssembleOptions{Store: orchStore, Orch: orch, Memory: memory.NewService(db)})
	if err != nil {
		t.Fatal(err)
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}

	daemonWired := wiredNames(svc.Tools)
	embWired := wiredNames(emb.Registry)
	for _, name := range emb.Registry.Names() {
		if _, err := svc.Tools.Get(name); err != nil {
			t.Errorf("embedded tool %q is missing on the daemon", name)
			continue
		}
		if embWired[name] && !daemonWired[name] {
			t.Errorf("tool %q is wired on the embedded path but not on the daemon", name)
		}
		if emb.Catalog.IsDeferred(name) != svc.Catalog.IsDeferred(name) {
			t.Errorf("tool %q deferred split differs between daemon and embedded", name)
		}
	}
	for _, name := range daemonCoreWired {
		if !daemonWired[name] {
			t.Errorf("core tool %q answers not wired on the daemon", name)
		}
	}
}

// Pinned surface and profile hashes: the embedded tool list (the exec
// and eval surface) and the frozen eval profile must not move because
// the daemon composition changed. A deliberate change updates these
// values in the same commit and says why.
const (
	embeddedToolsHash = "b9bedd828535af82488a5a7034279a376a3165377cdac3c87adddf7d7b4408eb"
	embeddedPrefix    = "3a3f4e015eab66ff5fd1696ded518a8762803a5787f4596c56bd72f9bef09fbb"
	evalProfileHash   = "a83cb917edc601f6b03c232a37b262ff61f72551bb9054ebf473a655a140b48d"
)

// TestEmbeddedSurfaceAndEvalProfileDoNotMove pins the embedded surface
// and the eval profile hash.
func TestEmbeddedSurfaceAndEvalProfileDoNotMove(t *testing.T) {
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	names := emb.Registry.Names()
	sort.Strings(names)
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	got := map[string]string{
		"names":   hex.EncodeToString(sum[:]),
		"prefix":  emb.Catalog.PrefixHash(),
		"profile": eval.DefaultEvalProfile().Hash(),
	}
	want := map[string]string{
		"names":   embeddedToolsHash,
		"prefix":  embeddedPrefix,
		"profile": evalProfileHash,
	}
	for k, v := range got {
		if v != want[k] {
			t.Errorf("%s hash moved: got %s, pinned %s", k, v, want[k])
		}
	}
}
