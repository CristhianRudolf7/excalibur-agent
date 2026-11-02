package websearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Route a public test address to a local server after exercising the production
// DNS/IP checks. No guard bypass is exposed in Config.
func localReader(t *testing.T, h http.HandlerFunc) (*http.Client, *addressGuard) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	g := &addressGuard{lookup: func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "public.test" || host == "93.184.215.14" {
			return []net.IPAddr{{IP: net.ParseIP("93.184.215.14")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}, dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		if !strings.HasPrefix(address, "93.184.215.14:") {
			return nil, errors.New("unvalidated dial")
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(s.URL, "http://"))
	}}
	return guardedClient(*g, time.Second), g
}
func readerBackend(t *testing.T, h http.HandlerFunc) *Backend {
	t.Helper()
	b := newTestBackend(t, nil)
	b.reader, _ = localReader(t, h)
	return b
}
func TestReadArticle(t *testing.T) {
	b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Testing articles</title></head><body><nav>Menu</nav><article><h1>Article</h1><p>`+strings.Repeat("A useful article about Go interfaces and bounded HTTP reading. ", 12)+`</p><a href="/more">More</a></article><script>SECRET SCRIPT</script></body></html>`)
	})
	p, err := b.Read(context.Background(), "http://public.test/article")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Testing articles" || !strings.Contains(p.Markdown, "useful article") || strings.Contains(p.Markdown, "SECRET SCRIPT") || p.ReadAt == "" || p.FinalURL != "http://public.test/article" {
		t.Fatalf("article: %+v", p)
	}
	sum := sha256.Sum256([]byte(p.Markdown))
	if p.ContentHash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("content hash")
	}
	if _, err = time.Parse(time.RFC3339, p.ReadAt); err != nil {
		t.Fatal(err)
	}
	b.limits.MarkdownBytes = 90
	p, err = b.Read(context.Background(), "http://public.test/article")
	if err != nil || len(p.Markdown) > 90 || !strings.HasSuffix(p.Markdown, truncationMarker) {
		t.Fatal("truncation", p, err)
	}
}
func TestReadContentTypes(t *testing.T) {
	for _, tc := range []struct {
		kind, body, want string
		fails            bool
	}{
		{"text/plain", "hello\nworld", "hello\nworld", false}, {"text/markdown", "# Heading", "# Heading", false},
		{"application/json", `{"x":1}`, "{\n  \"x\": 1\n}", false}, {"application/problem+json", `{"x":1}`, "{\n  \"x\": 1\n}", false},
		{"application/pdf", "%PDF-1.7", "unsupported content type", true}, {"application/octet-stream", "\x00\x01", "unsupported content type", true},
		{"application/json", "broken", "invalid JSON", true},
	} {
		t.Run(tc.kind+tc.body[:1], func(t *testing.T) {
			b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.kind)
				fmt.Fprint(w, tc.body)
			})
			p, err := b.Read(context.Background(), "http://public.test/a")
			if tc.fails {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatal(err)
				}
			} else if err != nil || p.Markdown != tc.want {
				t.Fatal(p, err)
			}
		})
	}
}
func TestReadLimitsRedirects(t *testing.T) {
	b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/private":
			http.Redirect(w, r, "http://127.0.0.1/secret", http.StatusFound)
		case "/private-dns":
			http.Redirect(w, r, "http://private.test/secret", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/blocked":
			http.Redirect(w, r, "https://tbench.ai/tasks", http.StatusFound)
		case "/oversize":
			fmt.Fprint(w, strings.Repeat("a", 101))
		case "/error":
			w.WriteHeader(500)
		default:
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "final")
		}
	})
	p, err := b.Read(context.Background(), "http://public.test/redirect")
	if err != nil || p.FinalURL != "http://public.test/final" {
		t.Fatal(p, err)
	}
	for _, path := range []string{"private", "private-dns", "loop", "blocked", "error"} {
		if _, err = b.Read(context.Background(), "http://public.test/"+path); err == nil {
			t.Fatal("accepted", path)
		}
	}
	if _, err = b.Read(context.Background(), "http://public.test/blocked"); err == nil || !strings.HasPrefix(err.Error(), "blocked: benchmark source") {
		t.Fatal("redirect lost benchmark denial", err)
	}
	b.limits.PageBytes = 100
	if _, err = b.Read(context.Background(), "http://public.test/oversize"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatal(err)
	}
}
func TestSSRF(t *testing.T) {
	b := newTestBackend(t, nil)
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com", "http://user:password@example.com", "http://127.0.0.1", "http://[::1]", "http://10.1.2.3", "http://172.16.0.1", "http://192.168.0.1", "http://169.254.169.254", "http://100.100.100.200", "http://0.0.0.0", "http://[::ffff:127.0.0.1]", "http://[fd00:ec2::254]", "http://224.0.0.1", "https://tbench.ai/tasks"} {
		if _, err := b.Read(context.Background(), u); err == nil {
			t.Fatal("SSRF accepted", u)
		}
	}
	reader, _ := localReader(t, func(w http.ResponseWriter, r *http.Request) { t.Error("private DNS reached server") })
	b.reader = reader
	if _, err := b.Read(context.Background(), "http://private.test"); err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatal("DNS loopback", err)
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.0.1", "198.18.0.1", "100.64.0.1", "::", "::1", "fc00::1", "ff02::1", "64:ff9b::7f00:1"} {
		if publicIP(net.ParseIP(ip)) {
			t.Fatal("private address allowed", ip)
		}
	}
	// Mixed public/private answers fail closed, and DNS is resolved at dial time.
	g := addressGuard{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.215.14")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}, dial: func(context.Context, string, string) (net.Conn, error) { t.Fatal("mixed DNS dialed"); return nil, nil }}
	if _, err := g.dialContext(context.Background(), "tcp", "rebind.test:80"); err == nil {
		t.Fatal("mixed DNS accepted")
	}
}

type renderFunc func(context.Context, string) (string, string, error)

func (f renderFunc) Render(ctx context.Context, u string) (string, string, error) { return f(ctx, u) }
func TestRenderer(t *testing.T) {
	b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><div id="root"></div><script>load()</script></body></html>`)
	})
	calls := 0
	b.renderer = renderFunc(func(ctx context.Context, u string) (string, string, error) {
		calls++
		return `<html><head><title>Rendered</title></head><body><article><p>Rendered article content.</p></article></body></html>`, "http://93.184.215.14/rendered", nil
	})
	p, err := b.Read(context.Background(), "http://public.test/js")
	if err != nil || calls != 1 || p.Title != "Rendered" || !strings.Contains(p.Markdown, "Rendered article") || p.FinalURL != "http://93.184.215.14/rendered" {
		t.Fatal(p, err)
	}
	b.renderer = renderFunc(func(context.Context, string) (string, string, error) { return "x", "http://127.0.0.1/secret", nil })
	if _, err = b.Read(context.Background(), "http://public.test/js"); err == nil {
		t.Fatal("private rendered URL accepted")
	}
	b.renderer = renderFunc(func(context.Context, string) (string, string, error) {
		return "", "", errors.New("private renderer detail")
	})
	if _, err = b.Read(context.Background(), "http://public.test/js"); err == nil || strings.Contains(err.Error(), "private renderer detail") {
		t.Fatal(err)
	}
}
func TestReadCancellation(t *testing.T) {
	b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := b.Read(ctx, "http://public.test/slow")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestUnicodeTruncation(t *testing.T) {
	s := truncate(strings.Repeat("界", 100), 50)
	if !utf8.ValidString(s) || len(s) > 50 || !strings.HasSuffix(s, truncationMarker) {
		t.Fatal(s)
	}
}

func TestDNSRebindingRefusedAtDial(t *testing.T) {
	calls := 0
	g := addressGuard{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		ip := "93.184.215.14"
		if calls > 1 {
			ip = "127.0.0.1"
		}
		return []net.IPAddr{{IP: net.ParseIP(ip)}}, nil
	}, dial: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("rebound address dialed")
		return nil, nil
	}}
	if err := g.check(context.Background(), "https://rebind.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.dialContext(context.Background(), "tcp", "rebind.test:443"); err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatal("rebinding allowed", err)
	}
}

func TestRedirectHopLimit(t *testing.T) {
	calls := 0
	b := readerBackend(t, func(w http.ResponseWriter, r *http.Request) { calls++; http.Redirect(w, r, "/next", http.StatusFound) })
	if _, err := b.Read(context.Background(), "http://public.test/start"); err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatal(err)
	}
	if calls != 6 {
		t.Fatalf("wanted initial request plus five redirects, got %d", calls)
	}
}
