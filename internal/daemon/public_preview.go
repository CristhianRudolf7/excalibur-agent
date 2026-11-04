package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const previewMaxBytes = 2 << 20

var previewURLPattern = regexp.MustCompile(`https://[a-z0-9]+(?:-[a-z0-9]+)*\.trycloudflare\.com`)

type publicPreview struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	LocalURL  string    `json:"local_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

type previewRequest struct {
	Action     string            `json:"action"`
	Files      map[string]string `json:"files,omitempty"`
	Path       string            `json:"path,omitempty"`
	Port       int               `json:"port,omitempty"`
	Confirmed  bool              `json:"confirmed,omitempty"`
	TTLSeconds int               `json:"ttl_seconds,omitempty"`
	ID         string            `json:"id,omitempty"`
}

// previewManager owns only resources created by this daemon. Nothing survives
// shutdown, and a caller-owned HTTP server is never stopped.
type previewManager struct {
	mu             sync.Mutex
	dataDir        string
	closed         bool
	active         map[string]*previewSession
	client         *http.Client
	startupTimeout time.Duration
	verifyTimeout  time.Duration
}

type previewSession struct {
	publicPreview
	mu          sync.Mutex
	once        sync.Once
	stopped     bool
	ready       bool
	run         string
	cancel      context.CancelFunc
	cmd         *exec.Cmd
	exited      chan struct{}
	finished    chan struct{}
	server      *http.Server
	root        *os.Root
	directory   string
	group       platform.ProcessGroup
	retainedJob bool
	manager     *previewManager
}

func newPreviewManager(dataDir string) *previewManager {
	return &previewManager{dataDir: dataDir, active: make(map[string]*previewSession),
		client:         previewHTTPClient(),
		startupTimeout: 40 * time.Second, verifyTimeout: 15 * time.Second}
}

func (m *previewManager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	sessions := make([]*previewSession, 0, len(m.active))
	for _, s := range m.active {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	for _, s := range sessions {
		s.stop()
	}
	m.client.CloseIdleConnections()
}

func (s *previewSession) stop() {
	s.once.Do(func() {
		s.cancel()
		s.mu.Lock()
		s.stopped = true
		s.manager.mu.Lock()
		delete(s.manager.active, s.ID)
		s.manager.mu.Unlock()
		if s.server != nil {
			_ = s.server.Close()
		}
		if s.cmd != nil {
			select {
			case <-s.exited:
				// A Windows Job remains owned after its leader exits. Closing it
				// releases the handle and stops descendants without a fresh PID lookup.
				if s.retainedJob {
					_ = s.group.Kill(s.cmd.Process.Pid)
				}
			default:
				_ = s.group.Kill(s.cmd.Process.Pid)
				<-s.exited
			}
		}
		if s.root != nil {
			_ = s.root.Close()
		}
		if s.directory != "" {
			_ = os.RemoveAll(s.directory)
		}
		s.mu.Unlock()
		close(s.finished)
	})
}

func (m *previewManager) list() []publicPreview {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []publicPreview{}
	for _, s := range m.active {
		if s.ready {
			out = append(out, s.publicPreview)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *previewManager) stopID(id string) error {
	m.mu.Lock()
	s := m.active[id]
	m.mu.Unlock()
	if s == nil {
		return fmt.Errorf("unknown preview id")
	}
	s.stop()
	return nil
}

// The output is ordinary Markdown, persisted and replayed by the existing chat
// message flow. Its title carries expiry metadata for the compact chat card.
func (m *previewManager) messageLinks(tree []string, text string) string {
	if m == nil {
		return text
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runs := make(map[string]bool, len(tree))
	for _, id := range tree {
		if id != "" {
			runs[id] = true
		}
	}
	for _, s := range m.active {
		if s.ready && runs[s.run] {
			link := fmt.Sprintf("[Public preview](%s \"public-preview:%s\")", s.URL, s.ExpiresAt.Format(time.RFC3339))
			if !strings.Contains(text, link) {
				text += "\n\n" + link
			}
		}
	}
	return text
}

func (m *previewManager) publish(ctx context.Context, req previewRequest, roots []string) (out publicPreview, err error) {
	sources := 0
	if req.Files != nil {
		sources++
	}
	if req.Path != "" {
		sources++
	}
	if req.Port != 0 {
		sources++
	}
	if sources != 1 {
		return out, fmt.Errorf("publish requires exactly one of files, path or port")
	}
	if req.Port != 0 && (!req.Confirmed || req.Port < 1 || req.Port > 65535) {
		return out, fmt.Errorf("publishing an existing 127.0.0.1 HTTP port requires a valid port and confirmed:true after user confirmation")
	}
	ttl := time.Duration(req.TTLSeconds) * time.Second
	if req.TTLSeconds < 0 || req.TTLSeconds > 86400 {
		return out, fmt.Errorf("ttl_seconds must be between 1 and 86400 (default 7200)")
	}
	if ttl == 0 {
		ttl = 2 * time.Hour
	}
	binary := os.Getenv("EXCALIBUR_CLOUDFLARED")
	if binary == "" {
		binary = "cloudflared"
	}
	binary, err = exec.LookPath(binary)
	if err != nil {
		return out, fmt.Errorf("cloudflared is missing: install it from https://developers.cloudflare.com/tunnel/downloads/ and add it to PATH, or set EXCALIBUR_CLOUDFLARED to its executable path")
	}
	ctx, cancel := context.WithTimeout(ctx, m.startupTimeout+m.verifyTimeout)
	defer cancel()
	s := &previewSession{publicPreview: publicPreview{ID: "preview_" + string(types.NewRunID())}, manager: m, cancel: cancel, exited: make(chan struct{}), finished: make(chan struct{}), group: previewProcessGroup()}
	s.run = tools.ExecutingRunID(ctx, "")
	success := false
	defer func() {
		if !success {
			s.stop()
		}
	}()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return out, fmt.Errorf("preview service is shutting down")
	}
	dataDir := m.dataDir
	m.active[s.ID] = s
	m.mu.Unlock()
	// Serialize preparation with stop so shutdown also covers an in-flight publish.
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return out, fmt.Errorf("preview stopped")
	}
	siteRoot := req.Path
	if req.Files != nil {
		siteRoot, err = writePreviewFiles(dataDir, req.Files)
		s.directory = siteRoot
	} else if siteRoot != "" {
		siteRoot, _, err = resolveWorkspaceFile(siteRoot, roots)
		if err == nil {
			err = validatePreviewDirectory(ctx, siteRoot)
		}
	}
	if err == nil && siteRoot != "" {
		s.root, err = os.OpenRoot(siteRoot)
		if err == nil {
			var listener net.Listener
			listener, err = listenPreview()
			if err == nil {
				s.LocalURL = "http://" + listener.Addr().String()
				s.server = &http.Server{Handler: previewFileHandler(s.root), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
				safego.Go("public-preview-http", nil, func() { _ = s.server.Serve(listener) })
			}
		}
	} else if err == nil {
		s.LocalURL = "http://127.0.0.1:" + strconv.Itoa(req.Port)
	}
	if err != nil {
		s.mu.Unlock()
		return out, err
	}
	urls := &previewURLWriter{found: make(chan string, 1)}
	s.cmd = exec.Command(binary, "tunnel", "--no-autoupdate", "--url", s.LocalURL)
	s.cmd.Stdout, s.cmd.Stderr = urls, urls
	s.group.Configure(s.cmd)
	if err = ctx.Err(); err == nil {
		err = s.cmd.Start()
	}
	if err != nil {
		s.cmd = nil
		s.mu.Unlock()
		return out, fmt.Errorf("start cloudflared: %w", err)
	}
	if jobs, ok := s.group.(interface{ AttachJob(int) error }); ok {
		err = jobs.AttachJob(s.cmd.Process.Pid)
		s.retainedJob = err == nil
	}
	safego.Go("public-preview-process", nil, func() { _ = s.cmd.Wait(); close(s.exited); s.stop() })
	s.mu.Unlock()
	if err != nil {
		return out, fmt.Errorf("own cloudflared process: %w", err)
	}
	timer := time.NewTimer(m.startupTimeout)
	defer timer.Stop()
	var publicURL string
	select {
	case publicURL = <-urls.found:
	case <-s.exited:
		return out, fmt.Errorf("cloudflared exited before publishing a preview URL")
	case <-ctx.Done():
		return out, fmt.Errorf("preview startup canceled: %w", ctx.Err())
	case <-timer.C:
		return out, fmt.Errorf("cloudflared did not provide a trycloudflare URL within the startup timeout")
	}
	if err = m.verify(ctx, publicURL); err != nil {
		return out, err
	}
	m.mu.Lock()
	select {
	case <-s.finished:
		err = fmt.Errorf("preview stopped during startup")
	default:
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if m.closed || m.active[s.ID] != s {
		err = fmt.Errorf("preview service is shutting down or preview stopped")
	}
	if err == nil {
		s.URL = publicURL
		s.ExpiresAt = time.Now().UTC().Add(ttl)
		s.ready = true
		out = s.publicPreview
	}
	m.mu.Unlock()
	if err != nil {
		return out, err
	}
	safego.Go("public-preview-expiry", nil, func() {
		timer := time.NewTimer(ttl)
		defer timer.Stop()
		select {
		case <-timer.C:
			s.stop()
		case <-s.finished:
		}
	})
	success = true
	return out, nil
}

func (m *previewManager) verify(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, m.verifyTimeout)
	defer cancel()
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := m.client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("public preview did not answer successfully before the verification timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type previewURLWriter struct {
	mu    sync.Mutex
	tail  string
	found chan string
}

func (w *previewURLWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := w.tail + string(data)
	if url := previewURLPattern.FindString(text); url != "" {
		select {
		case w.found <- url:
		default:
		}
	}
	if len(text) > 256 {
		text = text[len(text)-256:]
	}
	w.tail = text
	return len(data), nil
}

func writePreviewFiles(dataDir string, files map[string]string) (directory string, err error) {
	if dataDir == "" {
		return "", fmt.Errorf("daemon data directory is unavailable")
	}
	if len(files) == 0 || len(files) > 256 {
		return "", fmt.Errorf("files must contain between 1 and 256 site files")
	}
	total := 0
	for name, content := range files {
		if len(name) > 512 || !fs.ValidPath(name) || name == "." || strings.Contains(name, "\\") || !filepath.IsLocal(name) {
			return "", fmt.Errorf("site files require relative paths without traversal")
		}
		total += len(name) + len(content)
		if total > previewMaxBytes {
			return "", fmt.Errorf("site files exceed the 2 MiB total limit")
		}
	}
	parent := filepath.Join(dataDir, "previews")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	directory, err = os.MkdirTemp(parent, "site-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	for name, content := range files {
		target := filepath.Join(directory, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return directory, err
		}
		if err = os.WriteFile(target, []byte(content), 0600); err != nil {
			return directory, err
		}
	}
	return directory, nil
}

func validatePreviewDirectory(ctx context.Context, root string) error {
	st, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("preview path must be a directory")
	}
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(name)
			if err != nil {
				return fmt.Errorf("preview contains an invalid symlink")
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || !filepath.IsLocal(rel) {
				return fmt.Errorf("preview contains a symlink escaping its directory")
			}
		} else if !entry.IsDir() && !entry.Type().IsRegular() {
			return fmt.Errorf("preview contains a non-regular file")
		}
		return nil
	})
}

func listenPreview() (net.Listener, error) {
	for port := 5300; port <= 5399; port++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
		if err == nil {
			return listener, nil
		}
	}
	return nil, fmt.Errorf("no free preview port in 5300-5399")
}

func previewFileHandler(root *os.Root) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "."
		}
		st, err := root.Stat(filepath.FromSlash(name))
		if err == nil && st.IsDir() {
			name = path.Join(name, "index.html")
			st, err = root.Stat(filepath.FromSlash(name))
		}
		if err != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		f, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err = f.Stat()
		if err != nil || !st.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, path.Base(name), st.ModTime(), f)
	})
}

// Keep output structured and small: child output can contain local details and
// is used only to find the public hostname, never returned or logged.
func previewResult(value any, err error) (tools.Result, error) {
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	raw, err := json.Marshal(value)
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
}

var _ io.Writer = (*previewURLWriter)(nil)
