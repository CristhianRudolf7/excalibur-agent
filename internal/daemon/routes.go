package daemon

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/googleapi"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robot"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// BackendServices holds the real daemon-side backends attached to a
// DaemonBackend. Every hook is optional: a nil hook keeps the
// embedded MemoryBackend behavior so embedded profiles and unit
// tests never need the full stack.
type BackendServices struct {
	Google    googleapi.TokenProvider
	Agents    *agents.Registry
	Robot     *robot.Service
	RobotLab  *robotlab.Service
	FileBlobs gateway.BlobStore
	// WorkspaceRoots supplies trusted conversation-specific roots.
	WorkspaceRoots func(context.Context) []string
	Voice          *VoiceRoutes
	Sources        tools.SourceRecorder
	TaskQueries    *tasks.SQLStore
	Automations    automations.Service
	Reminders      *agenda.ReminderService
	Whiteboard     *whiteboard.Service
	Classroom      *classroom.Service
	Library        *LibraryRoutes

	// Principal serves the persisted local profile.
	Principal *PrincipalService
	// Tasks serves task.* through orchestrator runs (Section 3.6).
	Tasks *TaskService
	// Caps serves capability.* through the daemon registry (14.12).
	Caps *CapabilityRoutes
	// Memory serves memory.* in the allowed scopes (Section 10).
	Memory *memory.Service
	// Skills serves skills.* over the RAM index (Section 12).
	Skills *skills.Index
	// Vault serves vault.* without values (Section 20.1).
	Vault *vault.Vault
	// Projects serves project.* over the workspace registry (9.2).
	Projects *workspace.Registry
	// History backs the files.list agent_touched filter (13.7).
	History *workspace.History
	// Config serves config.* over the layered loader.
	Config *FileConfig
	// Metrics serves metrics.query (Section 22.2).
	Metrics *MetricsStore
	// Telemetry supplies durable usage snapshots.
	Telemetry *telemetry.SQLStore
}

// AttachServices installs the real backends on a DaemonBackend.
func (b *DaemonBackend) AttachServices(s BackendServices) {
	b.Services = (*daemonServices)(&s)
	if b.Master != nil && s.Principal != nil {
		b.Master.SetUserZoneResolver(func(ctx context.Context) (string, error) {
			p, err := s.Principal.Get(ctx)
			return p.Timezone, err
		})
	}
	if b.credentials == nil {
		b.credentials = newCredentialRegistry(b.Bus)
		b.credentials.vault = s.Vault
	}
}

// daemonServices is the resolved service set on the backend. It
// mirrors BackendServices field for field so the gate code reads one
// shape while external packages construct the exported one.
type daemonServices BackendServices

// TaskOp implements gateway.Backend: delegation through the daemon
// when the task service is attached, embedded fallback otherwise.
func (b *DaemonBackend) TaskOp(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if b.Services != nil && b.Services.Tasks != nil {
		return b.Services.Tasks.TaskOp(ctx, method, params)
	}
	return b.embed.TaskOp(ctx, method, params)
}

// FilesList implements gateway.Backend: project-scoped paginated
// listing when the project registry is attached, embedded empty page
// otherwise.
func (b *DaemonBackend) FilesList(ctx context.Context, params api.FilesListParams) (any, error) {
	if b.Services != nil && b.Services.Projects != nil {
		return ListProjectFiles(b.Services.Projects, b.Services.History, params)
	}
	return b.embed.FilesList(ctx, params)
}

// Passthrough implements gateway.Backend: every remaining family
// routes to its real backend when attached; unknown names still
// answer CodeMethodNotFound.
func (b *DaemonBackend) Passthrough(ctx context.Context, method string, params json.RawMessage) (any, error) {
	if strings.HasPrefix(method, "agents.") {
		return b.agentsOp(ctx, method, params)
	}
	if strings.HasPrefix(method, "credentials.") {
		return b.credentialsOp(ctx, method, params)
	}
	if strings.HasPrefix(method, "robot.") {
		if b.Services == nil {
			return (*robot.Service)(nil).Op(ctx, method, params)
		}
		return b.Services.Robot.Op(ctx, method, params)
	}
	if method == api.MethodArtifactsGet || method == api.MethodArtifactsList {
		return b.artifactOp(ctx, method, params)
	}
	if method == api.MethodFilesOpen {
		return b.filesOpen(ctx, params)
	}
	if method == api.MethodActivityList {
		return b.activityList(ctx, params)
	}
	if method == api.MethodVoiceStatus || method == api.MethodVoiceTranscribe || method == api.MethodVoiceSpeak {
		if b.Services == nil {
			return (*VoiceRoutes)(nil).Op(ctx, method, params)
		}
		return b.Services.Voice.Op(ctx, method, params)
	}

	if strings.HasPrefix(method, "automations.") {
		var service automations.Service
		if b.Services != nil {
			service = b.Services.Automations
		}
		return automationOp(ctx, service, method, params)
	}
	if strings.HasPrefix(method, "tasks.") || strings.HasPrefix(method, "reminders.") {
		if b.Services == nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: "persistent services unavailable"}
		}
		if strings.HasPrefix(method, "reminders.") {
			return b.Services.Reminders.Op(ctx, method, params)
		}
		if b.Services.TaskQueries == nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: "task store unavailable"}
		}
		if len(params) == 0 {
			params = json.RawMessage(`{}`)
		}
		switch method {
		case api.MethodTasksQuery:
			var p api.TasksQueryParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "invalid task query"}
			}
			return b.Services.TaskQueries.Query(ctx, p)
		case api.MethodTasksGet:
			var p struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(params, &p); err != nil || p.ID == "" {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "id is required"}
			}
			out, err := b.Services.TaskQueries.Detail(ctx, p.ID)
			if err != nil {
				if strings.Contains(err.Error(), "unknown task") {
					return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
				}
				return nil, err
			}
			return out, nil
		}
	}

	if strings.HasPrefix(method, "classroom.") {
		if b.Services == nil {
			return (*classroom.Service)(nil).Op(ctx, method, params)
		}
		return b.classroomOp(ctx, method, params)
	}
	if strings.HasPrefix(method, "robotlab.") {
		if b.Services == nil {
			return (*robotlab.Service)(nil).Op(ctx, method, params)
		}
		return b.Services.RobotLab.Op(ctx, method, params)
	}
	if strings.HasPrefix(method, "whiteboard.") {
		if b.Services == nil {
			return (*whiteboard.Service)(nil).Op(ctx, method, params)
		}
		return b.Services.Whiteboard.Op(ctx, method, params)
	}
	if strings.HasPrefix(method, "library.") {
		if b.Services == nil {
			return (*LibraryRoutes)(nil).Op(ctx, method, params)
		}
		return b.Services.Library.Op(ctx, method, params)
	}
	svc := b.Services
	if svc == nil {
		return b.embed.Passthrough(ctx, method, params)
	}
	switch method {
	case api.MethodGoogleStatus, api.MethodGoogleConnectStart, api.MethodGoogleConnectComplete, api.MethodGoogleDisconnect:
		return googleOp(ctx, svc.Google, method, params)
	case api.MethodPrincipalGet:
		if len(params) > 0 && !json.Valid(params) {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad principal.get params"}
		}
		if svc.Principal == nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: "profile store unavailable"}
		}
		return svc.Principal.Get(ctx)
	case api.MethodPrincipalUpdate:
		var p api.PrincipalUpdateParams
		if len(params) == 0 || json.Unmarshal(params, &p) != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad principal.update params"}
		}
		if svc.Principal == nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: "profile store unavailable"}
		}
		return svc.Principal.Update(ctx, p)
	case api.MethodProjectList, api.MethodProjectRegister, api.MethodProjectGet:
		if svc.Projects != nil {
			return ProjectOp(svc.Projects, method, params)
		}
	case api.MethodMemoryQuery, api.MethodMemoryCorrect, api.MethodMemoryProvenance:
		if svc.Memory != nil {
			return MemoryOp(ctx, svc.Memory, method, params)
		}
	case api.MethodSkillsGroups, api.MethodSkillsList, api.MethodSkillsGet, api.MethodSkillsUpdate, api.MethodSkillsSetState, api.MethodSkillsDelete,
		api.MethodSkillsSearch, api.MethodSkillsLoad, api.MethodSkillsInstall:
		if svc.Skills != nil {
			return SkillsOp(svc.Skills, method, params)
		}
	case api.MethodCapabilityRegister, api.MethodCapabilityInvoke, api.MethodCapabilityList:
		if svc.Caps != nil {
			return svc.Caps.Op(ctx, method, params)
		}
	case api.MethodVaultCreate, api.MethodVaultUpdate, api.MethodVaultListMetadata,
		api.MethodVaultLock, api.MethodVaultUnlock, api.MethodVaultDelete:
		if svc.Vault != nil {
			return VaultOp(ctx, svc.Vault, method, params)
		}
	case api.MethodQuestionAnswer:
		if b.Ctrls != nil {
			var p api.QuestionAnswerParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad question.answer params"}
			}
			if err := b.Ctrls.AnswerQuestion(p.QuestionID, p.Answer); err != nil {
				return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
			}
			return map[string]any{"question_id": p.QuestionID, "ok": true}, nil
		}
	case api.MethodUsageSummary:
		return usageSummaryOp(ctx, svc.Metrics, svc.Telemetry, params)
	case api.MethodMetricsQuery:
		if svc.Metrics != nil {
			var p api.MetricsQueryParams
			if len(params) > 0 {
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad metrics.query params"}
				}
			}
			return svc.Metrics.Query(p), nil
		}
	case api.MethodConfigGet, api.MethodConfigSet, api.MethodConfigList:
		if svc.Config != nil {
			return svc.Config.Op(method, params)
		}
	case api.MethodPlanApprove:
		if b.Ctrls != nil {
			var p api.PlanDecisionParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad plan.approve params"}
			}
			if err := b.Ctrls.ApprovePlan(p.PlanID); err != nil {
				return nil, &api.Error{Code: api.CodeConflict, Message: err.Error()}
			}
			return map[string]any{"plan_id": p.PlanID, "approved": true}, nil
		}
	case api.MethodPlanDeny:
		if b.Ctrls != nil {
			var p api.PlanDecisionParams
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad plan.deny params"}
			}
			if err := b.Ctrls.DenyPlan(p.PlanID, p.Reason); err != nil {
				return nil, &api.Error{Code: api.CodeConflict, Message: err.Error()}
			}
			return map[string]any{"plan_id": p.PlanID, "denied": true}, nil
		}
	}
	return b.embed.Passthrough(ctx, method, params)
}

// MemoryOp routes memory.query/correct/provenance to the memory
// service in the caller-supplied scopes (Section 9.5: the filter
// applies in SQL, never in the prompt).
func MemoryOp(ctx context.Context, svc *memory.Service, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodMemoryQuery:
		var p api.MemoryQueryParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad memory.query params"}
		}
		if len(p.Scopes) == 0 {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "memory.query needs scopes"}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = 8
		}
		ret, err := svc.Retrieve(ctx, memory.Query{Text: p.Query, Scopes: p.Scopes, K: limit})
		if err != nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
		}
		return ret, nil
	case api.MethodMemoryCorrect:
		var p struct {
			Statement string   `json:"statement"`
			Scopes    []string `json:"scopes"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad memory.correct params"}
		}
		if p.Statement == "" || len(p.Scopes) == 0 {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "memory.correct needs statement and scopes"}
		}
		// A client correction is authoritative: propose, then confirm
		// it in so the admitted memory carries the correction. Every
		// requested scope admits its own copy (Section 9.5).
		ids := make([]string, 0, len(p.Scopes))
		action := ""
		for _, scope := range p.Scopes {
			cand := memory.Candidate{Scope: scope, Statement: p.Statement}
			mem, err := svc.Propose(ctx, cand)
			if err != nil {
				return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
			}
			outcome, err := svc.Admit(ctx, cand)
			if err != nil {
				return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
			}
			ids = append(ids, string(mem.ID))
			action = outcome.Action
		}
		return map[string]any{"ids": ids, "outcome": action}, nil
	case api.MethodMemoryProvenance:
		var p struct {
			ID     string   `json:"id"`
			Scopes []string `json:"scopes"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad memory.provenance params"}
			}
		}
		if p.ID == "" || len(p.Scopes) == 0 {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "memory.provenance needs id and scopes"}
		}
		mem, err := svc.GetMemory(ctx, p.Scopes, p.ID)
		if err != nil {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
		}
		return mem, nil
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// SkillsOp routes skill discovery, loading and catalog management over the index.
func SkillsOp(ix *skills.Index, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodSkillsGroups, api.MethodSkillsList, api.MethodSkillsGet, api.MethodSkillsUpdate, api.MethodSkillsSetState, api.MethodSkillsDelete:
		return skillsCatalogOp(ix, method, params)
	case api.MethodSkillsSearch:
		var p api.SkillsSearchParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad skills.search params"}
			}
		}
		hits := ix.Search(p.Query, p.Group)
		limit := p.Limit
		if limit <= 0 || limit > len(hits) {
			limit = len(hits)
		}
		return map[string]any{"skills": hits[:limit]}, nil
	case api.MethodSkillsLoad:
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "skills.load needs name"}
		}
		loaded, err := ix.Load(p.Name)
		if err != nil {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
		}
		return loaded, nil
	case api.MethodSkillsInstall:
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "skills.install needs a staged directory; use excalibur ext or the installer surface"}
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// VaultOp routes vault.* to the secrets vault (Section 20.1). Values
// travel only in create/update calls, never in conversation messages,
// and list_metadata never returns them.
func VaultOp(ctx context.Context, v *vault.Vault, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodVaultCreate, api.MethodVaultUpdate:
		var p api.VaultEntryParams
		if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "vault entry needs a name"}
		}
		cp := vault.CreateParams{
			Name: p.Name, Type: p.Type, Value: []byte(p.Value),
			Metadata: p.Metadata, Scopes: p.Scopes, AllowedTools: p.AllowedTools,
		}
		if method == api.MethodVaultCreate {
			meta, err := v.Create(ctx, cp)
			if err != nil {
				return nil, vaultErr(err)
			}
			return meta, nil
		}
		meta, err := v.Update(ctx, cp)
		if err != nil {
			return nil, vaultErr(err)
		}
		return meta, nil
	case api.MethodVaultListMetadata:
		metas, err := v.ListMetadata(ctx)
		if err != nil {
			return nil, vaultErr(err)
		}
		return map[string]any{"entries": metas, "locked": v.IsLocked()}, nil
	case api.MethodVaultLock:
		v.LockVault()
		return map[string]any{"locked": true}, nil
	case api.MethodVaultUnlock:
		var p struct {
			Passphrase string `json:"passphrase"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Passphrase == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "vault.unlock needs a passphrase"}
		}
		if err := v.UnlockVault(ctx, []byte(p.Passphrase)); err != nil {
			return nil, vaultErr(err)
		}
		return map[string]any{"unlocked": true}, nil
	case api.MethodVaultDelete:
		var p struct {
			Name      string `json:"name"`
			Confirmed bool   `json:"confirmed"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "vault.delete needs a name"}
		}
		if !p.Confirmed {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "vault.delete needs confirmed:true"}
		}
		if err := v.DeleteEntry(ctx, p.Name, true); err != nil {
			return nil, vaultErr(err)
		}
		return map[string]any{"deleted": p.Name}, nil
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

func vaultErr(err error) *api.Error {
	return &api.Error{Code: api.CodeInternal, Message: err.Error()}
}

// ProjectOp routes project.list/register/get over the workspace
// registry (Section 9.2).
func ProjectOp(reg *workspace.Registry, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodProjectList:
		return map[string]any{"projects": reg.List()}, nil
	case api.MethodProjectRegister:
		var p api.ProjectRegisterParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad project.register params"}
		}
		proj, err := reg.Register(workspace.Project{Name: p.Name, Alias: p.Alias, Description: p.Description, Roots: p.Roots})
		if err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
		}
		return proj, nil
	case api.MethodProjectGet:
		var p struct {
			ID string `json:"project_id"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.ID == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "project.get needs project_id"}
		}
		proj, err := reg.Get(mustProjectID(p.ID))
		if err != nil {
			return nil, &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
		}
		return proj, nil
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// MetricsStore is the daemon-side metrics.query backend (Section
// 22.2): per-task snapshots recorded by the supervision tree, plus
// RAM/CPU samples from the watchdog. Full trajectories export as
// ATIF/trace from the telemetry package.
type MetricsStore struct {
	mu   sync.Mutex
	rows []MetricsRow
}

// MetricsRow is one recorded snapshot.
type MetricsRow struct {
	TaskID    string
	ProjectID string
	At        time.Time
	Snapshot  telemetry.Snapshot
}

// Record appends one snapshot.
func (m *MetricsStore) Record(taskID, projectID string, snap telemetry.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, MetricsRow{TaskID: taskID, ProjectID: projectID, At: time.Now().UTC(), Snapshot: snap})
}

// Query filters rows by the MetricsQueryParams selectors.
func (m *MetricsStore) Query(p api.MetricsQueryParams) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MetricsRow
	for _, r := range m.rows {
		if p.TaskID != nil && r.TaskID != *p.TaskID {
			continue
		}
		if p.ProjectID != nil && r.ProjectID != *p.ProjectID {
			continue
		}
		if p.Since != nil && r.At.Before(*p.Since) {
			continue
		}
		if p.Until != nil && r.At.After(*p.Until) {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return map[string]any{"rows": out, "count": len(out)}
}

// NewTaskQueries connects committed planning changes to the daemon event bus.
// Configure this shared store before binding the task_db provider and routes.
func NewTaskQueries(db *store.DB, bus *telemetry.Bus) *tasks.SQLStore {
	if db == nil {
		return nil
	}
	queries := tasks.OpenSQLStore(db)
	if bus != nil {
		queries.OnTaskUpdated = func(t api.TaskSummary) {
			id := types.TaskID(t.ID)
			_, _ = bus.Publish(api.EventTaskUpdated, nil, &id, api.TaskUpdatedEvent{Task: t})
		}
	}
	return queries
}
