// Package browser implements the browser capability using a persistent Chrome profile.
package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BlobStore stores full captures and downloads outside inline tool output.
type BlobStore interface {
	Put(ctx context.Context, kind string, data []byte) (string, error)
}

// SecretResolver resolves a secret only for a permitted destination host.
type SecretResolver interface {
	Resolve(ctx context.Context, name, host string) (string, error)
}

// Config owns one persistent profile. Nil boolean options use safe defaults.
type Config struct {
	ProfileDir        string
	DownloadsDir      string
	RoutinesDir       string
	AllowedUploadDirs []string
	Headless          *bool
	Sandbox           *bool
	Channel           string
	IdleTimeout       time.Duration
	MaxTabs           int
	Blobs             BlobStore
	Secrets           SecretResolver
	Clock             func() time.Time
}

func (c Config) defaults() Config {
	if c.Headless == nil {
		c.Headless = boolPtr(true)
	}
	if c.Sandbox == nil {
		c.Sandbox = boolPtr(true)
	}
	// Copy pointed-to values: callers cannot change a running engine's policy.
	c.Headless, c.Sandbox = boolPtr(*c.Headless), boolPtr(*c.Sandbox)
	c.AllowedUploadDirs = append([]string(nil), c.AllowedUploadDirs...)
	if c.Channel == "" {
		c.Channel = "chrome"
	}
	if c.ProfileDir == "" {
		root, err := os.UserConfigDir()
		if err == nil {
			c.ProfileDir = filepath.Join(root, "excalibur", "browser", "profile")
		}
	}
	if c.DownloadsDir == "" {
		c.DownloadsDir = filepath.Join(filepath.Dir(c.ProfileDir), "downloads")
	}
	if c.RoutinesDir == "" {
		c.RoutinesDir = filepath.Join(filepath.Dir(c.ProfileDir), "routines")
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 5 * time.Minute
	}
	if c.MaxTabs <= 0 {
		c.MaxTabs = 16
	}
	if c.Blobs == nil {
		c.Blobs = &MemoryBlobs{}
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	return c
}
func boolPtr(b bool) *bool { return &b }
func allowedURL(raw string) error {
	if raw == "about:blank" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("browser: blocked URL scheme or credentials (http(s) and about:blank only)")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return errors.New("browser: invalid URL")
	}
	return nil
}
func allowedUpload(path string, roots []string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", errors.New("browser: upload file unavailable")
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("browser: upload must be a regular file")
	}
	for _, root := range roots {
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return resolved, nil
		}
	}
	return "", errors.New("browser: upload blocked by policy")
}
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted URL]"
	}
	u.User = nil
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		lower := strings.ToLower(k)
		for _, marker := range []string{"key", "token", "secret", "password", "auth", "credential", "session", "code", "signature"} {
			if strings.Contains(lower, marker) {
				q.Set(k, "[redacted]")
				break
			}
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// MemoryBlobs is a concurrency-safe content-addressed store for tests and embedding.
type MemoryBlobs struct {
	mu   sync.Mutex
	Data map[string][]byte
}

func (m *MemoryBlobs) Put(ctx context.Context, kind string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	ref := "blob:" + kind + ":" + hex.EncodeToString(sum[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Data == nil {
		m.Data = map[string][]byte{}
	}
	m.Data[ref] = append([]byte(nil), data...)
	return ref, nil
}

// Get returns a copy of a stored blob.
func (m *MemoryBlobs) Get(ref string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.Data[ref]...)
}

// Secret binds a test credential to exact host names, without implicit subdomains.
type Secret struct {
	Value string
	Hosts []string
}

// MemorySecrets is immutable after construction; errors never include credentials.
type MemorySecrets map[string]Secret

func (m MemorySecrets) Resolve(ctx context.Context, name, host string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s, ok := m[name]
	if ok {
		for _, h := range s.Hosts {
			if strings.EqualFold(h, host) {
				return s.Value, nil
			}
		}
	}
	return "", errors.New("browser: secret unavailable for this host")
}

func (e *Engine) safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[redacted URL]"
	}
	u.Path = e.clean(u.Path)
	u.RawPath = ""
	q := u.Query()
	for key, values := range q {
		for i, value := range values {
			values[i] = e.clean(value)
		}
		q[key] = values
	}
	u.RawQuery = q.Encode()
	return redactURL(u.String())
}
