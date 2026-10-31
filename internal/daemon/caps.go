package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// CapabilityRoutes serves capability.* through the daemon registry
// (Section 14.12): native capabilities invoke directly, registered
// manifests list for discovery, and MCP servers dial lazily on first
// invoke (route-1) with the connection cached for later calls and
// crash-resume polls.
type CapabilityRoutes struct {
	Reg *capabilities.Registry

	mu     sync.Mutex
	routes map[string]capabilityRoute
	tasks  map[string]capabilities.TaskResult
	mcps   map[string]*capabilities.MCPClient
}

type capabilityRoute struct {
	manifest capabilities.Manifest
	dial     capabilities.MCPDial
	hasDial  bool
}

// NewCapabilityRoutes returns routes over reg. A nil reg gets a fresh
// empty registry; DefaultRegistry() wires every native capability.
func NewCapabilityRoutes(reg *capabilities.Registry) *CapabilityRoutes {
	if reg == nil {
		reg = capabilities.NewRegistry()
	}
	return &CapabilityRoutes{
		Reg:    reg,
		routes: map[string]capabilityRoute{},
		tasks:  map[string]capabilities.TaskResult{},
		mcps:   map[string]*capabilities.MCPClient{},
	}
}

// Op routes one capability.* gate call.
func (r *CapabilityRoutes) Op(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case api.MethodCapabilityList:
		manifests := r.Reg.Manifests()
		r.mu.Lock()
		for id, rt := range r.routes {
			if _, ok := r.Reg.Lookup(id); !ok {
				manifests = append(manifests, rt.manifest)
			}
		}
		r.mu.Unlock()
		sort.Slice(manifests, func(i, j int) bool { return manifests[i].ID < manifests[j].ID })
		return map[string]any{"capabilities": manifests}, nil
	case api.MethodCapabilityRegister:
		var p struct {
			Manifest  capabilities.Manifest `json:"manifest"`
			MCP       *capabilities.MCPDial `json:"mcp,omitempty"`
			Command   []string              `json:"mcp_command,omitempty"`
			ServerURL string                `json:"mcp_url,omitempty"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad capability.register params"}
		}
		if err := p.Manifest.Validate(); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
		}
		r.mu.Lock()
		rt := capabilityRoute{manifest: p.Manifest}
		if p.MCP != nil {
			rt.dial = *p.MCP
			rt.hasDial = true
		} else if len(p.Command) > 0 || p.ServerURL != "" {
			rt.dial = capabilities.MCPDial{Command: p.Command, URL: p.ServerURL}
			rt.hasDial = true
		}
		r.routes[p.Manifest.ID] = rt
		r.mu.Unlock()
		return map[string]any{"capability": p.Manifest.ID, "operations": len(p.Manifest.Operations)}, nil
	case api.MethodCapabilityInvoke:
		var p api.CapabilityInvokeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad capability.invoke params"}
		}
		if p.Capability == "" || p.Operation == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "capability.invoke needs capability and operation"}
		}
		args, err := json.Marshal(p.Args)
		if err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad capability.invoke args"}
		}
		if args == nil {
			args = json.RawMessage(`{}`)
		}
		return r.Invoke(ctx, p.Capability, p.Operation, args, p.IdempotencyKey)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}

// Invoke runs one operation: native capabilities through the
// registry, MCP-routed manifests through a lazily dialed client
// cached per capability id.
func (r *CapabilityRoutes) Invoke(ctx context.Context, id, operation string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	if _, ok := r.Reg.Lookup(id); ok {
		res, err := r.Reg.Invoke(id, operation, args, key)
		if err != nil {
			return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ClassifyError(err)}, err
		}
		r.remember(res)
		return res, nil
	}
	r.mu.Lock()
	rt, ok := r.routes[id]
	r.mu.Unlock()
	if !ok {
		return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ErrInput},
			&api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown capability %q", id)}
	}
	if !rt.hasDial {
		return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ErrInput},
			&api.Error{Code: api.CodeConflict, Message: fmt.Sprintf("capability %q registered without an execution route", id)}
	}
	client, err := r.mcpClient(ctx, id, rt.dial)
	if err != nil {
		return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ErrTransient}, err
	}
	callArgs := map[string]any{}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &callArgs)
	}
	res, err := client.Call(ctx, operation, callArgs)
	if err != nil {
		return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ClassifyError(err)}, err
	}
	r.remember(res)
	return res, nil
}

// Poll resumes a working task: native tasks poll the capability,
// MCP-routed tasks replay the cached terminal result (17.3).
func (r *CapabilityRoutes) Poll(ctx context.Context, id, taskID string) (capabilities.TaskResult, error) {
	if _, ok := r.Reg.Lookup(id); ok {
		return r.Reg.Poll(id, taskID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.tasks[taskID]; ok {
		return res, nil
	}
	return capabilities.TaskResult{State: capabilities.StateFailed, ErrClass: capabilities.ErrInput},
		&api.Error{Code: api.CodeUnknownTarget, Message: fmt.Sprintf("unknown task %q", taskID)}
}

func (r *CapabilityRoutes) remember(res capabilities.TaskResult) {
	if res.TaskID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[res.TaskID] = res
}

func (r *CapabilityRoutes) mcpClient(ctx context.Context, id string, dial capabilities.MCPDial) (*capabilities.MCPClient, error) {
	r.mu.Lock()
	if c, ok := r.mcps[id]; ok {
		r.mu.Unlock()
		return c, nil
	}
	r.mu.Unlock()
	c, err := capabilities.DialMCP(ctx, dial)
	if err != nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: fmt.Sprintf("dial MCP capability %q: %v", id, err)}
	}
	r.mu.Lock()
	r.mcps[id] = c
	r.mu.Unlock()
	return c, nil
}
