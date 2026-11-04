package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	platformfake "github.com/CristhianRudolf7/excalibur-agent/internal/platform/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type previewTransport func(*http.Request) (*http.Response, error)

func (f previewTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakePreviewBinary(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cloudflared uses a POSIX script")
	}
	name := filepath.Join(t.TempDir(), "cloudflared")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(name, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EXCALIBUR_CLOUDFLARED", name)
	return name
}
func previewTestManager(t *testing.T) *previewManager {
	t.Helper()
	fakePreviewBinary(t, `[ "$1" = tunnel ] && [ "$2" = --no-autoupdate ] && [ "$3" = --url ] || exit 9
printf 'https://test-preview.trycloudflare.com\n' >&2
exec sleep 120`)
	m := newPreviewManager(t.TempDir())
	m.client = &http.Client{Transport: previewTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://test-preview.trycloudflare.com" {
			t.Errorf("unexpected public check %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ready")), Header: make(http.Header)}, nil
	})}
	t.Cleanup(m.Close)
	return m
}
func previewPublish(t *testing.T, m *previewManager, req previewRequest) publicPreview {
	t.Helper()
	out, err := m.publish(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func previewGet(t *testing.T, url string) (int, string, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(data), response.Header.Get("Content-Type")
}

func TestPublicPreviewPublishListStop(t *testing.T) {
	m := previewTestManager(t)
	out := previewPublish(t, m, previewRequest{Files: map[string]string{"index.html": "<h1>Phone preview</h1>", "style.css": "body { color: red; }", "nested/index.html": "nested", "private/file.txt": "no listing"}})
	if out.ID == "" || out.URL != "https://test-preview.trycloudflare.com" || !strings.HasPrefix(out.LocalURL, "http://127.0.0.1:53") || time.Until(out.ExpiresAt) < 119*time.Minute {
		t.Fatal(out)
	}
	if got := m.list(); len(got) != 1 || got[0] != out {
		t.Fatal(got)
	}
	code, body, mime := previewGet(t, out.LocalURL+"/")
	if code != 200 || body != "<h1>Phone preview</h1>" || !strings.HasPrefix(mime, "text/html") {
		t.Fatal(code, body, mime)
	}
	_, _, mime = previewGet(t, out.LocalURL+"/style.css")
	if !strings.HasPrefix(mime, "text/css") {
		t.Fatal(mime)
	}
	code, body, _ = previewGet(t, out.LocalURL+"/nested/")
	if code != 200 || body != "nested" {
		t.Fatal(code, body)
	}
	code, _, _ = previewGet(t, out.LocalURL+"/private/")
	if code != 404 {
		t.Fatal("directory listing", code)
	}
	m.mu.Lock()
	directory := m.active[out.ID].directory
	m.mu.Unlock()
	if err := m.stopID(out.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.list()) != 0 {
		t.Fatal(m.list())
	}
	if _, err := http.Get(out.LocalURL); err == nil {
		t.Fatal("server still alive")
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("site directory not removed", err)
	}
	if m.stopID(out.ID) == nil {
		t.Fatal("unknown id accepted")
	}
}
func TestPublicPreviewTTLAndShutdown(t *testing.T) {
	m := previewTestManager(t)
	out := previewPublish(t, m, previewRequest{Files: map[string]string{"index.html": "ok"}, TTLSeconds: 1})
	deadline := time.Now().Add(4 * time.Second)
	for len(m.list()) != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(m.list()) != 0 {
		t.Fatal("TTL did not stop preview")
	}
	if _, err := http.Get(out.LocalURL); err == nil {
		t.Fatal("expired server still alive")
	}
	previewPublish(t, m, previewRequest{Files: map[string]string{"index.html": "ok"}, TTLSeconds: 86400})
	d := New(Config{}, Deps{Services: &Services{previews: m}})
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(m.list()) != 0 {
		t.Fatal("shutdown left previews")
	}
	if _, err := m.publish(context.Background(), previewRequest{Files: map[string]string{"index.html": "ok"}}, nil); err == nil {
		t.Fatal("published after shutdown")
	}
}
func TestPublicPreviewInputsAndPaths(t *testing.T) {
	m := previewTestManager(t)
	for _, req := range []previewRequest{
		{}, {Files: map[string]string{}}, {Files: map[string]string{"index.html": "ok"}, Port: 5300, Confirmed: true},
		{Port: 5300}, {Port: 65536, Confirmed: true}, {Port: -1, Confirmed: true},
		{Files: map[string]string{"../secret": "no"}}, {Files: map[string]string{"/absolute": "no"}},
		{Files: map[string]string{"a\\b": "no"}}, {Files: map[string]string{"index.html": strings.Repeat("x", previewMaxBytes)}},
		{Files: map[string]string{"index.html": "ok"}, TTLSeconds: -1}, {Files: map[string]string{"index.html": "ok"}, TTLSeconds: 86401},
		{Path: t.TempDir()},
	} {
		if _, err := m.publish(context.Background(), req, nil); err == nil {
			t.Fatalf("accepted invalid input %+v", req.Action)
		}
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("project"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := m.publish(context.Background(), previewRequest{Path: root}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := previewGet(t, out.LocalURL)
	if body != "project" {
		t.Fatal(body)
	}
	if err = m.stopID(out.ID); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err = os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err = m.publish(context.Background(), previewRequest{Path: root}, []string{root}); err == nil {
		t.Fatal("escaping symlink allowed")
	}
	if _, err = m.publish(context.Background(), previewRequest{Path: filepath.Join(root, "escape")}, []string{root}); err == nil {
		t.Fatal("escaping root symlink allowed")
	}
	if err = os.Remove(filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	out, err = m.publish(context.Background(), previewRequest{Path: root}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	// A symlink introduced after publication is also contained by os.Root.
	if err = os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	code, _, _ := previewGet(t, out.LocalURL+"/escape/secret")
	if code != 404 {
		t.Fatal("runtime symlink escape", code)
	}
	m.Close()
	if _, err = os.Stat(root); err != nil {
		t.Fatal("caller-owned directory was removed")
	}
}
func TestPublicPreviewExistingPortSurvivesStop(t *testing.T) {
	m := previewTestManager(t)
	listener, err := listenPreview()
	if err != nil {
		t.Fatal(err)
	}
	server := &httptest.Server{Listener: listener, Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "app") })}}
	server.Start()
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	out := previewPublish(t, m, previewRequest{Port: port, Confirmed: true})
	if err = m.stopID(out.ID); err != nil {
		t.Fatal(err)
	}
	code, body, _ := previewGet(t, server.URL)
	if code != 200 || body != "app" {
		t.Fatal(code, body)
	}
}
func TestPublicPreviewMissingBinaryAndFailures(t *testing.T) {
	m := previewTestManager(t)
	req := previewRequest{Files: map[string]string{"index.html": "ok"}}
	t.Setenv("EXCALIBUR_CLOUDFLARED", filepath.Join(t.TempDir(), "missing"))
	if _, err := m.publish(context.Background(), req, nil); err == nil || !strings.Contains(err.Error(), "install it") {
		t.Fatal(err)
	}
	fakePreviewBinary(t, "exit 1")
	if _, err := m.publish(context.Background(), req, nil); err == nil || !strings.Contains(err.Error(), "exited") {
		t.Fatal(err)
	}
	fakePreviewBinary(t, "exec sleep 120")
	m.startupTimeout = 40 * time.Millisecond
	if _, err := m.publish(context.Background(), req, nil); err == nil {
		t.Fatal("startup timeout missing")
	}
	if len(m.active) != 0 {
		t.Fatal("failed publish leaked resources")
	}
}
func TestPublicPreviewVerificationRetriesAndCancellation(t *testing.T) {
	m := previewTestManager(t)
	var attempts atomic.Int32
	m.client.Transport = previewTransport(func(*http.Request) (*http.Response, error) {
		status := 503
		if attempts.Add(1) >= 3 {
			status = 200
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("ready")), Header: make(http.Header)}, nil
	})
	previewPublish(t, m, previewRequest{Files: map[string]string{"index.html": "ok"}})
	if attempts.Load() != 3 {
		t.Fatal(attempts.Load())
	}
	m.verifyTimeout = 40 * time.Millisecond
	attempts.Store(-100)
	if _, err := m.publish(context.Background(), previewRequest{Files: map[string]string{"index.html": "ok"}}, nil); err == nil {
		t.Fatal("unverified URL returned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.publish(ctx, previewRequest{Files: map[string]string{"index.html": "ok"}}, nil); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestPublicPreviewChatSurfaceAndLinks(t *testing.T) {
	calls := 0
	b, s := assistantTestWiring(t, false, func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if !slices.ContainsFunc(req.Tools, func(d providers.ToolDefinition) bool { return d.Name == "public_preview" }) {
			t.Error("chat omitted public_preview")
		}
		if calls == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "preview-list", Name: "public_preview", Arguments: json.RawMessage(`{"action":"list"}`)}}}}, nil
		}
		return textReply("No active previews."), nil
	})
	tl, err := s.Tools.Get("public_preview")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Catalog.IsDeferred("public_preview") || !slices.Equal(tl.Definition().Profiles, []string{"default"}) {
		t.Fatal(tl.Definition())
	}
	hash := s.Catalog.PrefixHash()
	if err = wirePublicPreviewTool(b, s); err != nil || s.Catalog.PrefixHash() != hash {
		t.Fatal("prefix moved", err)
	}
	// Tool selection must survive broad creation requests in both languages.
	for _, request := range []string{"Crea un proyecto con una página web pequeña y dame un enlace público para abrirla desde mi celular", "Create a small page and give me a public link for my phone"} {
		activation := s.chatToolActivation(request, "test", nil)
		if !slices.Contains(activation.Names, "public_preview") {
			t.Fatal("preview not selected", request)
		}
	}
	a := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	if _, _, err = a.CompleteTurn(context.Background(), "cnv_preview", master.Normalized{Text: "Give me a public link for my phone"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}

func TestPublicPreviewMessageDelivery(t *testing.T) {
	m := previewTestManager(t)
	req := previewRequest{Files: map[string]string{"index.html": "ok"}}
	out, err := m.publish(tools.WithSourceRun(context.Background(), "preview-run"), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.messageLinks([]string{"other"}, "answer"); got != "answer" {
		t.Fatal("cross-conversation link", got)
	}
	b, _ := assistantTestWiring(t, false, nil)
	b.previews = m
	event, err := b.Master.SendAssistant(context.Background(), "cnv_preview", "Created your page.", []string{"preview-run"})
	if err != nil {
		t.Fatal(err)
	}
	text := event.Text
	if !strings.Contains(text, out.URL) || !strings.Contains(text, "public-preview:"+out.ExpiresAt.Format(time.RFC3339)) {
		t.Fatal(text)
	}
	if got := m.messageLinks([]string{"preview-run"}, text); got != text {
		t.Fatal("duplicate announcement", got)
	}
}

func TestPublicPreviewShutdownDuringStartup(t *testing.T) {
	m := previewTestManager(t)
	fakePreviewBinary(t, "exec sleep 120")
	result := make(chan error, 1)
	safego.Go("preview-startup-test", nil, func() {
		_, err := m.publish(context.Background(), previewRequest{Files: map[string]string{"index.html": "ok"}}, nil)
		result <- err
	})
	deadline := time.Now().Add(5 * time.Second)
	started := false
	for !started && time.Now().Before(deadline) {
		m.mu.Lock()
		var session *previewSession
		for _, s := range m.active {
			session = s
			break
		}
		m.mu.Unlock()
		if session != nil {
			session.mu.Lock()
			started = session.cmd != nil
			session.mu.Unlock()
		}
		if !started {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !started {
		t.Fatal("preview process did not start")
	}
	m.Close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("startup succeeded during shutdown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("startup was not interrupted")
	}
	m.mu.Lock()
	active := len(m.active)
	m.mu.Unlock()
	if active != 0 {
		t.Fatal("shutdown leaked a pending preview")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := validatePreviewDirectory(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal("path validation ignored interruption", err)
	}
}

func TestPublicPreviewPublishFromChatReachesFinalMessage(t *testing.T) {
	fakePreviewBinary(t, `printf 'https://test-preview.trycloudflare.com\n' >&2
exec sleep 120`)
	calls := 0
	b, s := assistantTestWiring(t, false, func(_ context.Context, request providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "publish-page", Name: "public_preview", Arguments: json.RawMessage(`{"action":"publish","files":{"index.html":"<h1>A page for your phone</h1>"}}`)}}}}, nil
		}
		found := false
		for _, message := range request.Messages {
			if message.ToolResult != nil && message.ToolResult.CallID == "publish-page" {
				var published publicPreview
				if err := json.Unmarshal([]byte(message.ToolResult.Output), &published); err != nil || published.URL != "https://test-preview.trycloudflare.com" {
					t.Fatal("chat did not receive a published URL", err)
				}
				found = true
			}
		}
		if !found {
			t.Error("publish result absent from model context")
		}
		return textReply("Created your page."), nil
	})
	m := s.previews
	m.dataDir = t.TempDir()
	m.client = &http.Client{Transport: previewTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ready"))}, nil
	})}
	t.Cleanup(m.Close)
	turn := &assistantTurns{backend: b, services: s, endpoint: s.engineOptions.Models, modelID: "test"}
	text, tree, err := turn.CompleteTurn(context.Background(), "cnv_phone", master.Normalized{Text: "Create a small web page and give me a public link for my phone."})
	if err != nil || calls != 2 {
		t.Fatal("chat publish failed", calls, err)
	}
	event, err := b.Master.SendAssistant(context.Background(), "cnv_phone", text, tree)
	if err != nil {
		t.Fatal(err)
	}
	if event.MessageID == "" || !strings.Contains(event.Text, `[Public preview](https://test-preview.trycloudflare.com "public-preview:`) {
		t.Fatal("published URL missing from final persisted message", event.Text)
	}
	previews := m.list()
	if len(previews) != 1 {
		t.Fatal(previews)
	}
	code, body, _ := previewGet(t, previews[0].LocalURL)
	if code != 200 || body != "<h1>A page for your phone</h1>" {
		t.Fatal(code, body)
	}
}

func TestPublicPreviewReleasesOwnedJobAfterProcessExit(t *testing.T) {
	binary := fakePreviewBinary(t, "exit 0")
	cmd := exec.Command(binary)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	m := newPreviewManager(t.TempDir())
	defer m.Close()
	group := &platformfake.ProcessGroup{}
	_, cancel := context.WithCancel(context.Background())
	session := &previewSession{publicPreview: publicPreview{ID: "exited-preview"}, cmd: cmd, group: group, retainedJob: true, manager: m, cancel: cancel, exited: make(chan struct{}), finished: make(chan struct{})}
	close(session.exited)
	m.active[session.ID] = session
	if err := m.stopID(session.ID); err != nil {
		t.Fatal(err)
	}
	session.stop()
	if !slices.Equal(group.Killed, []int{cmd.Process.Pid}) {
		t.Fatal("owned job was not released exactly once", group.Killed)
	}
	if len(m.list()) != 0 {
		t.Fatal("exited preview remained listed")
	}
}
