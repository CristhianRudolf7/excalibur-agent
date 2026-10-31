package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Route adapters for the extension kit (Section 14.12): MCP servers,
// applications with an interface, and the shared task-result mapping.
// Route 3 (packages) lives in bindPackage; route 4 (builtin) pairs
// manifests with in-process tools via ProvideBuiltin.

// AppChannel is the harness side of a route-2 application (Section
// 14.8): operation calls plus the manual-change events the compiler
// includes as an L3 delta. Applications register with
// capability.register and publish user changes with capability.event;
// both arrive here.
type AppChannel interface {
	// Invoke runs one application operation with an idempotency key.
	Invoke(ctx context.Context, operation string, args json.RawMessage, key string) (capabilities.TaskResult, error)
	// EventsSince returns the manual-change events after seq for the
	// compiler delta.
	EventsSince(instance string, seq int) []capabilities.AppEvent
	// Emit records a manual user change.
	Emit(instance, actor, change string)
	// Close releases the channel.
	Close() error
}

// MemoryAppChannel is the deterministic route-2 simulation over the
// apps capability fake: operations mutate the same state the user
// edits, and every change is visible to the compiler delta.
type MemoryAppChannel struct {
	mu     sync.Mutex
	apps   *capabilities.AppsFake
	closed bool
}

// NewMemoryAppChannel returns a route-2 simulation.
func NewMemoryAppChannel() *MemoryAppChannel {
	return &MemoryAppChannel{apps: capabilities.NewAppsFake()}
}

// Apps exposes the underlying fake for test setup.
func (c *MemoryAppChannel) Apps() *capabilities.AppsFake { return c.apps }

// Invoke implements AppChannel. Unknown instances auto-register,
// simulating the capability.register of a route-2 application.
func (c *MemoryAppChannel) Invoke(_ context.Context, operation string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return capabilities.TaskResult{}, fmt.Errorf("tools: app channel is closed")
	}
	var decoded map[string]any
	if err := json.Unmarshal(args, &decoded); err == nil {
		if id, _ := decoded["instance"].(string); id != "" {
			c.apps.Register(id, "generic")
		}
	}
	return c.apps.Invoke(operation, args, key)
}

// EventsSince implements AppChannel.
func (c *MemoryAppChannel) EventsSince(instance string, seq int) []capabilities.AppEvent {
	return c.apps.DeltaSince(instance, seq)
}

// Emit implements AppChannel.
func (c *MemoryAppChannel) Emit(instance, actor, change string) {
	if actor == "user" {
		c.apps.EmitUserChange(instance, change)
		return
	}
	c.apps.EmitUserChange(instance, actor+": "+change)
}

// Close implements AppChannel.
func (c *MemoryAppChannel) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// taskResultToResult maps a durable-task outcome to a tool result
// (Sections 13.4, 14.1): completed payloads become OK outputs, failed
// maps by error class, input_required surfaces its need for the
// engine ask_user, and a still-working operation reports a timeout the
// harness re-polls by task id.
func taskResultToResult(tr capabilities.TaskResult) Result {
	switch tr.State {
	case capabilities.StateCompleted:
		out := string(tr.Payload)
		if out == "" {
			out = "{}"
		}
		return Result{Status: StatusOK, Output: out}
	case capabilities.StateFailed:
		msg := tr.ErrorText
		if msg == "" {
			msg = string(tr.Payload)
		}
		if msg == "" {
			msg = "capability failed"
		}
		if tr.ErrClass == capabilities.ErrTransient {
			return Result{Status: StatusTransient, Transient: true, Output: msg}
		}
		if tr.ErrClass == capabilities.ErrInput {
			return Result{Status: StatusInputError, Output: msg}
		}
		return Result{Status: StatusToolError, Output: msg}
	case capabilities.StateCancelled:
		return Result{Status: StatusCanceled, Output: "capability cancelled"}
	case capabilities.StateInputRequired:
		need := tr.Need
		if need == "" {
			need = string(tr.Payload)
		}
		return Result{Status: StatusOK, Output: "input_required: " + need}
	case capabilities.StateWorking:
		return Result{Status: StatusTimeout, Output: fmt.Sprintf("capability task %q still working", tr.TaskID)}
	default:
		return Result{Status: StatusToolError, Output: fmt.Sprintf("unknown capability state %q", tr.State)}
	}
}

// appOpTool is one route-2 application operation as a catalog tool.
type appOpTool struct {
	def     Definition
	channel AppChannel
	op      string
}

func (t *appOpTool) Definition() Definition { return t.def }

func (t *appOpTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	tr, err := t.channel.Invoke(ctx, t.op, inv.Arguments, inv.IdempotencyKey)
	if err != nil {
		if capabilities.ClassifyError(err) == capabilities.ErrTransient {
			return Result{Status: StatusTransient, Transient: true, Output: err.Error()}, nil
		}
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return taskResultToResult(tr), nil
}

func (t *appOpTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return []string{"app:" + t.op}, nil
}

// bindApp builds the route-2 tools: every manifest entry invokes the
// bound application channel. The channel must be bound with BindApp
// before activation, mirroring capability.register on the live gate.
func (l *Loader) bindApp(ext *LoadedExtension) ([]Tool, ioCloser, error) {
	ch, ok := l.apps[ext.Manifest.ID]
	if !ok {
		return nil, nil, fmt.Errorf("app extension %q has no bound channel (call BindApp after capability.register)", ext.Manifest.ID)
	}
	tools := make([]Tool, 0, len(ext.Manifest.Tools))
	for _, entry := range ext.Manifest.Tools {
		entry := entry
		tools = append(tools, &appOpTool{def: entry.Definition, channel: ch, op: entry.Name})
	}
	return tools, nil, nil
}

// mcpConn is the lazy MCP connection of one route-1 extension.
type mcpConn struct {
	mu       sync.Mutex
	dial     capabilities.MCPDial
	dir      string
	client   *capabilities.MCPClient
	onceErr  error
	once     sync.Once
	lastUsed atomic.Int64
}

func (c *mcpConn) ensure(ctx context.Context) (*capabilities.MCPClient, error) {
	c.once.Do(func() {
		client, err := capabilities.DialMCP(ctx, c.dial)
		if err != nil {
			c.onceErr = err
			return
		}
		c.mu.Lock()
		c.client = client
		c.mu.Unlock()
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.onceErr != nil {
		return nil, c.onceErr
	}
	return c.client, nil
}

// Close implements ioCloser.
func (c *mcpConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// mcpOpTool is one route-1 MCP server tool as a catalog tool.
type mcpOpTool struct {
	def    Definition
	conn   *mcpConn
	bare   string
	server string
}

func (t *mcpOpTool) Definition() Definition { return t.def }

func (t *mcpOpTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	client, err := t.conn.ensure(ctx)
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	var args map[string]any
	if len(inv.Arguments) > 0 {
		if err := json.Unmarshal(inv.Arguments, &args); err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
	}
	t.conn.lastUsed.Store(time.Now().UnixNano())
	tr, err := client.Call(ctx, t.bare, args)
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return taskResultToResult(tr), nil
}

func (t *mcpOpTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return []string{"mcp:" + t.server + ":" + t.bare}, nil
}

// bindMCP builds the route-1 tools by listing the server once at
// activation. The connection stays lazy: the first call dials, and a
// resident lifecycle dials eagerly so activation fails fast.
func (l *Loader) bindMCP(ext *LoadedExtension) ([]Tool, ioCloser, error) {
	m := ext.Manifest
	dial := capabilities.MCPDial{Server: m.ID, Dir: ext.Dir}
	switch m.Transport {
	case "", "stdio":
		if len(m.Command) == 0 {
			return nil, nil, fmt.Errorf("mcp extension %q needs a command for stdio transport", m.ID)
		}
		dial.Command = m.Command
	case "http", "streamable":
		if m.URL == "" {
			return nil, nil, fmt.Errorf("mcp extension %q needs a url for http transport", m.ID)
		}
		dial.URL = m.URL
	case "socket":
		return nil, nil, fmt.Errorf("mcp extension %q: unix-socket transport is not wired in this version", m.ID)
	default:
		return nil, nil, fmt.Errorf("mcp extension %q: unknown transport %q", m.ID, m.Transport)
	}
	conn := &mcpConn{dial: dial, dir: ext.Dir}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := conn.ensure(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp extension %q: connect: %w", m.ID, err)
	}
	bareTools, err := client.Tools(context.Background())
	if err != nil {
		_ = client.Close()
		return nil, nil, fmt.Errorf("mcp extension %q: list tools: %w", m.ID, err)
	}
	manifest := capabilities.ManifestFromMCPTools(m.ID, bareTools)
	byName := map[string]string{}
	for _, bt := range bareTools {
		byName[capabilities.MCPToolName(m.ID, bt.Name)] = bt.Name
	}
	tools := make([]Tool, 0, len(manifest.Operations))
	for _, op := range manifest.Operations {
		op := op
		def := Definition{
			Name: op.Name, Description: op.Description,
			InputSchema: op.InputSchema, OutputSchema: op.OutputSchema,
			Effect: types.EffectClass(op.Effect), Idempotency: types.IdempotencyClass(op.Idempotency),
			Cancel: types.CancelMode(op.Cancel), Fast: op.Fast,
			Timeout: 120 * time.Second, OutputBudget: DefaultOutputBudget,
			Platforms: append([]string(nil), op.Platforms...),
			Profiles:  append([]string(nil), op.Profiles...),
			Group:     op.Group, Tags: append([]string(nil), op.Tags...),
			Keywords: append([]string(nil), op.Keywords...),
		}
		if op.TimeoutSecs > 0 {
			def.Timeout = time.Duration(op.TimeoutSecs) * time.Second
		}
		if op.OutputBudget > 0 {
			def.OutputBudget = op.OutputBudget
		}
		tools = append(tools, &mcpOpTool{def: def, conn: conn, bare: byName[op.Name], server: m.ID})
	}
	return tools, conn, nil
}
