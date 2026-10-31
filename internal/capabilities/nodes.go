package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Remote nodes (Section 14.10). A node is another daemon or a light
// agent on a paired device declaring capabilities (terminal, browser,
// desktop). Tools such as exec address a node with target node:<id>;
// the node runs them through the same pipeline (Section 13.4). The
// nodes tool lists the paired nodes with their capabilities and state.

// NodeState is the harness view of one paired node.
type NodeState struct {
	ID           string   `json:"id"`
	Capabilities []string `json:"capabilities"`
	Connected    bool     `json:"connected"`
	OS           string   `json:"os"`
}

// NodesManifest publishes the nodes listing operation.
func NodesManifest() Manifest {
	return Manifest{
		ID: "nodes", Version: "1",
		Operations: []Operation{
			Op("nodes", "List the paired remote nodes with their capabilities and state", "read_only", "pure", "immediate", true, "admin", []string{"nodes", "remote", "devices", "paired"}, ObjSchema(nil, map[string]string{})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// NodeRegistry is the deterministic node simulation: pairing,
// capability declaration and connected/disconnected runs.
type NodeRegistry struct {
	mu    sync.Mutex
	nodes map[string]*NodeState
	runs  map[string][]string
}

// NewNodeRegistry returns an empty node registry.
func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{nodes: map[string]*NodeState{}, runs: map[string][]string{}}
}

// Manifest implements Capability.
func (r *NodeRegistry) Manifest() Manifest { return NodesManifest() }

// Pair registers a node with its declared capabilities.
func (r *NodeRegistry) Pair(id, os string, capabilities []string, connected bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[id] = &NodeState{ID: id, Capabilities: capabilities, Connected: connected, OS: os}
}

// SetConnected flips the connection state of a node.
func (r *NodeRegistry) SetConnected(id string, connected bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n, ok := r.nodes[id]; ok {
		n.Connected = connected
	}
}

// Runs returns the commands executed on a node.
func (r *NodeRegistry) Runs(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.runs[id]...)
}

// ParseTarget splits a node:<id> tool target.
func ParseTarget(target string) (string, bool) {
	if !strings.HasPrefix(target, "node:") {
		return "", false
	}
	id := strings.TrimPrefix(target, "node:")
	if id == "" {
		return "", false
	}
	return id, true
}

// Exec runs a command on a node through the same pipeline shape
// (Section 13.4): connected nodes execute, disconnected ones return a
// declared error (or wait when wait=true) instead of running locally.
func (r *NodeRegistry) Exec(target, command string, wait bool) (TaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := ParseTarget(target)
	if !ok {
		return Failed("", fmt.Sprintf("nodes: bad target %q (want node:<id>)", target), ErrInput), nil
	}
	n, known := r.nodes[id]
	if !known {
		return Failed("", fmt.Sprintf("nodes: unknown node %q", id), ErrInput), nil
	}
	if !n.Connected {
		if wait {
			return TaskResult{TaskID: "node-" + id, State: StateWorking}, nil
		}
		return Failed("node-"+id, fmt.Sprintf("nodes: node %q is disconnected", id), ErrTransient), nil
	}
	hasTerminal := false
	for _, c := range n.Capabilities {
		if c == "terminal" {
			hasTerminal = true
		}
	}
	if !hasTerminal {
		return Failed("node-"+id, fmt.Sprintf("nodes: node %q does not declare the terminal capability", id), ErrInput), nil
	}
	r.runs[id] = append(r.runs[id], command)
	return Completed("node-"+id, map[string]any{"node": id, "ran": command, "exit": 0}), nil
}

// Invoke implements Capability (the nodes listing tool).
func (r *NodeRegistry) Invoke(operation string, _ json.RawMessage, _ string) (TaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if operation != "nodes" {
		return Failed("", fmt.Sprintf("nodes: unknown operation %q", operation), ErrInput), nil
	}
	out := []NodeState{}
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	return Completed("nodes", map[string]any{"nodes": out}), nil
}

// Poll implements Capability: a waiting disconnected node reports
// working until it reconnects.
func (r *NodeRegistry) Poll(taskID string) (TaskResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := strings.TrimPrefix(taskID, "node-")
	if n, ok := r.nodes[id]; ok && n.Connected {
		return Completed(taskID, map[string]any{"node": id, "reconnected": true}), nil
	}
	return TaskResult{TaskID: taskID, State: StateWorking}, nil
}

// Cancel implements Capability.
func (r *NodeRegistry) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (r *NodeRegistry) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return r.Poll(taskID)
}
