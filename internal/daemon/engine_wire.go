// Production composition of the daemon step engine (Sections 7.3,
// 13.1, 19.2). The engine takes its model endpoint and its request
// builder from the daemon configuration, and its tool surface from the
// same composition root as the embedded path: a daemon turn can use
// every tool an embedded run can (wireengine.BuildEmbedded), plus the
// memory tools that need the daemon's memory service.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robot"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// errNoModel is the step failure of a daemon without a configured
// provider: runs fail with this message instead of dereferencing a
// missing endpoint.
var errNoModel = errors.New("daemon: no model provider configured; the run cannot take a step")

// unconfiguredModels answers every model call with errNoModel. It
// keeps the engine total when the operator has not configured a
// provider (the deterministic intake path still runs).
type unconfiguredModels struct{}

// Chat implements engine.ModelEndpoint.
func (unconfiguredModels) Chat(context.Context, types.ModelRole, providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	return providers.ChatResponse{}, models.CallMeta{}, errNoModel
}

// engineWiring carries the collaborators of one daemon engine.
type engineWiring struct {
	Classroom  classroom.AgentService
	Robot      *robot.Service
	Workdir    string
	History    *wireengine.SharedHistory
	Leases     *workspace.LeaseManager
	Whiteboard *whiteboard.Service
	Media      *tools.MediaDeps
	BrowserWeb *wireengine.BrowserWeb
	// Caps are the native capabilities whose tools join the registry.
	Caps *capabilities.Registry
	// Models is the configured model endpoint; nil means unconfigured.
	Models engine.ModelEndpoint
	// ModelID selects the L0 prompt variant and the limits row (19.1).
	ModelID string
	// Vault binds secret resolution for exec and output redaction
	// (Section 20.1). Nil keeps pattern-only redaction.
	Vault *vault.Vault
	// Memory serves memory_*, library_search and doc_* (Section 10).
	// Nil leaves those tools reporting their missing owner.
	Memory *memory.Service
	// Skills backs search_skills and load_skill (Section 12.4). Nil
	// falls back to the embedded builtin catalog, as the embedded path.
	Skills *skills.Index
	// Orch, OrchStore and Root back subagent and ack_directive
	// (Section 15): the daemon's own orchestrator, with Root as the
	// master the delegated work reports to. Any nil leaves those two
	// tools on private task-scoped stores.
	Orch      *orchestrator.Orchestrator
	OrchStore *orchestrator.MemoryStore
	Root      types.AgentID
}

// daemonTools is the composed daemon tool surface: the engine runs
// through Exec and resolves definitions from Registry.
type daemonTools struct {
	SharedHistory *wireengine.SharedHistory
	Leases        *workspace.LeaseManager
	History       *workspace.History
	Sources       tools.SourceRecorder
	Registry      *tools.MapRegistry
	Catalog       *tools.DeferredCatalog
	Exec          *tools.Executor
}

// buildEngine assembles the daemon step engine over the same registry
// the embedded path builds (wireengine.BuildEmbedded), with the daemon
// memory service attached and the capability tools added.
func buildEngine(w engineWiring) (*engine.Engine, *daemonTools, error) {
	var surface *wireengine.SurfaceStores
	if w.Orch != nil && w.OrchStore != nil {
		surface = &wireengine.SurfaceStores{
			Orch: w.Orch, OrchStore: w.OrchStore, Root: w.Root, Scope: types.GeneralScope(),
		}
	}
	var web *tools.WebDeps
	if w.BrowserWeb != nil {
		web = w.BrowserWeb.Web
	}
	// Keep source attribution separate from tool definitions and eval pins.
	if web == nil {
		web = &tools.WebDeps{}
	} else {
		copy := *web
		web = &copy
	}
	if web.Sources == nil {
		web.Sources = &tools.MemorySourceRecorder{}
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{
		Workdir: w.Workdir, History: w.History, Leases: w.Leases,
		Whiteboard: w.Whiteboard, Classroom: w.Classroom,
		Web:     web,
		Vault:   w.Vault,
		Media:   w.Media,
		Skills:  w.Skills,
		Surface: surface,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("daemon: build tool surface: %w", err)
	}
	if w.Robot != nil {
		if err := emb.Catalog.RegisterDeferred(&robot.Tool{Service: w.Robot}); err != nil {
			return nil, nil, err
		}
	}
	// Daemon delegation must never use the embedded record-only adapter,
	// including when the authoritative orchestrator store is SQL-backed.
	if w.Orch != nil {
		emb.Registry.Unregister("subagent")
		if err := emb.Registry.Register(&orchestrator.SubagentTool{Orch: w.Orch}); err != nil {
			return nil, nil, err
		}
	}
	if w.Memory != nil {
		memory.WireMemoryTools(emb.Registry, w.Memory)
	}
	if w.Caps != nil {
		for _, id := range w.Caps.IDs() {
			if c, ok := w.Caps.Lookup(id); ok {
				if id == "browser" {
					if err := wireengine.WireBrowser(emb, c); err != nil {
						return nil, nil, err
					}
					continue
				}
				if ts, err := tools.ToolsFromCapability(capabilityInvoker{id: id, cap: c}); err == nil {
					for _, t := range ts {
						_ = emb.Registry.Register(t)
					}
				}
			}
		}
	}
	dt := &daemonTools{SharedHistory: emb.History, Leases: emb.Leases, History: emb.History.Work, Sources: web.Sources, Registry: emb.Registry, Catalog: emb.Catalog, Exec: emb.Exec}
	var endpoint engine.ModelEndpoint = unconfiguredModels{}
	if w.Models != nil {
		endpoint = w.Models
	}
	return engine.New(engine.Deps{
		Models:  endpoint,
		Context: daemonRequestBuilder(emb.Registry, w.ModelID),
		Tools:   &daemonToolRunner{ex: emb.Exec, reg: emb.Registry},
		Gate:    engine.GateFunc(func(*engine.Run) tasks.GateReport { return tasks.GateReport{} }),
		Clock:   time.Now,
	}), dt, nil
}

// daemonRequestBuilder compiles one model request per step over the
// registry the engine executes. Daemon runs carry the role child and
// the harness release name in the stable prefix (Section 11.1).
func daemonRequestBuilder(reg *tools.MapRegistry, modelID string) engine.RequestBuilder {
	toolDefs := func(*engine.Run) []providers.ToolDefinition {
		var out []providers.ToolDefinition
		for _, name := range reg.Names() {
			t, err := reg.Get(name)
			if err != nil {
				continue
			}
			def := t.Definition()
			out = append(out, providers.ToolDefinition{Name: def.Name, Description: def.Description, Schema: def.InputSchema})
		}
		return out
	}
	snapshot := func(context.Context, *engine.Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{
			Role:           "child",
			HarnessVersion: "daemon",
			Volatile:       ctxcompiler.Volatile{Now: time.Now().UTC()},
		}, nil
	}
	return &engine.DefaultBuilder{
		Config:   ctxcompiler.DefaultConfig(),
		Counter:  ctxcompiler.NewEstimator(0),
		Snapshot: snapshot,
		ToolDefs: toolDefs,
		ModelID:  modelID,
	}
}
