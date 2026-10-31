package codeintel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// DefaultIdleTTL shuts down unused language servers after 10 minutes
// without use, freeing RAM (Sections 13.8, 10.8, Appendix B).
const DefaultIdleTTL = 10 * time.Minute

// ServerConfig describes one language server (Section 13.8):
// configurable command and arguments in the general or project
// configuration. RootDir is set by the Manager from the project.
type ServerConfig struct {
	// Language is the codeintel language key ("go", "python", ...).
	Language string
	// Command and Args spawn the server on stdio.
	Command string
	Args    []string
	// Env holds KEY=VAL additions for the server process.
	Env []string
	// RootDir is the project root served as workspace root.
	RootDir string
}

// Validate checks the server configuration.
func (c ServerConfig) Validate() error {
	if c.Language == "" {
		return fmt.Errorf("codeintel: lsp config needs a language")
	}
	if c.Command == "" {
		return fmt.Errorf("codeintel: lsp server for %q needs a command", c.Language)
	}
	if c.RootDir == "" {
		return fmt.Errorf("codeintel: lsp server for %q needs a root dir", c.Language)
	}
	return nil
}

// LSP wire subsets used by the deferred lsp tool (Section 13.8:
// definition, references, document and workspace symbols,
// diagnostics, rename).

// Position is a zero-based LSP position.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a zero-based LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is an LSP location.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// LocationLink is the go-to-definition link form.
type LocationLink struct {
	TargetURI   string `json:"targetUri"`
	TargetRange Range  `json:"targetRange"`
}

// DocumentSymbol is one hierarchical document symbol.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is one flat workspace symbol.
type SymbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location Location `json:"location"`
}

// Diagnostic is one published diagnostic.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// TextEdit is one workspace edit entry.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit is the rename outcome: files to change with edits.
type WorkspaceEdit struct {
	// Changes maps file paths (project-relative when inside the
	// workspace root, absolute otherwise) to their edits.
	Changes map[string][]TextEdit
}

// jsonRPC envelopes.

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("lsp: rpc error %d: %s", e.Code, e.Message)
}

// Client is a JSON-RPC 2.0 LSP client over a framed stream (Section
// 13.8). Production transports are language-server processes on
// stdio; tests inject pipes. The client is safe for concurrent use.
type Client struct {
	rwc     io.ReadWriteCloser
	rootURI string

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan rpcResult
	closed  bool

	diagMu sync.RWMutex
	diags  map[string][]Diagnostic

	openMu sync.Mutex
	opened map[string]int // uri -> version
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

// NewPipeClient builds a Client over an open stream and starts the
// reader loop. The caller owns the handshake (Initialize) and the
// shutdown sequence (Shutdown/Close).
func NewPipeClient(rwc io.ReadWriteCloser, rootDir string) *Client {
	c := &Client{
		rwc:     rwc,
		rootURI: dirToURI(rootDir),
		pending: map[int64]chan rpcResult{},
		diags:   map[string][]Diagnostic{},
		opened:  map[string]int{},
	}
	safego.Go("codeintel/lsp-reader", nil, func() {
		c.readLoop()
	})
	return c
}

// StartServer spawns a language server process and returns a client
// connected to its stdio. The process dies with Close.
func StartServer(cfg ServerConfig) (*Client, *exec.Cmd, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.RootDir
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("codeintel: lsp stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("codeintel: lsp stdout: %w", err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("codeintel: start lsp %s: %w", cfg.Command, err)
	}
	// Reap the process on exit so stopped servers never linger as
	// zombies; the wait is short-lived and guarded like every other
	// spawned goroutine (Section 3.2).
	safego.Go("codeintel/lsp-wait", nil, func() {
		_ = cmd.Wait()
	})
	rwc := &cmdStream{in: stdin, out: stdout, cmd: cmd}
	return NewPipeClient(rwc, cfg.RootDir), cmd, nil
}

type cmdStream struct {
	in  io.WriteCloser
	out io.ReadCloser
	cmd *exec.Cmd
}

func (s *cmdStream) Read(p []byte) (int, error)  { return s.out.Read(p) }
func (s *cmdStream) Write(p []byte) (int, error) { return s.in.Write(p) }
func (s *cmdStream) Close() error {
	_ = s.in.Close()
	_ = s.out.Close()
	return nil
}

// Initialize performs the LSP handshake for rootDir.
func (c *Client) Initialize(ctx context.Context, rootDir string) error {
	var result struct {
		Capabilities any `json:"capabilities"`
	}
	err := c.Call(ctx, "initialize", map[string]any{
		"processId":    nil,
		"rootUri":      dirToURI(rootDir),
		"capabilities": map[string]any{},
	}, &result)
	if err != nil {
		return err
	}
	return c.Notify(ctx, "initialized", map[string]any{})
}

// Call sends a request and decodes the result into out (may be nil).
func (c *Client) Call(ctx context.Context, method string, params any, out any) error {
	id := atomic.AddInt64(&c.nextID, 1)
	ch := make(chan rpcResult, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("codeintel: lsp client closed")
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out == nil || len(r.result) == 0 {
			return nil
		}
		if err := json.Unmarshal(r.result, out); err != nil {
			return fmt.Errorf("codeintel: decode %s: %w", method, err)
		}
		return nil
	}
}

// Notify sends a notification.
func (c *Client) Notify(_ context.Context, method string, params any) error {
	return c.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) write(msg rpcRequest) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("codeintel: encode %s: %w", msg.Method, err)
	}
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(raw))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("codeintel: lsp client closed")
	}
	if _, err := io.WriteString(c.rwc, frame); err != nil {
		return fmt.Errorf("codeintel: lsp write: %w", err)
	}
	if _, err := c.rwc.Write(raw); err != nil {
		return fmt.Errorf("codeintel: lsp write: %w", err)
	}
	return nil
}

func (c *Client) readLoop() {
	defer c.failAll(fmt.Errorf("codeintel: lsp stream closed"))
	br := bufio.NewReader(c.rwc)
	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}
		var msg rpcResponse
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		c.dispatch(msg)
	}
}

func readFrame(br *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			n, err := strconv.Atoi(strings.TrimSpace(line[len("content-length:"):]))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("codeintel: bad content-length %q", line)
			}
			length = n
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("codeintel: frame without content-length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) dispatch(msg rpcResponse) {
	if msg.ID != nil && msg.Method == "" {
		c.mu.Lock()
		ch, ok := c.pending[*msg.ID]
		c.mu.Unlock()
		if !ok {
			return
		}
		if msg.Error != nil {
			ch <- rpcResult{err: msg.Error}
		} else {
			ch <- rpcResult{result: msg.Result}
		}
		return
	}
	if msg.Method == "" {
		return
	}
	switch msg.Method {
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return
		}
		c.diagMu.Lock()
		c.diags[p.URI] = p.Diagnostics
		// Bound the cache: diagnostics are transient hints, not history.
		for len(c.diags) > 512 {
			for k := range c.diags {
				if k != p.URI {
					delete(c.diags, k)
					break
				}
			}
		}
		c.diagMu.Unlock()
	case "workspace/configuration":
		c.respond(msg.ID, []any{})
	case "client/registerCapability", "client/unregisterCapability":
		c.respond(msg.ID, nil)
	case "$/progress", "window/logMessage", "window/showMessage",
		"workspace/semanticTokens/refresh":
		// One-way server events: nothing to answer.
	default:
		c.respondError(msg.ID, -32601, "method not found: "+msg.Method)
	}
}

func (c *Client) respond(id *int64, result any) {
	if id == nil {
		return
	}
	raw, _ := json.Marshal(result)
	resp := map[string]any{"jsonrpc": "2.0", "id": *id, "result": json.RawMessage(raw)}
	framed, _ := json.Marshal(resp)
	_ = framed
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	body, _ := json.Marshal(resp)
	_, _ = io.WriteString(c.rwc, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))
	_, _ = c.rwc.Write(body)
}

func (c *Client) respondError(id *int64, code int, message string) {
	if id == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": *id,
		"error": map[string]any{"code": code, "message": message},
	})
	_, _ = io.WriteString(c.rwc, fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body)))
	_, _ = c.rwc.Write(body)
}

func (c *Client) failAll(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, ch := range c.pending {
		select {
		case ch <- rpcResult{err: err}:
		default:
		}
	}
}

// EnsureOpen opens (or reopens on content drift) a workspace file for
// LSP queries. The uri is derived from rootDir+rel.
func (c *Client) EnsureOpen(ctx context.Context, rootDir, rel string) (string, error) {
	abs := filepath.Join(rootDir, filepath.FromSlash(rel))
	src, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("codeintel: lsp open %s: %w", rel, err)
	}
	uri := dirToURI(rootDir) + "/" + uriEscapeRel(rel)
	version := len(src)
	c.openMu.Lock()
	cur, ok := c.opened[uri]
	c.openMu.Unlock()
	if ok && cur == version {
		return uri, nil
	}
	if ok {
		_ = c.Notify(ctx, "textDocument/didClose", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		})
	}
	err = c.Notify(ctx, "textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": langOfRel(rel),
			"version": version, "text": string(src),
		},
	})
	if err != nil {
		return "", err
	}
	c.openMu.Lock()
	c.opened[uri] = version
	c.openMu.Unlock()
	return uri, nil
}

// Definition returns definition locations of the symbol at pos
// (zero-based line/character) in rel.
func (c *Client) Definition(ctx context.Context, rootDir, rel string, line, ch int) ([]Location, error) {
	uri, err := c.EnsureOpen(ctx, rootDir, rel)
	if err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	err = c.Call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     Position{Line: line, Character: ch},
	}, &raw)
	if err != nil {
		return nil, err
	}
	var out []Location
	for _, r := range raw {
		var l Location
		if err := json.Unmarshal(r, &l); err == nil && l.URI != "" {
			out = append(out, l)
			continue
		}
		var link LocationLink
		if err := json.Unmarshal(r, &link); err == nil && link.TargetURI != "" {
			out = append(out, Location{URI: link.TargetURI, Range: link.TargetRange})
		}
	}
	return out, nil
}

// References returns reference locations of the symbol at pos.
func (c *Client) References(ctx context.Context, rootDir, rel string, line, ch int, includeDecl bool) ([]Location, error) {
	uri, err := c.EnsureOpen(ctx, rootDir, rel)
	if err != nil {
		return nil, err
	}
	var out []Location
	err = c.Call(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     Position{Line: line, Character: ch},
		"context":      map[string]any{"includeDeclaration": includeDecl},
	}, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DocumentSymbols returns the hierarchical symbols of rel.
func (c *Client) DocumentSymbols(ctx context.Context, rootDir, rel string) ([]DocumentSymbol, error) {
	uri, err := c.EnsureOpen(ctx, rootDir, rel)
	if err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if err := c.Call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &raw); err != nil {
		return nil, err
	}
	var out []DocumentSymbol
	for _, r := range raw {
		var flat SymbolInformation
		if err := json.Unmarshal(r, &flat); err == nil && flat.Name != "" && len(flat.Location.URI) > 0 {
			out = append(out, DocumentSymbol{
				Name: flat.Name, Kind: flat.Kind,
				Range: flat.Location.Range, SelectionRange: flat.Location.Range,
			})
			continue
		}
		var sym DocumentSymbol
		if err := json.Unmarshal(r, &sym); err == nil && sym.Name != "" {
			out = append(out, sym)
		}
	}
	return out, nil
}

// WorkspaceSymbols searches workspace symbols by query.
func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	var out []SymbolInformation
	if err := c.Call(ctx, "workspace/symbol", map[string]any{"query": query}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Rename computes the workspace edit of renaming the symbol at pos
// to newName. The tool reports the edit; the agent applies it with
// the file tools so history and leases stay consistent (13.6, 13.7).
func (c *Client) Rename(ctx context.Context, rootDir, rel string, line, ch int, newName string) (WorkspaceEdit, error) {
	uri, err := c.EnsureOpen(ctx, rootDir, rel)
	if err != nil {
		return WorkspaceEdit{}, err
	}
	var raw struct {
		Changes         map[string][]TextEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if err := c.Call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     Position{Line: line, Character: ch},
		"newName":      newName,
	}, &raw); err != nil {
		return WorkspaceEdit{}, err
	}
	edit := WorkspaceEdit{Changes: map[string][]TextEdit{}}
	for uri, edits := range raw.Changes {
		edit.Changes[uriToRel(rootDir, uri)] = append(edit.Changes[uriToRel(rootDir, uri)], edits...)
	}
	for _, dc := range raw.DocumentChanges {
		rel := uriToRel(rootDir, dc.TextDocument.URI)
		edit.Changes[rel] = append(edit.Changes[rel], dc.Edits...)
	}
	return edit, nil
}

// Diagnostics returns the latest published diagnostics for paths,
// formatted for tool results. It implements
// tools.DiagnosticProvider so edit tools can append them to their
// outcome (Sections 13.4, 13.6).
func (c *Client) Diagnostics(paths []string) string {
	c.diagMu.RLock()
	defer c.diagMu.RUnlock()
	var b strings.Builder
	shown := 0
	for _, p := range paths {
		uri := c.uriForPath(p)
		ds := c.diags[uri]
		if len(ds) == 0 {
			continue
		}
		sort.Slice(ds, func(i, j int) bool {
			if ds[i].Range.Start.Line != ds[j].Range.Start.Line {
				return ds[i].Range.Start.Line < ds[j].Range.Start.Line
			}
			return ds[i].Range.Start.Character < ds[j].Range.Start.Character
		})
		for _, d := range ds {
			if shown >= 30 {
				fmt.Fprintf(&b, "(+%d more diagnostics; open the file in an editor for the full list)\n", len(ds))
				break
			}
			fmt.Fprintf(&b, "%s:%d:%d: %s: %s\n", p,
				d.Range.Start.Line+1, d.Range.Start.Character+1,
				severityName(d.Severity), d.Message)
			shown++
		}
	}
	return b.String()
}

func (c *Client) uriForPath(p string) string {
	if strings.HasPrefix(p, "file://") {
		return p
	}
	// Match EnsureOpen's URI form: root + escaped relative path.
	for uri := range c.snapshotDiags() {
		if uriToRel("", uri) == p || strings.HasSuffix(uri, "/"+uriEscapeRel(p)) {
			return uri
		}
	}
	return p
}

func (c *Client) snapshotDiags() map[string][]Diagnostic {
	c.diagMu.RLock()
	defer c.diagMu.RUnlock()
	out := make(map[string][]Diagnostic, len(c.diags))
	for k, v := range c.diags {
		out[k] = v
	}
	return out
}

func severityName(s int) string {
	switch s {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "information"
	case 4:
		return "hint"
	default:
		return "diagnostic"
	}
}

// Shutdown runs the LSP shutdown/exit sequence and closes the stream.
func (c *Client) Shutdown(ctx context.Context) error {
	_ = c.Call(ctx, "shutdown", nil, nil)
	_ = c.Notify(ctx, "exit", nil)
	return c.Close()
}

// Close closes the transport and fails pending calls.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	c.failAll(fmt.Errorf("codeintel: lsp client closed"))
	return c.rwc.Close()
}

// Manager keeps one lazy language-server instance per project and
// language, shared by all agents, and stops instances idle past the
// TTL (Section 13.8, Appendix B: 10 minutes).
type Manager struct {
	mu       sync.Mutex
	configs  map[string]ServerConfig
	clients  map[string]*managedClient
	starting map[string]*inflight
	// IdleTTL overrides DefaultIdleTTL (tests use milliseconds).
	IdleTTL time.Duration
	// Start hooks process spawning (tests inject pipes).
	Start func(cfg ServerConfig) (*Client, func(), error)

	closed   bool
	reaped   chan struct{}
	stopReap chan struct{}
	reapOnce sync.Once
}

type managedClient struct {
	client *Client
	close  func()
	last   time.Time
}

// NewManager returns a Manager with DefaultIdleTTL.
func NewManager() *Manager {
	return &Manager{
		configs:  map[string]ServerConfig{},
		clients:  map[string]*managedClient{},
		starting: map[string]*inflight{},
		IdleTTL:  DefaultIdleTTL,
		Start: func(cfg ServerConfig) (*Client, func(), error) {
			cl, cmd, err := StartServer(cfg)
			if err != nil {
				return nil, nil, err
			}
			return cl, func() {
				_ = cl.Close()
				_ = cmd.Process.Kill()
			}, nil
		},
	}
}

// SetConfig registers (or replaces) the server for a language.
func (m *Manager) SetConfig(cfg ServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[cfg.Language] = cfg
}

func managerKey(project, lang string) string { return project + "\x00" + lang }

func (m *Manager) ttl() time.Duration {
	if m.IdleTTL <= 0 {
		return DefaultIdleTTL
	}
	return m.IdleTTL
}

// inflight tracks one lazy server start so concurrent For callers
// share it instead of spawning one server each.
type inflight struct {
	done   chan struct{}
	client *Client
	err    error
}

// For returns the shared client for project+language, starting the
// server lazily on the first use and handshaking it. Concurrent
// callers share the single in-flight start.
func (m *Manager) For(ctx context.Context, project, lang string) (*Client, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("codeintel: lsp manager closed")
	}
	key := managerKey(project, lang)
	if mc, ok := m.clients[key]; ok {
		mc.last = time.Now()
		cl := mc.client
		m.mu.Unlock()
		return cl, nil
	}
	cfg, ok := m.configs[lang]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("codeintel: no lsp server configured for %q", lang)
	}
	if f, ok := m.starting[key]; ok {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.done:
		}
		return f.client, f.err
	}
	f := &inflight{done: make(chan struct{})}
	m.starting[key] = f
	start := m.Start
	m.mu.Unlock()
	cfg.RootDir = project
	if err := cfg.Validate(); err != nil {
		m.finishStart(key, f, nil, err)
		return nil, err
	}
	cl, closer, err := start(cfg)
	if err != nil {
		m.finishStart(key, f, nil, err)
		return nil, err
	}
	if err := cl.Initialize(ctx, project); err != nil {
		closer()
		m.finishStart(key, f, nil, err)
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		delete(m.starting, key)
		m.mu.Unlock()
		closer()
		f.err = fmt.Errorf("codeintel: lsp manager closed")
		close(f.done)
		return nil, f.err
	}
	m.clients[key] = &managedClient{client: cl, close: closer, last: time.Now()}
	delete(m.starting, key)
	m.startReaperLocked()
	m.mu.Unlock()
	f.client = cl
	close(f.done)
	return cl, nil
}

// finishStart publishes a failed start to waiters.
func (m *Manager) finishStart(key string, f *inflight, cl *Client, err error) {
	m.mu.Lock()
	delete(m.starting, key)
	m.mu.Unlock()
	f.client = cl
	f.err = err
	close(f.done)
}

// Diagnostics aggregates the latest published diagnostics of every
// live server for paths. It satisfies the tools.DiagnosticProvider
// shape (Diagnostics([]string) string) so edit tools can append LSP
// diagnostics to their outcome (Sections 13.4, 13.6) without importing
// this package's client type directly.
func (m *Manager) Diagnostics(paths []string) string {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for _, mc := range m.clients {
		mc.last = time.Now()
		clients = append(clients, mc.client)
	}
	m.mu.Unlock()
	var b strings.Builder
	for _, cl := range clients {
		if s := cl.Diagnostics(paths); s != "" {
			b.WriteString(s)
		}
	}
	return b.String()
}

// Configured returns the sorted language keys with a registered
// server configuration.
func (m *Manager) Configured() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.configs))
	for lang := range m.configs {
		out = append(out, lang)
	}
	sort.Strings(out)
	return out
}

// Drop stops and forgets the server for project+language, if running.
func (m *Manager) Drop(project, lang string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := managerKey(project, lang)
	if mc, ok := m.clients[key]; ok {
		delete(m.clients, key)
		safego.Go("codeintel/lsp-stop", nil, mc.close)
	}
}

// Close stops every server and the reaper.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	all := make([]*managedClient, 0, len(m.clients))
	for _, mc := range m.clients {
		all = append(all, mc)
	}
	m.clients = map[string]*managedClient{}
	reaped := m.reaped
	stopReap := m.stopReap
	m.mu.Unlock()
	for _, mc := range all {
		mc.close()
	}
	if stopReap != nil {
		close(stopReap)
	}
	if reaped != nil {
		<-reaped
	}
}

func (m *Manager) startReaperLocked() {
	m.reapOnce.Do(func() {
		m.reaped = make(chan struct{})
		m.stopReap = make(chan struct{})
		ttl := m.ttl()
		tick := ttl / 4
		if tick < 10*time.Millisecond {
			tick = 10 * time.Millisecond
		}
		if tick > time.Minute {
			tick = time.Minute
		}
		ticker := time.NewTicker(tick)
		stop := m.stopReap
		safego.Go("codeintel/lsp-reaper", nil, func() {
			defer close(m.reaped)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					m.mu.Lock()
					if m.closed {
						m.mu.Unlock()
						return
					}
					now := time.Now()
					for key, mc := range m.clients {
						if now.Sub(mc.last) > m.ttl() {
							delete(m.clients, key)
							c := mc.close
							safego.Go("codeintel/lsp-stop", nil, c)
						}
					}
					m.mu.Unlock()
				}
			}
		})
	})
}

// URI helpers.

func dirToURI(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	u := url.URL{Scheme: "file", Path: abs}
	return strings.TrimSuffix(u.String(), "/")
}

func uriEscapeRel(rel string) string {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

func langOfRel(rel string) string {
	if lang, ok := DetectLanguage(rel); ok {
		return lang
	}
	return "plaintext"
}

// uriToRel maps a file URI back to a project-relative path when the
// URI sits under rootDir, else to the absolute path.
func uriToRel(rootDir, uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	p, err := url.PathUnescape(u.Path)
	if err != nil {
		return uri
	}
	rel, err := filepath.Rel(rootDir, filepath.FromSlash(p))
	if err != nil || strings.HasPrefix(rel, "..") {
		return p
	}
	return filepath.ToSlash(rel)
}
