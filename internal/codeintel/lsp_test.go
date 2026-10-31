package codeintel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeServer is a scripted LSP server over one end of a net.Pipe for
// tests: ordinary tests never spawn real servers (Section 13.8 is
// covered against simulations of the contract).
type fakeServer struct {
	t       *testing.T
	conn    net.Conn
	mu      sync.Mutex
	root    string
	defLoc  Location
	wg      sync.WaitGroup
	onOpen  func(uri string)
	timeout time.Duration
}

func startFake(t *testing.T, root string) (*fakeServer, net.Conn) {
	t.Helper()
	sc, cc := net.Pipe()
	fs := &fakeServer{t: t, conn: sc, root: root, timeout: 5 * time.Second}
	fs.defLoc = Location{
		URI:   dirToURI(root) + "/target.go",
		Range: Range{Start: Position{Line: 3, Character: 5}, End: Position{Line: 3, Character: 9}},
	}
	fs.wg.Add(1)
	go fs.serve()
	t.Cleanup(func() {
		_ = sc.Close()
		_ = cc.Close()
	})
	return fs, cc
}

func (f *fakeServer) serve() {
	defer f.wg.Done()
	br := bufio.NewReader(f.conn)
	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}
		var msg rpcResponse
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		f.handle(msg)
	}
}

func (f *fakeServer) send(v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = f.conn.SetWriteDeadline(time.Now().Add(f.timeout))
	_, _ = fmt.Fprintf(f.conn, "Content-Length: %d\r\n\r\n", len(raw))
	_, _ = f.conn.Write(raw)
	_ = f.conn.SetWriteDeadline(time.Time{})
}

func (f *fakeServer) handle(msg rpcResponse) {
	if msg.ID == nil {
		// Notification.
		if msg.Method == "textDocument/didOpen" {
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if f.onOpen != nil {
				f.onOpen(p.TextDocument.URI)
			}
			f.send(map[string]any{
				"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
				"params": map[string]any{
					"uri": p.TextDocument.URI,
					"diagnostics": []any{map[string]any{
						"range": map[string]any{
							"start": map[string]any{"line": 0, "character": 0},
							"end":   map[string]any{"line": 0, "character": 5},
						},
						"severity": 1,
						"message":  "undefined: x",
					}},
				},
			})
		}
		return
	}
	id := *msg.ID
	reply := func(result any) {
		f.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(code int, message string) {
		f.send(map[string]any{"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": code, "message": message}})
	}
	switch msg.Method {
	case "initialize":
		reply(map[string]any{"capabilities": map[string]any{}})
	case "shutdown":
		reply(nil)
	case "workspace/configuration":
		reply([]any{})
	case "client/registerCapability", "client/unregisterCapability":
		reply(nil)
	case "textDocument/definition":
		reply([]Location{f.defLoc})
	case "textDocument/references":
		reply([]Location{f.defLoc, {
			URI:   dirToURI(f.root) + "/other.go",
			Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 4}},
		}})
	case "textDocument/documentSymbol":
		reply([]DocumentSymbol{{
			Name: "Target", Kind: 5,
			Range:          Range{Start: Position{Line: 3}, End: Position{Line: 9}},
			SelectionRange: Range{Start: Position{Line: 3, Character: 5}, End: Position{Line: 3, Character: 9}},
			Children: []DocumentSymbol{{
				Name: "helper", Kind: 12,
				Range:          Range{Start: Position{Line: 4}, End: Position{Line: 6}},
				SelectionRange: Range{Start: Position{Line: 4, Character: 5}, End: Position{Line: 4, Character: 11}},
			}},
		}})
	case "workspace/symbol":
		reply([]SymbolInformation{{
			Name:     "Target",
			Kind:     5,
			Location: f.defLoc,
		}})
	case "textDocument/rename":
		reply(map[string]any{
			"changes": map[string]any{
				f.defLoc.URI: []any{map[string]any{
					"range": map[string]any{
						"start": map[string]any{"line": 3, "character": 5},
						"end":   map[string]any{"line": 3, "character": 9},
					},
					"newText": "Renamed",
				}},
			},
			"documentChanges": []any{map[string]any{
				"textDocument": map[string]any{"uri": dirToURI(f.root) + "/second.go"},
				"edits": []any{map[string]any{
					"range": map[string]any{
						"start": map[string]any{"line": 0, "character": 0},
						"end":   map[string]any{"line": 0, "character": 6},
					},
					"newText": "Renamed",
				}},
			}},
		})
	default:
		fail(-32601, "method not found: "+msg.Method)
	}
}

func lspTestProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\n\nfunc main() {\n\tTarget()\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "target.go"),
		[]byte("package main\n\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLSPClientRoundTrip(t *testing.T) {
	root := lspTestProject(t)
	_, cc := startFake(t, root)
	ctx := context.Background()
	cl := NewPipeClient(cc, root)
	defer cl.Close()
	if err := cl.Initialize(ctx, root); err != nil {
		t.Fatal(err)
	}
	defs, err := cl.Definition(ctx, root, "main.go", 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || !strings.HasSuffix(uriToRel(root, defs[0].URI), "target.go") {
		t.Fatalf("definitions: %+v", defs)
	}
	refs, err := cl.References(ctx, root, "main.go", 3, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("references: %+v", refs)
	}
	syms, err := cl.DocumentSymbols(ctx, root, "target.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(syms) != 1 || syms[0].Name != "Target" || len(syms[0].Children) != 1 {
		t.Fatalf("symbols: %+v", syms)
	}
	ws, err := cl.WorkspaceSymbols(ctx, "Tar")
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].Name != "Target" {
		t.Fatalf("workspace symbols: %+v", ws)
	}
	edit, err := cl.Rename(ctx, root, "main.go", 3, 1, "Renamed")
	if err != nil {
		t.Fatal(err)
	}
	// Both the changes map and documentChanges convert to
	// project-relative paths.
	if len(edit.Changes) != 2 {
		t.Fatalf("rename edit: %+v", edit.Changes)
	}
	// Diagnostics arrive as a push after didOpen; poll briefly.
	var diag string
	for i := 0; i < 50; i++ {
		diag = cl.Diagnostics([]string{"main.go"})
		if diag != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(diag, "main.go:1:1") || !strings.Contains(diag, "undefined: x") {
		t.Fatalf("diagnostics: %q", diag)
	}
	// Unknown methods surface as errors, not hangs.
	if err := cl.Call(ctx, "nope/missing", nil, nil); err == nil {
		t.Fatal("expected method-not-found error")
	}
	if err := cl.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLSPManagerLazySharedAndReaped(t *testing.T) {
	root := lspTestProject(t)
	m := NewManager()
	m.IdleTTL = 120 * time.Millisecond
	defer m.Close()
	starts := 0
	var mu sync.Mutex
	m.Start = func(cfg ServerConfig) (*Client, func(), error) {
		mu.Lock()
		starts++
		mu.Unlock()
		if err := cfg.Validate(); err != nil {
			return nil, nil, err
		}
		_, cc := startFake(mtT(t), cfg.RootDir)
		cl := NewPipeClient(cc, cfg.RootDir)
		return cl, func() { _ = cl.Close() }, nil
	}
	m.SetConfig(ServerConfig{Language: "go", Command: "fake-ls"})
	ctx := context.Background()
	// Concurrent For shares one instance (single lazy start).
	var wg sync.WaitGroup
	got := make([]*Client, 8)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cl, err := m.For(ctx, root, "go")
			if err != nil {
				t.Error(err)
				return
			}
			got[i] = cl
		}(i)
	}
	wg.Wait()
	for i := 1; i < len(got); i++ {
		if got[i] != got[0] || got[0] == nil {
			t.Fatal("manager did not share the instance")
		}
	}
	mu.Lock()
	if starts != 1 {
		t.Fatalf("starts=%d want 1", starts)
	}
	mu.Unlock()
	// Idle past the TTL reaps the server; the next For restarts it.
	time.Sleep(500 * time.Millisecond)
	if _, err := m.For(ctx, root, "go"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if starts != 2 {
		t.Fatalf("starts=%d want 2 after reap", starts)
	}
	mu.Unlock()
	// Unknown language is a configuration error, not a spawn.
	if _, err := m.For(ctx, root, "cobol"); err == nil {
		t.Fatal("expected no-server error")
	}
	m.Drop(root, "go")
	if _, err := m.For(ctx, root, "go"); err != nil {
		t.Fatal(err)
	}
}

// mtT adapts *testing.T for the fake starter (same test).
func mtT(t *testing.T) *testing.T { return t }

func TestLSPServerConfigValidate(t *testing.T) {
	if err := (ServerConfig{}).Validate(); err == nil {
		t.Error("empty config must fail")
	}
	if err := (ServerConfig{Language: "go"}).Validate(); err == nil {
		t.Error("missing command must fail")
	}
	if err := (ServerConfig{Language: "go", Command: "gopls"}).Validate(); err == nil {
		t.Error("missing root must fail")
	}
	if err := (ServerConfig{Language: "go", Command: "gopls", RootDir: "/x"}).Validate(); err != nil {
		t.Errorf("valid config: %v", err)
	}
}

func TestLSPURIHelpers(t *testing.T) {
	root := t.TempDir()
	uri := dirToURI(root)
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("uri: %s", uri)
	}
	rel := uriToRel(root, uri+"/a b.go")
	if rel != "a b.go" {
		t.Fatalf("rel: %q", rel)
	}
	// Outside the root falls back to the absolute path.
	if got := uriToRel(root, "file:///elsewhere/x.go"); got != "/elsewhere/x.go" {
		t.Fatalf("outside: %q", got)
	}
	// Non-file URIs pass through.
	if got := uriToRel(root, "untitled:x"); got != "untitled:x" {
		t.Fatalf("passthrough: %q", got)
	}
}

func TestLSPReadFrameErrors(t *testing.T) {
	for _, input := range []string{
		"Content-Length: nope\r\n\r\n",
		"X-Other: 1\r\n\r\n",
	} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(input))); err == nil {
			t.Errorf("expected error for %q", input)
		}
	}
	// Short body errors.
	if _, err := readFrame(bufio.NewReader(strings.NewReader("Content-Length: 100\r\n\r\nshort"))); err != io.ErrUnexpectedEOF && err != io.EOF {
		t.Logf("short body err: %v", err)
	}
}
