package capabilities

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExcaliburMetaKeys are the _meta keys carrying Excalibur execution
// fields over MCP (Section 14.12). Servers write their manifest fields
// under "excalibur/"; the bare "excalibur" key is accepted for
// robustness. When neither is present the fields derive from the MCP
// tool annotations with the most conservative value.
const (
	ExcaliburMetaKey     = "excalibur/"
	ExcaliburMetaKeyBare = "excalibur"
)

// MCPToolMeta holds the Excalibur execution fields of one MCP tool.
type MCPToolMeta struct {
	Effect       string   `json:"effect,omitempty"`
	Idempotency  string   `json:"idempotency,omitempty"`
	Cancel       string   `json:"cancel,omitempty"`
	Fast         bool     `json:"fast,omitempty"`
	Group        string   `json:"group,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Keywords     []string `json:"keywords,omitempty"`
	TimeoutSecs  int64    `json:"timeout_secs,omitempty"`
	OutputBudget int64    `json:"output_budget,omitempty"`
	DryRun       string   `json:"dry_run,omitempty"`
	Inverse      string   `json:"inverse,omitempty"`
	Probe        string   `json:"probe,omitempty"`
}

// ExcaliburMeta extracts the Excalibur fields of an MCP tool. Explicit
// _meta wins; missing fields derive from the MCP annotations with the
// most conservative mapping: readOnlyHint -> read_only/pure/immediate,
// idempotentHint -> idempotent, otherwise write_external (destructive
// world assumed), non_idempotent, cooperative cancel.
func ExcaliburMeta(t *mcp.Tool) MCPToolMeta {
	meta := MCPToolMeta{}
	if t == nil {
		return conservativeMeta(meta)
	}
	if raw := lookupExcaliburMeta(t.Meta); len(raw) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err == nil {
			meta = metaFromMap(decoded)
		} else {
			// A scalar string under the key is a no-op: annotations rule.
			_ = raw
		}
	}
	if meta.Effect == "" || meta.Idempotency == "" || meta.Cancel == "" {
		derived := deriveFromAnnotations(t.Annotations)
		if meta.Effect == "" {
			meta.Effect = derived.Effect
		}
		if meta.Idempotency == "" {
			meta.Idempotency = derived.Idempotency
		}
		if meta.Cancel == "" {
			meta.Cancel = derived.Cancel
		}
	}
	return conservativeMeta(meta)
}

func lookupExcaliburMeta(m mcp.Meta) json.RawMessage {
	if m == nil {
		return nil
	}
	for _, key := range []string{ExcaliburMetaKey, ExcaliburMetaKeyBare} {
		if v, ok := m[key]; ok {
			raw, err := json.Marshal(v)
			if err == nil {
				return raw
			}
		}
	}
	return nil
}

func metaFromMap(decoded map[string]any) MCPToolMeta {
	meta := MCPToolMeta{}
	str := func(key string) string {
		v, _ := decoded[key].(string)
		return v
	}
	meta.Effect = str("effect")
	meta.Idempotency = str("idempotency")
	meta.Cancel = str("cancel")
	meta.Group = str("group")
	meta.DryRun = str("dry_run")
	meta.Inverse = str("inverse")
	meta.Probe = str("probe")
	if b, ok := decoded["fast"].(bool); ok {
		meta.Fast = b
	}
	if n, ok := decoded["timeout_secs"].(float64); ok {
		meta.TimeoutSecs = int64(n)
	}
	if n, ok := decoded["output_budget"].(float64); ok {
		meta.OutputBudget = int64(n)
	}
	meta.Tags = strSlice(decoded["tags"])
	meta.Keywords = strSlice(decoded["keywords"])
	return meta
}

func strSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), t...)
	}
	return nil
}

// deriveFromAnnotations maps MCP hints to the conservative harness triple.
func deriveFromAnnotations(a *mcp.ToolAnnotations) MCPToolMeta {
	meta := MCPToolMeta{}
	if a != nil && a.ReadOnlyHint {
		meta.Effect = "read_only"
		meta.Idempotency = "pure"
		meta.Cancel = "immediate"
		return meta
	}
	if a != nil && a.IdempotentHint {
		meta.Effect = "write_local"
		meta.Idempotency = "idempotent"
		meta.Cancel = "after_completion"
		return meta
	}
	meta.Effect = "write_external"
	meta.Idempotency = "non_idempotent"
	meta.Cancel = "cooperative"
	return meta
}

// conservativeMeta fills any missing triple field with the most
// conservative value so an unknown MCP server can never claim a weaker
// effect than it has.
func conservativeMeta(m MCPToolMeta) MCPToolMeta {
	if m.Effect == "" {
		m.Effect = "write_external"
	}
	if m.Idempotency == "" {
		m.Idempotency = "non_idempotent"
	}
	if m.Cancel == "" {
		m.Cancel = "cooperative"
	}
	return m
}

// ManifestFromMCPTools converts one MCP server's tool list into a
// capability manifest (Section 14.1). Catalog names follow Section
// 13.3: mcp__<server>__<tool>, hash-abbreviated past 64 chars. The
// server list order is normalized to deterministic (sorted) order so
// the input cache prefix never depends on server ordering (13.1).
func ManifestFromMCPTools(server string, tools []*mcp.Tool) Manifest {
	ops := make([]Operation, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		meta := ExcaliburMeta(t)
		in := schemaAnyToRaw(t.InputSchema)
		if len(in) == 0 {
			in = json.RawMessage(`{"type":"object"}`)
		}
		op := Operation{
			Name: MCPToolName(server, t.Name), Description: toolDescription(t),
			InputSchema: in, OutputSchema: schemaAnyToRaw(t.OutputSchema),
			Effect: meta.Effect, Idempotency: meta.Idempotency,
			Cancel: meta.Cancel, Fast: meta.Fast,
			Platforms:   []string{"linux"},
			Profiles:    []string{"default", "delegate", "eval"},
			Group:       meta.Group,
			Tags:        append([]string{"mcp", "server:" + server}, meta.Tags...),
			Keywords:    append([]string{t.Name, server}, meta.Keywords...),
			TimeoutSecs: meta.TimeoutSecs, OutputBudget: meta.OutputBudget,
			Inverse: meta.Inverse, Probe: meta.Probe, DryRun: meta.DryRun,
		}
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
	return Manifest{
		ID: "mcp-" + sanitizeMCP(server), Version: "1",
		Operations: ops,
		Platforms:  []string{"linux"},
		Profiles:   []string{"default", "delegate", "eval"},
	}
}

func toolDescription(t *mcp.Tool) string {
	if t.Description != "" {
		return t.Description
	}
	return "MCP tool " + t.Name
}

func schemaAnyToRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return append(json.RawMessage(nil), raw...)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return out
}

// TaskResultFromMCP converts an MCP CallToolResult into a durable-task
// result (Section 14.1). Content text becomes the payload; a task
// handle under the tasks-extension meta key yields a working state the
// harness polls by id.
func TaskResultFromMCP(taskID string, res *mcp.CallToolResult) TaskResult {
	if res == nil {
		return TaskResult{TaskID: taskID, State: StateFailed, ErrorText: "mcp: empty result", ErrClass: ErrPermanent}
	}
	if handle := lookupTaskHandle(res.Meta); handle != "" {
		return TaskResult{TaskID: handle, State: StateWorking}
	}
	if res.IsError {
		return TaskResult{TaskID: taskID, State: StateFailed,
			ErrorText: mcpTextContent(res), ErrClass: ErrPermanent}
	}
	return TaskResult{TaskID: taskID, State: StateCompleted, Payload: mcpPayload(res)}
}

func lookupTaskHandle(m mcp.Meta) string {
	if m == nil {
		return ""
	}
	for key, v := range m {
		lk := strings.ToLower(key)
		if strings.Contains(lk, "task") {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case map[string]any:
				if id, _ := t["taskId"].(string); id != "" {
					return id
				}
				if id, _ := t["task_id"].(string); id != "" {
					return id
				}
			}
		}
	}
	return ""
}

func mcpTextContent(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if b.Len() == 0 {
		return "mcp tool reported an error"
	}
	return b.String()
}

func mcpPayload(res *mcp.CallToolResult) json.RawMessage {
	if res.StructuredContent != nil {
		if raw, err := json.Marshal(res.StructuredContent); err == nil && len(raw) > 0 {
			return raw
		}
	}
	text := mcpTextContent(res)
	if text == "" {
		return json.RawMessage(`{"ok":true}`)
	}
	raw, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		return json.RawMessage(`{"ok":true}`)
	}
	return raw
}

// MCPClient is the harness MCP client (Section 14.1, capability
// `mcp`) over the official Go SDK. It speaks stdio servers (default
// route) and streamable HTTP servers, lists tools in deterministic
// order and calls them through the durable-task mapping above.
// Unix-socket servers are rejected at dial time until wired.
type MCPClient struct {
	server  string
	command []string
	url     string

	client  *mcp.Client
	session *mcp.ClientSession
}

// MCPDial describes how to reach one MCP server (Section 14.12:
// process by stdio, running server by HTTP streamable or Unix socket).
// Dir is the working directory of a stdio process and the base a
// relative command resolves against, so extensions ship their server
// next to their manifest.
type MCPDial struct {
	Server  string
	Command []string
	URL     string
	Socket  string
	Dir     string
}

// DialMCP connects to one MCP server.
func DialMCP(ctx context.Context, d MCPDial) (*MCPClient, error) {
	if d.Server == "" {
		return nil, fmt.Errorf("capabilities: mcp server needs a name")
	}
	switch {
	case len(d.Command) > 0:
		return dialMCPCommand(ctx, d)
	case d.URL != "":
		return dialMCPURL(ctx, d)
	case d.Socket != "":
		return nil, fmt.Errorf("capabilities: mcp server %q: unix-socket transport not wired in this version", d.Server)
	default:
		return nil, fmt.Errorf("capabilities: mcp server %q needs a command, url or socket", d.Server)
	}
}

func dialMCPCommand(ctx context.Context, d MCPDial) (*MCPClient, error) {
	bin := d.Command[0]
	if d.Dir != "" && !filepath.IsAbs(bin) {
		bin = filepath.Join(d.Dir, bin)
	}
	cmd := exec.Command(bin, d.Command[1:]...)
	if d.Dir != "" {
		cmd.Dir = d.Dir
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "excalibur", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("capabilities: mcp server %q connect: %w", d.Server, err)
	}
	return &MCPClient{server: d.Server, command: d.Command, client: client, session: session}, nil
}

func dialMCPURL(ctx context.Context, d MCPDial) (*MCPClient, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "excalibur", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: d.URL}, nil)
	if err != nil {
		return nil, fmt.Errorf("capabilities: mcp server %q connect: %w", d.Server, err)
	}
	return &MCPClient{server: d.Server, url: d.URL, client: client, session: session}, nil
}

// Server returns the configured server name.
func (c *MCPClient) Server() string { return c.server }

// Close ends the MCP session.
func (c *MCPClient) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}

// Tools lists the server tools in deterministic (sorted) order.
func (c *MCPClient) Tools(ctx context.Context) ([]*mcp.Tool, error) {
	if c == nil || c.session == nil {
		return nil, fmt.Errorf("capabilities: mcp server %q is not connected", c.server)
	}
	res, err := c.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("capabilities: mcp server %q list tools: %w", c.server, err)
	}
	out := append([]*mcp.Tool(nil), res.Tools...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Manifest lists the server tools as a capability manifest.
func (c *MCPClient) Manifest(ctx context.Context) (Manifest, error) {
	tools, err := c.Tools(ctx)
	if err != nil {
		return Manifest{}, err
	}
	return ManifestFromMCPTools(c.server, tools), nil
}

// Call invokes one server tool by its bare (server-local) name.
func (c *MCPClient) Call(ctx context.Context, tool string, args map[string]any) (TaskResult, error) {
	if c == nil || c.session == nil {
		return TaskResult{}, fmt.Errorf("capabilities: mcp server %q is not connected", c.server)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		if ClassifyError(err) == ErrTransient {
			return TaskResult{State: StateFailed, ErrorText: err.Error(), ErrClass: ErrTransient}, nil
		}
		return TaskResult{State: StateFailed, ErrorText: err.Error(), ErrClass: ErrPermanent}, nil
	}
	return TaskResultFromMCP("mcp-"+sanitizeMCP(c.server)+"-"+sanitizeMCP(tool), res), nil
}
