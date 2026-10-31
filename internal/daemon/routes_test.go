package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

func wiredBackend(t *testing.T) (*DaemonBackend, *Services, func()) {
	t.Helper()
	ctx := context.Background()
	bus := telemetry.NewBus(nil)
	orchStore := orchestrator.NewMemoryStore()
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchStore, orchestrator.NoopJournal{})
	ctrls := &OrchControl{Orch: orch, Store: orchStore}
	decider := NewModelDecider(models.NewFakeDecider(0.95), []string{"general"})
	m := master.New(master.Config{}, master.NewMemStore(), master.NewLexicon(), decider, ctrls, nil, nil)
	backend := NewDaemonBackend(m, ctrls, bus)

	// Memory over an isolated database.
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "mem.db"), opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	memSvc := memory.NewService(db,
		memory.WithEmbedder(memory.NewFakeEmbedder("fake-test", 64)),
		memory.WithScopes("general"))

	// Vault over a fake keyring.
	v, err := vault.Open(ctx, filepath.Join(t.TempDir(), "vault.db"), vault.Options{Keyring: platformfake.NewKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatal(err)
	}

	// Projects with one real temp root.
	reg := workspace.NewRegistry()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skip.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := reg.Register(workspace.Project{Name: "demo", Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}

	ix := skills.NewIndex(skills.Roots{}, skills.BuiltinFS())
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}

	tasks, err := NewTaskService(orch, orchStore)
	if err != nil {
		t.Fatal(err)
	}
	backend.AttachServices(BackendServices{
		Tasks:    tasks,
		Caps:     NewCapabilityRoutes(capabilities.DefaultRegistry()),
		Memory:   memSvc,
		Skills:   ix,
		Vault:    v,
		Projects: reg,
		History:  workspace.NewHistory(),
		Config:   &FileConfig{UserPath: filepath.Join(t.TempDir(), "config.toml")},
		Metrics:  &MetricsStore{},
	})
	cleanup := func() {
		_ = db.Close()
		_ = v.Close()
	}
	_ = proj
	return backend, nil, cleanup
}

// TestGateFamiliesRealRouting exercises every passthrough family
// through the real daemon backends: nothing falls back to the
// embedded stubs when services attach.
func TestGateFamiliesRealRouting(t *testing.T) {
	backend, _, cleanup := wiredBackend(t)
	defer cleanup()
	ctx := context.Background()

	// task.start/status/result through orchestrator runs.
	start, err := backend.TaskOp(ctx, api.MethodTaskStart, json.RawMessage(`{"request":"write the tests"}`))
	if err != nil {
		t.Fatalf("task.start: %v", err)
	}
	taskID := start.(api.TaskStartResult).TaskID
	if taskID == "" {
		t.Fatal("empty task id")
	}
	st, err := backend.TaskOp(ctx, api.MethodTaskStatus, json.RawMessage(`{"task_id":"`+string(taskID)+`"}`))
	if err != nil {
		t.Fatalf("task.status: %v", err)
	}
	if st.(map[string]any)["task_id"] != string(taskID) {
		t.Fatalf("status for wrong task: %+v", st)
	}
	res, err := backend.TaskOp(ctx, api.MethodTaskResult, json.RawMessage(`{"task_id":"`+string(taskID)+`"}`))
	if err != nil {
		t.Fatalf("task.result: %v", err)
	}
	if res.(map[string]any)["done"] == nil {
		t.Fatalf("result has no done flag: %+v", res)
	}

	// Unknown tasks stay unknown-target, not silent success.
	if _, err := backend.TaskOp(ctx, api.MethodTaskStatus, json.RawMessage(`{"task_id":"T_nonexistent"}`)); err == nil {
		t.Fatal("unknown task must fail")
	}

	// memory.query/correct/provenance in the general scope.
	correct, err := backend.Passthrough(ctx, api.MethodMemoryCorrect,
		json.RawMessage(`{"statement":"Ana prefers small commits","scopes":["general"]}`))
	if err != nil {
		t.Fatalf("memory.correct: %v", err)
	}
	ids := correct.(map[string]any)["ids"].([]string)
	if len(ids) == 0 {
		t.Fatal("memory.correct admitted nothing")
	}
	query, err := backend.Passthrough(ctx, api.MethodMemoryQuery,
		json.RawMessage(`{"query":"what does Ana prefer","scopes":["general"]}`))
	if err != nil {
		t.Fatalf("memory.query: %v", err)
	}
	if query == nil {
		t.Fatal("memory.query returned nil")
	}
	prov, err := backend.Passthrough(ctx, api.MethodMemoryProvenance,
		json.RawMessage(`{"id":"`+ids[0]+`","scopes":["general"]}`))
	if err != nil {
		t.Fatalf("memory.provenance: %v", err)
	}
	if prov == nil {
		t.Fatal("memory.provenance returned nil")
	}

	// skills.search/load over the builtin index.
	search, err := backend.Passthrough(ctx, api.MethodSkillsSearch,
		json.RawMessage(`{"query":"write tests","limit":3}`))
	if err != nil {
		t.Fatalf("skills.search: %v", err)
	}
	found := search.(map[string]any)["skills"].([]skills.Hit)
	if len(found) == 0 {
		t.Fatal("skills.search found nothing in the builtin index")
	}
	load, err := backend.Passthrough(ctx, api.MethodSkillsLoad,
		json.RawMessage(`{"name":"`+found[0].Name+`"}`))
	if err != nil {
		t.Fatalf("skills.load: %v", err)
	}
	if load.(skills.Loaded).Body == "" {
		t.Fatal("skills.load returned an empty body")
	}

	// capability.list/invoke through the default registry.
	listed, err := backend.Passthrough(ctx, api.MethodCapabilityList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("capability.list: %v", err)
	}
	caps := listed.(map[string]any)["capabilities"].([]capabilities.Manifest)
	if len(caps) == 0 {
		t.Fatal("capability.list is empty")
	}
	invoked, err := backend.Passthrough(ctx, api.MethodCapabilityInvoke,
		json.RawMessage(`{"capability":"browser","operation":"browser_open_session","args":{}}`))
	if err != nil {
		t.Fatalf("capability.invoke: %v", err)
	}
	if invoked.(capabilities.TaskResult).TaskID == "" {
		t.Fatalf("browser open returned no task id: %+v", invoked)
	}

	// project.list/get over the workspace registry.
	pl, err := backend.Passthrough(ctx, api.MethodProjectList, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("project.list: %v", err)
	}
	projs := pl.(map[string]any)["projects"].([]*workspace.Project)
	if len(projs) != 1 {
		t.Fatalf("want 1 project, have %d", len(projs))
	}
	pg, err := backend.Passthrough(ctx, api.MethodProjectGet,
		json.RawMessage(`{"project_id":"`+string(projs[0].ID)+`"}`))
	if err != nil {
		t.Fatalf("project.get: %v", err)
	}
	if pg.(*workspace.Project).Name != "demo" {
		t.Fatalf("wrong project: %+v", pg)
	}

	// files.list honors .gitignore and paginates.
	fl, err := backend.FilesList(ctx, api.FilesListParams{ProjectID: string(projs[0].ID), Limit: 50})
	if err != nil {
		t.Fatalf("files.list: %v", err)
	}
	entries := fl.(map[string]any)["entries"].([]string)
	for _, e := range entries {
		if e == "skip.log" {
			t.Fatal("files.list leaked a gitignored file")
		}
	}
	foundNotes := false
	for _, e := range entries {
		if e == "notes.txt" {
			foundNotes = true
		}
	}
	if !foundNotes {
		t.Fatalf("files.list missed notes.txt: %v", entries)
	}

	// vault.create/list_metadata without values.
	if _, err := backend.Passthrough(ctx, api.MethodVaultCreate,
		json.RawMessage(`{"name":"api-token","type":"token","value":"s3cr3t"}`)); err != nil {
		t.Fatalf("vault.create: %v", err)
	}
	metas, err := backend.Passthrough(ctx, api.MethodVaultListMetadata, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("vault.list_metadata: %v", err)
	}
	if len(metas.(map[string]any)["entries"].([]vault.Metadata)) != 1 {
		t.Fatalf("vault metadata wrong: %+v", metas)
	}

	// question.answer resolves a confirmer question.
	qid := "confirm-w1-node-1"
	if err := backend.Ctrls.Store.SaveQuestion(types.Question{ID: qid, Content: "send?", State: "open"}); err != nil {
		t.Fatal(err)
	}
	answered, err := backend.Passthrough(ctx, api.MethodQuestionAnswer,
		json.RawMessage(`{"question_id":"`+qid+`","answer":"yes"}`))
	if err != nil {
		t.Fatalf("question.answer: %v", err)
	}
	if !answered.(map[string]any)["ok"].(bool) {
		t.Fatalf("question not acked: %+v", answered)
	}

	// metrics.query over the daemon store.
	backend.Services.Metrics.Record("T1", "demo", telemetry.Snapshot{})
	mq, err := backend.Passthrough(ctx, api.MethodMetricsQuery,
		json.RawMessage(`{"task_id":"T1"}`))
	if err != nil {
		t.Fatalf("metrics.query: %v", err)
	}
	if mq.(map[string]any)["count"].(int) != 1 {
		t.Fatalf("metrics.query count wrong: %+v", mq)
	}

	// config.get returns the effective configuration.
	cfg, err := backend.Passthrough(ctx, api.MethodConfigGet, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	if cfg == nil {
		t.Fatal("config.get returned nil")
	}

	// Unknown names still answer CodeMethodNotFound.
	if _, err := backend.Passthrough(ctx, "nope.unknown", json.RawMessage(`{}`)); err == nil {
		t.Fatal("unknown method must fail")
	}
}
