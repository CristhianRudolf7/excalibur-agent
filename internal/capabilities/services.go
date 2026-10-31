package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Service capabilities: the GPU providers (§14.1 gpu-providers), the
// two stores (tool store and automation-flow store), the GitHub
// connector behind the github tool, and the deferred agent-to-agent
// representation channel (Section 14.11).

// GPUManifest publishes gpu_training: launch, follow, pause and cancel
// training jobs on the configured GPU provider, with metric reads.
func GPUManifest() Manifest {
	return Manifest{
		ID: "gpu-providers", Version: "1",
		Operations: []Operation{
			Op("gpu_training", "Launch, follow, pause and cancel training jobs on the configured GPU provider and read their metrics", "exec", "keyed", "cooperative", false, "apps", []string{"gpu", "training", "jobs", "metrics"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "job": "string", "model": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// StoreManifest publishes the store install/publish requests. The
// stores themselves are black boxes; the harness validates the package
// through the conformance suite before installing or publishing at the
// user request.
func StoreManifest() Manifest {
	return Manifest{
		ID: "store", Version: "1",
		Operations: []Operation{
			Op("store_install", "Install a skill, tool, workflow or MCP server package from the store at the user request", "write_local", "keyed", "graceful", false, "admin", []string{"store", "install", "package"}, ObjSchema([]string{"package"}, map[string]string{"package": "string", "version": "string"})),
			Op("store_publish", "Publish an own skill, tool or workflow package to the store at the user request", "write_external", "keyed", "cooperative", false, "admin", []string{"store", "publish", "package"}, ObjSchema([]string{"package"}, map[string]string{"package": "string", "version": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// GitHubManifest publishes the github operations: issues, pull
// requests, comments, commits, SHA comparison, releases and CI runs,
// with the vault token (secret://github-token, Section 20.1) or a
// fallback environment variable.
func GitHubManifest() Manifest {
	return Manifest{
		ID: "github", Version: "1",
		Operations: []Operation{
			Op("github", "Operate GitHub: issues, pull requests, comments, commits, SHA comparison, releases and CI runs", "write_external", "keyed", "cooperative", false, "code", []string{"github", "issues", "pull", "commits", "ci"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "repo": "string", "number": "string", "title": "string", "body": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// GPUJob is one simulated training job.
type GPUJob struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	State   string   `json:"state"`
	Metrics []string `json:"metrics"`
}

// GPUFake is the deterministic GPU-provider simulation.
type GPUFake struct {
	mu   sync.Mutex
	jobs map[string]*GPUJob
	seq  int
}

// NewGPUFake returns a GPU simulation.
func NewGPUFake() *GPUFake { return &GPUFake{jobs: map[string]*GPUJob{}} }

// Manifest implements Capability.
func (g *GPUFake) Manifest() Manifest { return GPUManifest() }

// Invoke implements Capability.
func (g *GPUFake) Invoke(operation string, args json.RawMessage, _ string) (TaskResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if operation != "gpu_training" {
		return Failed("", fmt.Sprintf("gpu: unknown operation %q", operation), ErrInput), nil
	}
	action := argString(args, "action")
	if action == "" {
		action = "metrics"
	}
	switch action {
	case "launch":
		model := argString(args, "model")
		if model == "" {
			return Failed("", "gpu: model is required to launch a job", ErrInput), nil
		}
		g.seq++
		id := fmt.Sprintf("job-%d", g.seq)
		g.jobs[id] = &GPUJob{ID: id, Model: model, State: "running", Metrics: []string{"loss=0.42"}}
		return Completed(id, map[string]any{"job": id, "state": "running"}), nil
	case "metrics", "status":
		id := argString(args, "job")
		if id == "" {
			out := []GPUJob{}
			for _, j := range g.jobs {
				out = append(out, *j)
			}
			return Completed("jobs", map[string]any{"jobs": out}), nil
		}
		j, ok := g.jobs[id]
		if !ok {
			return Failed("", fmt.Sprintf("gpu: unknown job %q", id), ErrInput), nil
		}
		return Completed(id, map[string]any{"job": j}), nil
	case "pause", "cancel":
		id := argString(args, "job")
		j, ok := g.jobs[id]
		if !ok {
			return Failed("", fmt.Sprintf("gpu: unknown job %q", id), ErrInput), nil
		}
		j.State = action + "d"
		return Completed(id, map[string]any{"job": id, "state": j.State}), nil
	default:
		return Failed("", fmt.Sprintf("gpu: unknown gpu action %q", action), ErrInput), nil
	}
}

// Poll implements Capability.
func (g *GPUFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (g *GPUFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (g *GPUFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return g.Poll(taskID)
}

// StoreFake records store install/publish requests after package
// validation. Activation itself stays behind the conformance suite
// (Section 14.12, ext test).
type StoreFake struct {
	mu        sync.Mutex
	installed []string
	published []string
	dedup     KeyDedup
}

// NewStoreFake returns a store simulation.
func NewStoreFake() *StoreFake { return &StoreFake{} }

// Manifest implements Capability.
func (s *StoreFake) Manifest() Manifest { return StoreManifest() }

// Installed returns the installed packages.
func (s *StoreFake) Installed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.installed...)
}

// Invoke implements Capability.
func (s *StoreFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pkg := argString(args, "package")
	if pkg == "" {
		return Failed("", "store: package is required", ErrInput), nil
	}
	switch operation {
	case "store_install":
		if s.dedup.First("install|" + key) {
			s.installed = append(s.installed, pkg)
		}
		return Completed(pkg, map[string]any{"installed": pkg}), nil
	case "store_publish":
		if s.dedup.First("publish|" + key) {
			s.published = append(s.published, pkg)
		}
		return Completed(pkg, map[string]any{"published": pkg}), nil
	default:
		return Failed("", fmt.Sprintf("store: unknown operation %q", operation), ErrInput), nil
	}
}

// Poll implements Capability.
func (s *StoreFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (s *StoreFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (s *StoreFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return s.Poll(taskID)
}

// GitHubFake is the deterministic GitHub simulation: issues, pull
// requests, comments, commits, comparisons, releases and CI runs.
type GitHubFake struct {
	mu     sync.Mutex
	issues map[string]map[string]any
	prs    map[string]map[string]any
	seq    int
}

// NewGitHubFake returns a GitHub simulation.
func NewGitHubFake() *GitHubFake {
	return &GitHubFake{issues: map[string]map[string]any{}, prs: map[string]map[string]any{}}
}

// Manifest implements Capability.
func (g *GitHubFake) Manifest() Manifest { return GitHubManifest() }

// Invoke implements Capability.
func (g *GitHubFake) Invoke(operation string, args json.RawMessage, _ string) (TaskResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if operation != "github" {
		return Failed("", fmt.Sprintf("github: unknown operation %q", operation), ErrInput), nil
	}
	action := strings.ToLower(argString(args, "action"))
	repo := argString(args, "repo")
	if repo == "" {
		return Failed("", "github: repo is required", ErrInput), nil
	}
	switch action {
	case "issue_create", "issue":
		g.seq++
		id := fmt.Sprintf("%s#%d", repo, g.seq)
		g.issues[id] = map[string]any{"repo": repo, "title": argString(args, "title"), "body": argString(args, "body"), "state": "open"}
		return Completed(id, map[string]any{"issue": id}), nil
	case "issue_list":
		out := []map[string]any{}
		for id, is := range g.issues {
			if strings.HasPrefix(id, repo+"#") {
				out = append(out, map[string]any{"id": id, "issue": is})
			}
		}
		return Completed(repo, map[string]any{"issues": out}), nil
	case "pr_create", "pr":
		g.seq++
		id := fmt.Sprintf("%s!%d", repo, g.seq)
		g.prs[id] = map[string]any{"repo": repo, "title": argString(args, "title"), "state": "open"}
		return Completed(id, map[string]any{"pr": id}), nil
	case "comment":
		return Completed("comment-1", map[string]any{"commented": argString(args, "number")}), nil
	case "commits", "compare":
		return Completed("sha-1", map[string]any{"shas": []string{"abc1234"}}), nil
	case "release":
		return Completed("rel-1", map[string]any{"release": argString(args, "title")}), nil
	case "ci_runs", "ci":
		return Completed("ci-1", map[string]any{"runs": []map[string]any{{"name": "ci", "status": "success"}}}), nil
	default:
		return Failed("", fmt.Sprintf("github: unknown action %q", action), ErrInput), nil
	}
}

// Poll implements Capability.
func (g *GitHubFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (g *GitHubFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (g *GitHubFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return g.Poll(taskID)
}

// UserRepStub is the deferred agent-to-agent representation channel
// (Section 14.11). Representing another user's agent depends on the
// disclosure filter and the representation policy, both deferred with
// the rest of Section 20; the stub refuses with a permanent error so
// no agent can impersonate a user by mistake.
type UserRepStub struct{}

// UserRepManifest publishes the deferred representation operation.
func UserRepManifest() Manifest {
	return Manifest{
		ID: "userrep", Version: "0",
		Operations: []Operation{
			Op("agent_delegate_user", "Represent a user before another user's agent (deferred: needs the disclosure filter and representation policy)", "communication", "non_idempotent", "cooperative", false, "communication", []string{"agent", "represent", "delegate", "user"}, AnySchema()),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// Manifest implements Capability.
func (u UserRepStub) Manifest() Manifest { return UserRepManifest() }

// Invoke implements Capability.
func (u UserRepStub) Invoke(_ string, _ json.RawMessage, _ string) (TaskResult, error) {
	return Failed("", "userrep: deferred: agent-to-agent user representation needs the disclosure filter and representation policy (Section 20)", ErrPermanent), nil
}

// Poll implements Capability.
func (u UserRepStub) Poll(taskID string) (TaskResult, error) {
	return Failed(taskID, "userrep: deferred", ErrPermanent), nil
}

// Cancel implements Capability.
func (u UserRepStub) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (u UserRepStub) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return u.Poll(taskID)
}
