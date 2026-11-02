package websearch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/markusmobius/go-trafilatura"
	"golang.org/x/net/html"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type addressGuard struct {
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
}

var deniedNetworks = []netip.Prefix{
	netip.MustParsePrefix("168.63.129.16/32"), netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"), netip.MustParsePrefix("fec0::/10"),
}

func publicIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range deniedNetworks {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
func (g addressGuard) addresses(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := g.lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("websearch: DNS resolution failed")
	}
	for _, ip := range ips {
		if ip.Zone != "" || !publicIP(ip.IP) {
			return nil, errors.New("websearch: SSRF refused non-public address")
		}
	}
	return ips, nil
}

type benchmarkReadError struct{ url string }

func (e *benchmarkReadError) Error() string {
	return fmt.Sprintf("blocked: benchmark source %q is not readable (reward hacking, Section 23.2)", e.url)
}

func validateURL(raw string) (*url.URL, error) {
	if capabilities.BlockedBenchmarkSource(raw) {
		return nil, &benchmarkReadError{url: raw}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("websearch: only public http(s) URLs without credentials are readable")
	}
	return u, nil
}
func (g addressGuard) check(ctx context.Context, raw string) error {
	u, err := validateURL(raw)
	if err != nil {
		return err
	}
	_, err = g.addresses(ctx, u.Hostname())
	return err
}
func (g addressGuard) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("websearch: invalid address")
	}
	ips, err := g.addresses(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		conn, e := g.dial(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("websearch: connection failed")
}
func newReaderClient(timeout time.Duration) *http.Client {
	g := addressGuard{lookup: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: timeout}).DialContext}
	return guardedClient(g, timeout)
}
func guardedClient(g addressGuard, timeout time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = g.dialContext
	t.DialTLSContext = nil
	t.ResponseHeaderTimeout = timeout
	return &http.Client{Transport: t, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("websearch: too many redirects")
		}
		return g.check(req.Context(), req.URL.String())
	}}
}

const truncationMarker = "\n\n[truncated]"

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	keep := n - len(truncationMarker)
	if keep < 0 {
		keep = 0
	}
	for keep > 0 && !utf8.RuneStart(s[keep]) {
		keep--
	}
	return s[:keep] + truncationMarker
}
func htmlMarkdown(raw, base string) (string, string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return "", "", errors.New("websearch: invalid HTML")
	}
	title := strings.TrimSpace(doc.Find("title").First().Text())
	doc.Find("script,style,noscript,template").Remove()
	clean, err := doc.Html()
	if err != nil {
		return "", "", errors.New("websearch: HTML conversion failed")
	}
	extracted, err := trafilatura.Extract(strings.NewReader(clean), trafilatura.Options{ExcludeComments: true, IncludeLinks: true, MaxTreeSize: 100000})
	if err == nil && extracted != nil && len(strings.TrimSpace(extracted.ContentText)) >= 80 && extracted.ContentNode != nil {
		var buf bytes.Buffer
		if html.Render(&buf, extracted.ContentNode) == nil {
			clean = buf.String()
		}
		if title == "" {
			title = extracted.Metadata.Title
		}
	}
	body, err := md.NewConverter(base, true, nil).ConvertString(clean)
	if err != nil {
		return "", "", errors.New("websearch: Markdown conversion failed")
	}
	return title, strings.TrimSpace(body), nil
}

// Read fetches a public HTTP(S) page and returns bounded Markdown and provenance.
func (b *Backend) Read(ctx context.Context, rawURL string) (tools.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, b.limits.Timeout)
	defer cancel()
	if _, err := validateURL(rawURL); err != nil {
		return tools.Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return tools.Page{}, errors.New("websearch: invalid URL")
	}
	req.Header.Set("User-Agent", b.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,text/markdown,application/json;q=0.9")
	resp, err := b.reader.Do(req)
	if err != nil {
		var blocked *benchmarkReadError
		if errors.As(err, &blocked) {
			return tools.Page{}, blocked
		}
		return tools.Page{}, fmt.Errorf("websearch: page fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Page{}, fmt.Errorf("websearch: page HTTP status %d", resp.StatusCode)
	}
	raw, err := readBounded(resp.Body, b.limits.PageBytes)
	if err != nil {
		return tools.Page{}, err
	}
	finalURL := resp.Request.URL.String()
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind == "" {
		kind, _, _ = mime.ParseMediaType(http.DetectContentType(raw))
	}
	title, body := "", ""
	switch {
	case kind == "text/html" || kind == "application/xhtml+xml":
		title, body, err = htmlMarkdown(string(raw), finalURL)
		if err == nil && len(body) < 80 && b.renderer != nil {
			// Renderer is a trusted hook: its implementation must guard all browser requests.
			rendered, renderURL, e := b.renderer.Render(ctx, finalURL)
			if e != nil {
				return tools.Page{}, errors.New("websearch: rendering failed")
			}
			if int64(len(rendered)) > b.limits.PageBytes {
				return tools.Page{}, errors.New("websearch: rendered response too large")
			}
			if renderURL == "" {
				renderURL = finalURL
			}
			if e = validateRenderedURL(ctx, renderURL); e != nil {
				return tools.Page{}, e
			}
			finalURL = renderURL
			title, body, err = htmlMarkdown(rendered, finalURL)
		}
	case kind == "text/plain" || kind == "text/markdown" || kind == "text/x-markdown":
		body = string(raw)
	case kind == "application/json" || strings.HasSuffix(kind, "+json"):
		var pretty bytes.Buffer
		if json.Indent(&pretty, raw, "", "  ") != nil {
			return tools.Page{}, errors.New("websearch: invalid JSON document")
		}
		body = pretty.String()
	default:
		return tools.Page{}, fmt.Errorf("websearch: unsupported content type %q", kind)
	}
	if err != nil {
		return tools.Page{}, err
	}
	if err = ctx.Err(); err != nil {
		return tools.Page{}, err
	}
	body = truncate(body, b.limits.MarkdownBytes)
	sum := sha256.Sum256([]byte(body))
	return tools.Page{URL: rawURL, FinalURL: finalURL, Title: title, Markdown: body, ReadAt: time.Now().UTC().Format(time.RFC3339), ContentHash: "sha256:" + hex.EncodeToString(sum[:])}, nil
}
func validateRenderedURL(ctx context.Context, raw string) error {
	return (addressGuard{lookup: net.DefaultResolver.LookupIPAddr}).check(ctx, raw)
}
