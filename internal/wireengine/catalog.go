package wireengine

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/pdf"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract/visionocr"
	"github.com/CristhianRudolf7/excalibur-agent/internal/library"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"

	"github.com/CristhianRudolf7/excalibur-agent/internal/codeintel"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// EmbeddedOptions tunes one embedded catalog build. Every field is
// optional; nil entries fall back to deterministic in-memory fakes so
// unit tests never need a daemon, a language server or the vault.
type EmbeddedOptions struct {
	Classroom   classroom.AgentService
	Reminders   *agenda.ReminderService
	Automations automations.Service

	RobotLab       *robotlab.Tool
	Whiteboard     *whiteboard.Service
	ConversationID string
	Profile        string
	// Memory binds document library reads in embedded runs.
	Memory *memory.Service
	// Media supplies real document backends; nil keeps honest unavailable behavior.
	Media *tools.MediaDeps
	// MemoryStore optionally constructs embedded library memory (never set for eval).
	MemoryStore *store.DB
	// Web is optional; nil preserves the deterministic memory backend.
	Web *tools.WebDeps
	// Leases is the workspace lease manager (Section 16.2). Nil
	// builds an idle manager.
	Leases *workspace.LeaseManager
	// History is the shared edit history (Section 13.7). Nil builds
	// an empty one.
	History *SharedHistory
	// Diagnostics feeds LSP diagnostics into edit outcomes
	// (Sections 13.4 step 6, 13.8). Nil appends nothing.
	Diagnostics tools.DiagnosticProvider
	// RunID stamps file versions and secret uses.
	RunID string
	// Project scopes vault resolution for delegate tasks.
	Project string
	// Delegated marks profile-delegate tasks: only entries scoped
	// to Project resolve (Section 3.6). The eval profile never
	// passes a vault at all.
	Delegated bool
	// Vault wires secret resolution and output redaction
	// (Section 20.1). Nil keeps pattern-only redaction.
	Vault *vault.Vault
	// Autonomous selects eval/exec output policy; assistant and daemon keep patterns.
	Autonomous bool
	// KnownSecrets holds provider and harness-injected credentials for this run.
	KnownSecrets [][]byte
	// Skills is the skill index backing search_skills/load_skill
	// (Section 12.4). Nil builds the embedded builtin index so
	// exec/eval runs always have skills instead of "not wired"
	// errors; pass Skills: &skills.Index{} nowhere — an explicitly
	// empty index still wires (and finds nothing).
	Skills *skills.Index
	// Surface carries prebuilt task-scoped stores for the embedded
	// adapters (task_db, subagent, ack_directive, history, pins,
	// notes). Nil builds task-scoped in-memory defaults.
	Surface *SurfaceStores
	// Tune overrides the tool-surface ablation switches. Nil reads
	// EXCALIBUR_TOOL_* over the all-ON default.
	Tune *tools.SurfaceTuning
	// Codeintel backs the lsp and code_graph deferred tools.
	// Nil leaves them unwired (their stubs report the owner).
	Codeintel *codeintel.Deps
	// Workdir is the run's project directory. Relative paths in the file
	// tools resolve against it (EXCALIBUR_RUN_WORKDIR). Empty keeps the
	// process working directory.
	Workdir string
	// Worktrees and Queue back the deferred workspace tools. Nil
	// leaves those tools reporting their missing collaborator.
	Worktrees *workspace.WorktreeManager
	Queue     *workspace.IntegrationQueue
	// Snapshots backs workspace_snapshot. Nil leaves it reporting.
	Snapshots *workspace.SnapshotManager
}

// Embedded is one wired embedded catalog: the registry, the deferred
// catalog with its fixed prefix, and the pipeline executor with vault
// redaction installed. Surface holds the task-scoped stores when the
// embedded adapters are wired (nil when WireEmbedded is off).
// Sessions is the shared terminal session manager behind exec/exec_io:
// background sessions detached through it survive the run (W10).
type Embedded struct {
	Library  *library.Ingester
	Memory   *memory.Service
	Registry *tools.MapRegistry
	Catalog  *tools.DeferredCatalog
	Exec     *tools.Executor
	Files    *tools.FileDeps
	History  *SharedHistory
	Leases   *workspace.LeaseManager
	Surface  *SurfaceStores
	Sessions *tools.SessionManager
	Prefix   string
}

// BuildEmbedded wires the full embedded tool surface through the
// shared Registry interface with no privileged path (Section 14.12):
// built-ins with real leases, shared history and LSP diagnostics,
// search_tools/call_tool over the deferred catalog, the deferred
// harness families (codeintel, workspace, documents/media, web,
// personal, packages) marked deferred without touching the fixed
// prefix, and the skill tools bound to the index. The executor
// redacts every output through the vault (Section 20.1 step 5).
func BuildEmbedded(opts EmbeddedOptions) (*Embedded, error) {
	leases := opts.Leases
	if leases == nil {
		leases = workspace.NewLeaseManager()
	}
	hist := opts.History
	if hist == nil {
		hist = NewSharedHistory()
	}
	files := &tools.FileDeps{
		Fingerprints: tools.NewFingerprintStore(),
		Leases:       &WorkspaceLeases{M: leases},
		History:      hist,
		Diagnostics:  opts.Diagnostics,
		RunID:        opts.RunID,
		Workdir:      opts.Workdir,
	}
	reg := tools.NewMapRegistry()
	sess := tools.NewSessionManager(tools.TerminalDeps{})
	if err := tools.RegisterBuiltins(reg, &tools.BuiltinDeps{Files: files, Sessions: sess}); err != nil {
		return nil, err
	}
	// Point the built-in exec tool at the vault for env resolution.
	// The registry holds the same *FileDeps pointers the tools use,
	// so wiring here reaches the live tool instances.
	if ex, err := reg.Get("exec"); err == nil {
		if et, ok := ex.(*tools.ExecTool); ok {
			et.SecretResolver = ExecSecretResolver(opts.Vault, opts.RunID, opts.Project, opts.Delegated, false)
			et.RunID = opts.RunID
			et.Project = opts.Project
		}
	}
	if opts.Profile != "eval" {
		if err := automations.WireTool(reg, opts.Automations); err != nil {
			return nil, err
		}
	}
	cat := tools.NewDeferredCatalog(reg)
	prefix := cat.PrefixHash()
	exec := tools.NewExecutor(reg)
	exec.Redact = ProfileRedactor(opts.Vault, opts.Autonomous, opts.KnownSecrets)
	// The pipeline stores truncated full outputs in exec.Outputs, so
	// get_tool_output must read from that same store: otherwise the
	// blob ref in the truncation marker resolves nowhere and the
	// model re-runs the command instead of paging.
	if gto, err := reg.Get("get_tool_output"); err == nil {
		if gt, ok := gto.(*tools.GetToolOutputTool); ok {
			gt.Outputs = exec.Outputs
		}
	}
	// Elided exec output is stored in the same store (EXCALIBUR_EXEC_ELIDE_REF).
	if ex, err := reg.Get("exec"); err == nil {
		if et, ok := ex.(*tools.ExecTool); ok {
			et.Outputs = exec.Outputs
		}
	}
	tools.WireSearchCall(reg, cat, exec)
	tools.WireWebTools(reg, cat, opts.Web)
	// Always discoverable outside eval, even when disabled or not installed.
	if opts.Profile != "eval" {
		if err := cat.RegisterDeferred(&classroom.Tool{Service: opts.Classroom}); err != nil {
			return nil, err
		}
		lab := &robotlab.Tool{}
		if opts.RobotLab != nil {
			copied := *opts.RobotLab
			lab = &copied
		}
		if lab.Vision == nil {
			loop := &robotlab.VisionLoop{Sidecar: lab.Sidecar, Publisher: lab.Publisher}
			if opts.Media != nil {
				loop.Asker, _ = opts.Media.Vision.(robotlab.VisionAsker)
			}
			lab.Vision = loop
		}
		if err := cat.RegisterDeferred(lab); err != nil {
			return nil, err
		}
	}
	if whiteboard.Enabled() && opts.Profile != "eval" {
		board := opts.Whiteboard
		if board == nil {
			board = whiteboard.NewService(whiteboard.NewStore(opts.MemoryStore), nil)
		}
		conv := opts.ConversationID
		if conv == "" {
			conv = opts.RunID
		}
		if err := cat.RegisterDeferred(&whiteboard.Tool{Service: board, ConversationID: conv}); err != nil {
			return nil, err
		}
	}
	// One memory service serves library tools and memory tools: an
	// injected service wins; otherwise an optional embedded store builds
	// one with the configured vision extractor (never for eval).
	memSvc := opts.Memory
	if memSvc == nil && opts.MemoryStore != nil {
		memSvc = memory.NewService(opts.MemoryStore, VisionMemoryOption(opts.Media))
	}
	opts.Memory = memSvc
	if err := memory.RegisterLibraryTools(reg, cat, opts.Memory); err != nil {
		return nil, err
	}
	if opts.Memory != nil {
		memory.WireMemoryTools(reg, opts.Memory)
	}

	media := &tools.MediaDeps{}
	if opts.Media != nil {
		copied := *opts.Media
		media = &copied
	}
	if media.Workdir == "" {
		media.Workdir = opts.Workdir
	}
	if media.PDFSecret == nil {
		media.PDFSecret = PDFSecretResolver(opts.Vault, opts.RunID, opts.Project, opts.Delegated)
	}
	if media.PDFText == nil || media.PDFRender == nil {
		reader := pdf.New(pdf.Options{})
		if media.PDFText == nil {
			media.PDFText = reader
		}
		if media.PDFRender == nil {
			media.PDFRender = reader
		}
	}
	if err := tools.RegisterDeferredMedia(reg, cat, media); err != nil {
		return nil, err
	}
	memory.WireLibraryIngestTool(reg, opts.Memory)
	cat.MarkDeferred(memory.ToolLibraryIngest)
	if _, err := tools.RegisterPersonalDeferred(reg, cat, tools.PersonalFakes()); err != nil {
		return nil, err
	}
	tools.WirePackageTool(reg, cat, tools.NewPackageManager(reg, cat, exec))
	// The narrow registry wires only reminders; settings keeps its owner.
	if reminder, err := reg.Get("reminders"); err == nil {
		reminderRegistry := tools.NewMapRegistry()
		if err := reminderRegistry.Register(reminder); err != nil {
			return nil, err
		}
		tools.WirePersonalCore(reminderRegistry, nil, nil)
	}
	if opts.Reminders != nil {
		opts.Reminders.WireTool(reg)
	}

	// Codeintel deferred tools (Section 13.3 table). They register
	// even without a graph opener or language servers: search finds
	// them and invoking without collaborators reports the missing
	// owner instead of an unknown tool.
	codeps := opts.Codeintel
	if codeps == nil {
		codeps = &codeintel.Deps{}
	}
	if err := codeintel.Register(reg, codeps); err != nil {
		return nil, err
	}
	for _, name := range []string{codeintel.CodeGraphToolName, codeintel.LSPToolName} {
		cat.MarkDeferred(name)
	}
	if opts.Profile != "eval" {
		if err := cat.RegisterDeferred(&codeintel.CodeDiagramTool{Media: media, RunID: opts.RunID}); err != nil {
			return nil, err
		}
	}

	// Workspace deferred tools (Sections 13.7, 16.4).
	wdeps := &workspace.ToolDeps{
		History: hist.Work, Snapshots: opts.Snapshots,
		Worktrees: opts.Worktrees, Queue: opts.Queue, RunID: opts.RunID,
	}
	if err := workspace.RegisterWorkspaceTools(reg, wdeps); err != nil {
		return nil, err
	}
	for _, name := range workspace.WorkspaceToolNames() {
		cat.MarkDeferred(name)
	}

	// Skill tools (Section 12.4): search_skills and load_skill live
	// in the core prefix as stubs; wiring fills their providers so
	// the prefix hash never moves and load_skill resolves the skill's
	// deferred tools up front (Section 13.3). A nil index falls back
	// to the embedded builtin catalog so exec/eval always have
	// skills; disable the fallback with the WireEmbedded switch.
	tune := tools.DefaultSurfaceTuning()
	if opts.Tune != nil {
		tune = *opts.Tune
	} else {
		tune = tools.TuningFromEnv(tune)
	}
	exec.Tune = &tune
	ix := opts.Skills
	if ix == nil && tune.WireEmbedded {
		if opts.Profile == "eval" {
			ix = skills.NewEvalBuiltinIndex()
			_ = ix.Scan()
		} else {
			ix = DefaultSkillsIndex()
		}
	}
	WireSkills(reg, cat, ix)

	// Task-scoped embedded surface (Section 13.2 core tools owned
	// elsewhere): task_db, subagent, ack_directive, history_search,
	// pin_observation and leave_continuation_note run against
	// run-local stores instead of reporting "not wired".
	var surface *SurfaceStores
	if opts.Surface != nil {
		surface = opts.Surface
	} else if tune.WireEmbedded {
		surface = EnsureSurfaceStores(opts.RunID)
	}
	if surface != nil {
		WireEmbeddedSurface(reg, surface)
	}

	if got := cat.PrefixHash(); got != prefix {
		return nil, prefixChangedError(prefix, got)
	}
	var worker *library.Ingester
	if memSvc != nil && opts.MemoryStore != nil && library.Enabled() {
		registry := docextract.NewRegistry()
		if err := library.RegisterDefaultExtractors(registry); err != nil {
			return nil, err
		}
		var ocr docextract.OCR
		if backend, ok := media.Vision.(visionocr.Backend); ok {
			ocr = visionocr.New(backend)
		}
		var err error
		worker, err = library.New(library.Options{Store: memSvc, Blobs: opts.MemoryStore.Blob(), Queue: library.NewSQLQueue(opts.MemoryStore), Registry: registry, OCR: ocr, Renderer: pdf.New(pdf.Options{}), Policy: library.PDFPolicy{}, Proposer: memSvc})
		if err != nil {
			return nil, err
		}
		if err = worker.Start(context.Background()); err != nil {
			return nil, err
		}
		memSvc.BindLibraryIngester(worker)
	}
	return &Embedded{
		Library: worker, Memory: memSvc,
		Registry: reg, Catalog: cat, Exec: exec,
		Files: files, History: hist, Leases: leases, Surface: surface, Sessions: sess, Prefix: prefix,
	}, nil
}

// WireSkills connects the real search_skills/load_skill providers to
// the core stubs registered by RegisterBuiltins, or registers them
// when the registry lacks them. The catalog prefix (core names and
// hash) never changes: wiring only fills providers. A nil index
// leaves the stubs reporting their owner.
func WireSkills(reg *tools.MapRegistry, cat *tools.DeferredCatalog, ix *skills.Index) {
	if ix == nil {
		return
	}
	resolve := func(names []string) []tools.Definition {
		if cat == nil {
			return nil
		}
		defs, err := cat.ResolveSkillTools(names)
		if err != nil {
			return nil
		}
		return defs
	}
	for _, real := range []tools.Tool{
		&skills.SearchSkillsTool{Index: ix},
		&skills.LoadSkillTool{Index: ix, ResolveTools: resolve},
	} {
		existing, err := reg.Get(real.Definition().Name)
		if err != nil {
			_ = reg.Register(real)
			continue
		}
		if stub, ok := existing.(*tools.StubTool); ok {
			provider := real
			stub.Provider = func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
				return provider.Invoke(ctx, inv)
			}
		}
	}
}

func prefixChangedError(before, after string) error {
	return fmt.Errorf("wireengine: deferred wiring changed the fixed prefix hash %s -> %s", before, after)
}

// Close joins background ingestion before its persistent store is closed.
func (e *Embedded) Close() error {
	if e != nil && e.Library != nil {
		return e.Library.Close()
	}
	return nil
}
