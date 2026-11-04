package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testDiagramSVG = `<svg xmlns="http://www.w3.org/2000/svg"><path d="M0 0L10 10"/><text>rendered</text></svg>`

func diagramFake(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
func diagramLookup(binaries map[string]string) LookPathFunc {
	return func(name string) (string, error) {
		if path, ok := binaries[name]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}
func TestDiagramLocalBackends(t *testing.T) {
	for _, tc := range []struct{ dialect, source, bin, body string }{
		{"mermaid", "flowchart TD\nA-->B", "mmdc", `[ "$1" = '-i' ]; [ "$3" = '-o' ]; printf '%s' '` + testDiagramSVG + `' > "$4"`},
		{"d2", "a -> b", "d2", `[ -f "$1" ]; printf '%s' '` + testDiagramSVG + `' > "$2"`},
		{"graphviz", "digraph {a->b}", "dot", `[ "$1" = '-Tsvg' ]; [ "$3" = '-o' ]; printf '%s' '` + testDiagramSVG + `' > "$4"`},
		{"plantuml", "@startuml\nA -> B\n@enduml", "plantuml", `[ "$1" = '-pipe' ]; [ "$2" = '-tsvg' ]; [ "$PLANTUML_SECURITY_PROFILE" = 'SANDBOX' ]; cat >/dev/null; printf '%s' '` + testDiagramSVG + `'`},
		{"drawio", `<mxfile><diagram/></mxfile>`, "drawio", `[ "$1" = '--export' ]; [ "$2" = '--format' ]; [ "$3" = 'svg' ]; printf '%s' '` + testDiagramSVG + `' > "$5"`},
	} {
		t.Run(tc.dialect, func(t *testing.T) {
			dir := t.TempDir()
			b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{tc.bin: diagramFake(t, tc.bin, tc.body)}), TempDir: dir}
			got, used, err := b.renderWithReport(context.Background(), tc.source, tc.dialect, "svg")
			if err != nil || !strings.Contains(got, `<path d="M0 0L10 10"`) || !strings.Contains(used, tc.bin) {
				t.Fatalf("got %q, %q, %v", got, used, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary directories remain: %v, %v", entries, err)
			}
		})
	}
}
func TestDiagramPlantUMLJar(t *testing.T) {
	jar := writeTemp(t, "plantuml.jar", "fake jar")
	t.Setenv("EXCALIBUR_PLANTUML_JAR", jar)
	java := diagramFake(t, "java", `[ "$3" = '-jar' ]; [ "$4" = '`+jar+`' ]; [ "$5" = '-pipe' ]; printf '%s' '`+testDiagramSVG+`'`)
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"java": java})}
	got, err := b.Render(context.Background(), "@startuml\nA->B\n@enduml", "plantuml", "svg")
	if err != nil || !strings.Contains(got, "rendered") {
		t.Fatalf("%q, %v", got, err)
	}
}
func TestDiagramLimitsAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		timeout          time.Duration
		limit            int64
	}{
		{"timeout", `sleep 5`, "deadline exceeded", 60 * time.Millisecond, 1024},
		{"file limit", `printf '%02000d' 0 > "$4"`, "exceeds", time.Second, 100},
		{"stdout limit", `printf '%02000d' 0`, "exceeds", time.Second, 100},
		{"stderr limit", `i=0; while [ "$i" -lt 1000 ]; do printf '%0100d' 0 >&2; i=$((i+1)); done`, "exceeds", time.Second, 100},
		{"missing output", `exit 0`, "no such file", time.Second, 100},
		{"empty output", `: > "$4"`, "empty output", time.Second, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": diagramFake(t, "dot", tc.body)}), Timeout: tc.timeout, MaxOutputBytes: tc.limit, TempDir: dir}
			start := time.Now()
			_, err := b.Render(context.Background(), "digraph {a->b}", "graphviz", "svg")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("renderer did not stop promptly")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("temporary directories remain")
			}
		})
	}
	b := &SVGDiagramBackend{LookPath: diagramLookup(nil)}
	if _, err := b.Render(context.Background(), strings.Repeat("x", diagramSourceLimit+1), "d2", "svg"); err == nil {
		t.Fatal("oversized source accepted")
	}
}
func TestDiagramSVGSanitizing(t *testing.T) {
	source := `<?xml version="1.0"?><!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" onload="bad()"><script>evil()</script><g onclick="evil()"><use xlink:href="#local"/><image href="https://example.invalid/x"/><use href="file:///etc/passwd"/><a href="javascript:bad()"><text>label</text></a><path style="fill:url(https://example.invalid/p)"/><style>@import 'https://example.invalid/x';</style><style>.safe{fill:red}.local{fill:url(#local)}</style><foreignObject><div xmlns="http://www.w3.org/1999/xhtml" onmouseover="bad()"><span>safe</span><iframe src="https://example.invalid"/></div></foreignObject><animate attributeName="href" values="javascript:bad()"/></g></svg>`
	b := &SVGDiagramBackend{}
	got, err := b.Render(context.Background(), source, "svg", "svg")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"evil", "bad()", "https://", "file:", "@import", "onload", "onclick", "onmouseover", "iframe", "animate", "DOCTYPE"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe %q remains in %s", bad, got)
		}
	}
	for _, want := range []string{`href="#local"`, `.safe{fill:red}`, "label", "safe"} {
		if !strings.Contains(html.UnescapeString(got), want) {
			t.Errorf("lost %q: %s", want, got)
		}
	}
	for _, bad := range []string{`<svg><g></svg>`, `<svg/><svg/>`, `<svg>&unknown;</svg>`, `<div/>`} {
		if _, err := b.Render(context.Background(), bad, "svg", "svg"); err == nil {
			t.Errorf("invalid svg accepted: %s", bad)
		}
	}
}
func TestDiagramKrokiOptIn(t *testing.T) {
	t.Setenv("EXCALIBUR_KROKI_URL", "")
	b := &SVGDiagramBackend{LookPath: diagramLookup(nil)}
	if _, err := b.Render(context.Background(), "a -> b", "d2", "svg"); err == nil {
		t.Fatal("missing renderer unexpectedly succeeded")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/diagrams/d2/svg" || r.Method != "POST" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, testDiagramSVG)
	}))
	defer server.Close()
	t.Setenv("EXCALIBUR_KROKI_URL", server.URL+"/diagrams")
	got, err := b.Render(context.Background(), "a -> b", "d2", "svg")
	if err != nil || !strings.Contains(got, "rendered") {
		t.Fatalf("%q, %v", got, err)
	}
	b.MaxOutputBytes = 10
	if _, err := b.Render(context.Background(), "a -> b", "d2", "svg"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("limit: %v", err)
	}
	t.Setenv("EXCALIBUR_KROKI_URL", "https://user:password@example.invalid")
	if _, err := b.Render(context.Background(), "a -> b", "d2", "svg"); err == nil || strings.Contains(err.Error(), "password") {
		t.Fatalf("invalid URL: %v", err)
	}
}
func TestDiagramCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": diagramFake(t, "dot", `sleep 5`)})}
	_, err := b.Render(ctx, "digraph {a->b}", "graphviz", "svg")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestDiagramMissingRendererHints(t *testing.T) {
	t.Setenv("EXCALIBUR_KROKI_URL", "")
	for _, tc := range []struct{ dialect, source, binary, install string }{
		{"mermaid", "graph TD; A-->B", "mmdc", "npm install -g @mermaid-js/mermaid-cli"},
		{"d2", "a -> b", "d2", "go install oss.terrastruct.com/d2@latest"},
		{"graphviz", "digraph {a->b}", "dot", "graphviz"},
		{"plantuml", "@startuml\nA->B\n@enduml", "plantuml", "plantuml"},
		{"drawio", "<mxfile/>", "drawio", "drawio-desktop/releases"},
	} {
		t.Run(tc.dialect, func(t *testing.T) {
			b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"apt-get": "/usr/bin/apt-get"})}
			_, err := b.Render(context.Background(), tc.source, tc.dialect, "svg")
			if err == nil {
				t.Fatal("missing renderer succeeded")
			}
			wants := []string{tc.binary, b.installHint(tc.dialect, runtime.GOOS), runtime.GOOS, "Another dialect such as graphviz"}
			if runtime.GOOS != "darwin" {
				// Retain the existing GNU install-hint assertions as well.
				wants = append(wants, tc.install)
			}
			for _, want := range wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err, want)
				}
			}
		})
	}
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": "/fake/dot"})}
	if err := b.missingRenderer("d2"); !strings.Contains(err.Error(), "Available alternative dialects: graphviz") {
		t.Fatal(err)
	}
	for _, tc := range []struct{ dialect, system, want string }{
		{"d2", "darwin", "brew install d2"}, {"drawio", "darwin", "brew install --cask drawio"},
		{"graphviz", "windows", "graphviz.org/download"}, {"plantuml", "windows", "EXCALIBUR_PLANTUML_JAR"},
	} {
		if got := b.installHint(tc.dialect, tc.system); !strings.Contains(got, tc.want) {
			t.Errorf("%s/%s: %s", tc.dialect, tc.system, got)
		}
	}
}

func TestDiagramNativeFormats(t *testing.T) {
	for _, dialect := range []string{"mermaid", "d2", "graphviz", "plantuml", "drawio"} {
		for _, format := range []string{"png", "pdf"} {
			t.Run(dialect+"/"+format, func(t *testing.T) {
				payload := `\211PNG\r\n\032\nrendered`
				if format == "pdf" {
					payload = `%%PDF-1.7\nrendered`
				}
				body := map[string]string{
					"mermaid":  `case "$4" in *.` + format + `) ;; *) exit 2;; esac; printf '` + payload + `' > "$4"`,
					"d2":       `case "$2" in *.` + format + `) ;; *) exit 2;; esac; printf '` + payload + `' > "$2"`,
					"graphviz": `[ "$1" = '-T` + format + `' ]; printf '` + payload + `' > "$4"`,
					"plantuml": `[ "$2" = '-t` + format + `' ]; printf '` + payload + `'`,
					"drawio":   `[ "$3" = '` + format + `' ]; printf '` + payload + `' > "$5"`,
				}[dialect]
				bin := map[string]string{"mermaid": "mmdc", "d2": "d2", "graphviz": "dot", "plantuml": "plantuml", "drawio": "drawio"}[dialect]
				source := map[string]string{"mermaid": "graph TD; A-->B", "d2": "a -> b", "graphviz": "digraph {a->b}", "plantuml": "@startuml\nA->B\n@enduml", "drawio": "<mxfile/>"}[dialect]
				b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{bin: diagramFake(t, bin, body)})}
				got, used, err := b.renderWithReport(context.Background(), source, dialect, format)
				if err != nil || !strings.Contains(got, "rendered") || !strings.Contains(used, bin) {
					t.Fatalf("%q %q %v", got, used, err)
				}
			})
		}
	}
}
func TestDiagramSVGConversion(t *testing.T) {
	for _, converter := range []string{"rsvg-convert", "chromium"} {
		for _, format := range []string{"png", "pdf"} {
			t.Run(converter+"/"+format, func(t *testing.T) {
				payload := `\211PNG\r\n\032\nconverted`
				if format == "pdf" {
					payload = `%%PDF-1.7\nconverted`
				}
				body := `out=''; for arg in "$@"; do case "$arg" in --output=*|--screenshot=*|--print-to-pdf=*) out="${arg#*=}";; esac; done; [ -n "$out" ]; printf '` + payload + `' > "$out"`
				b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{converter: diagramFake(t, converter, body)})}
				got, used, err := b.renderWithReport(context.Background(), testDiagramSVG, "svg", format)
				if err != nil || !strings.Contains(got, "converted") || !strings.Contains(used, converter) {
					t.Fatalf("%q %q %v", got, used, err)
				}
			})
		}
	}
}
func TestDiagramUnsupportedNativeFormatConversion(t *testing.T) {
	dot := diagramFake(t, "dot", `if [ "$1" != '-Tsvg' ]; then echo 'Format pdf not supported' >&2; exit 1; fi; printf '%s' '`+testDiagramSVG+`' > "$4"`)
	rsvg := diagramFake(t, "rsvg-convert", `printf '%%PDF-1.7\nconverted' > "${2#*=}"`)
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": dot, "rsvg-convert": rsvg})}
	got, used, err := b.renderWithReport(context.Background(), "digraph {a->b}", "graphviz", "pdf")
	if err != nil || !strings.Contains(got, "converted") || !strings.Contains(used, "dot") || !strings.Contains(used, "rsvg-convert") {
		t.Fatalf("%q %q %v", got, used, err)
	}
	b.LookPath = diagramLookup(nil)
	if _, err := b.Render(context.Background(), testDiagramSVG, "svg", "png"); err == nil || !strings.Contains(err.Error(), "rsvg-convert") {
		t.Fatalf("missing converter: %v", err)
	}
	for _, tc := range []struct{ data, format string }{{testDiagramSVG, "png"}, {testDiagramSVG, "pdf"}} {
		if _, err := finishDiagramOutput(tc.data, tc.format, 1024); err == nil {
			t.Fatal("SVG mislabeled as binary")
		}
	}
}

func TestDiagramErrorLineParsing(t *testing.T) {
	for _, tc := range []struct{ diagnostic, want string }{
		{"Error: Parse error on line 3:\nA -> B\n----^\nExpecting 'LINK', got 'MINUS'\n at parser.js:20:30", "line 3: Expecting 'LINK', got 'MINUS'"},
		{"err: input.d2:4:12: unexpected token: }", "line 4: unexpected token: }"},
		{"Error: input.dot: syntax error in line 7 near '}'", "line 7: syntax error near '}'"},
		{"ERROR\n2\nSyntax Error?", "line 2: Syntax Error?"},
		{"Error on line 8: unknown participant", "line 8: unknown participant"},
		{"XML syntax error on line 2: element <g> closed by </svg>", "line 2: syntax error element <g> closed by </svg>"},
		{"renderer could not start", "renderer could not start"},
		{"Could not find Chrome\n    at run (file.js:9:27)", "Could not find Chrome\n    at run (file.js:9:27)"},
	} {
		if got := diagramDiagnostic(tc.diagnostic); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.diagnostic, got, tc.want)
		}
	}
	dot := diagramFake(t, "dot", `echo 'Error: input.dot: syntax error in line 7 near EOF' >&2; exit 1`)
	b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": dot})}
	if _, err := b.Render(context.Background(), "digraph {a->}", "graphviz", "svg"); err == nil || !strings.Contains(err.Error(), "line 7: syntax error near EOF") {
		t.Fatalf("error %v", err)
	}
	// Syntax errors must not be retried through SVG or a converter.
	counter := filepath.Join(t.TempDir(), "attempts")
	dot = diagramFake(t, "dot", `printf x >> '`+counter+`'; echo 'input.dot:3: syntax error' >&2; exit 1`)
	b.LookPath = diagramLookup(map[string]string{"dot": dot})
	if _, err := b.Render(context.Background(), "digraph {a->}", "graphviz", "pdf"); err == nil || !strings.Contains(err.Error(), "line 3: syntax error") {
		t.Fatalf("error %v", err)
	}
	attempts, _ := os.ReadFile(counter)
	if string(attempts) != "x" {
		t.Fatalf("syntax error retried: %q", attempts)
	}
}

func TestDiagramSVGCSSPreservesStyles(t *testing.T) {
	source := `<svg xmlns="http://www.w3.org/2000/svg"><style>@font-face{font-family:d2;src:url(data:font/woff2;base64,AAAA)}@import "https://example.invalid/import"; .node{fill:red;stroke:black}.icon{fill:url(https://example.invalid/pattern)}.local{fill:url('#local')}</style><style>.split{fill:url<![CDATA[(https://example.invalid/split)]]>}</style><text>Email @example and a backslash \</text><rect class="node" width="10" height="10"/></svg>`
	got, err := sanitizeDiagramSVG(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"https://", "@import"} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe %s remains: %s", bad, got)
		}
	}
	for _, want := range []string{"@font-face", "data:font/woff2;base64,AAAA", ".node{fill:red;stroke:black}", "url('#local')", "Email @example and a backslash \\"} {
		if !strings.Contains(html.UnescapeString(got), want) {
			t.Errorf("lost %q: %s", want, got)
		}
	}
	if _, err := sanitizeDiagramSVG(`<svg xmlns="https://example.invalid/foreign"/>`); err == nil {
		t.Fatal("foreign SVG root accepted")
	}
}

func TestDiagramNetworkAndFileLimitWrappers(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux wrappers")
	}
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			probeBody := `[ "$1" = '--user' ]; [ "$2" = '--map-current-user' ]; [ "$3" = '--net' ]; [ "$4" = '--' ]; shift 4; exec "$@"`
			if !available {
				probeBody = `exit 1`
			}
			unshare := diagramFake(t, "unshare", probeBody)
			prlimit := diagramFake(t, "prlimit", `[ "$1" = '--fsize=16777216' ]; [ "$2" = '--' ]; shift 2; exec "$@"`)
			dot := diagramFake(t, "dot", `printf '%s' '`+testDiagramSVG+`' > "$4"`)
			b := &SVGDiagramBackend{LookPath: diagramLookup(map[string]string{"dot": dot, "unshare": unshare, "prlimit": prlimit})}
			_, used, err := b.renderWithReport(context.Background(), "digraph {a->b}", "graphviz", "svg")
			if err != nil {
				t.Fatal(err)
			}
			if available && !strings.Contains(used, "network namespace") {
				t.Fatal(used)
			}
			if !available && !strings.Contains(used, "isolation unavailable") {
				t.Fatal(used)
			}
		})
	}
}

func TestDiagramKrokiFailuresAndLocalPreference(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/timeout/d2/svg":
			select {
			case <-r.Context().Done():
			case <-time.After(250 * time.Millisecond):
			}
		case "/redirect/d2/svg":
			http.Redirect(w, r, "/public-service", http.StatusFound)
		case "/syntax/d2/svg":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "input.d2:9:3: invalid connection")
		case "/d2/png":
			fmt.Fprint(w, "\x89PNG\r\n\x1a\nremote")
		default:
			fmt.Fprint(w, testDiagramSVG)
		}
	}))
	defer server.Close()
	b := &SVGDiagramBackend{LookPath: diagramLookup(nil), Timeout: 50 * time.Millisecond}
	t.Setenv("EXCALIBUR_KROKI_URL", server.URL+"/timeout")
	if _, err := b.Render(context.Background(), "a->b", "d2", "svg"); err == nil {
		t.Fatal("remote timeout ignored")
	}
	b.Timeout = 5 * time.Second
	t.Setenv("EXCALIBUR_KROKI_URL", server.URL+"/redirect")
	if _, err := b.Render(context.Background(), "a->b", "d2", "svg"); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("redirect: %v", err)
	}
	t.Setenv("EXCALIBUR_KROKI_URL", server.URL+"/syntax")
	if _, err := b.Render(context.Background(), "a->b", "d2", "svg"); err == nil || !strings.Contains(err.Error(), "line 9: invalid connection") {
		t.Fatalf("syntax: %v", err)
	}
	t.Setenv("EXCALIBUR_KROKI_URL", server.URL)
	if got, err := b.Render(context.Background(), "a->b", "d2", "png"); err != nil || !strings.Contains(got, "remote") {
		t.Fatalf("remote PNG %q: %v", got, err)
	}
	before := requests.Load()
	d2 := diagramFake(t, "d2", `printf '%s' '`+testDiagramSVG+`' > "$2"`)
	b.LookPath = diagramLookup(map[string]string{"d2": d2})
	if _, err := b.Render(context.Background(), "a->b", "d2", "svg"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before {
		t.Fatal("configured service contacted when local renderer exists")
	}
}

func TestDiagramInstalledRenderers(t *testing.T) {
	t.Setenv("EXCALIBUR_KROKI_URL", "")
	for _, tc := range []struct{ binary, dialect, source string }{
		{"dot", "graphviz", "digraph {A->B}"},
		{"d2", "d2", "A -> B"},
		{"mmdc", "mermaid", "flowchart TD\nA-->B"},
	} {
		t.Run(tc.binary, func(t *testing.T) {
			if _, err := exec.LookPath(tc.binary); err != nil {
				t.Skip(tc.binary + " is not installed")
			}
			got, err := (&SVGDiagramBackend{}).Render(context.Background(), tc.source, tc.dialect, "svg")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(got, "<svg") || !strings.Contains(got, "<path") || !strings.Contains(got, "A") || !strings.Contains(got, "B") {
				t.Fatalf("expected rendered nodes and edge: %s", firstLine(got))
			}
		})
	}
}

func TestDiagramSVGEmbeddedImages(t *testing.T) {
	source := `<svg><image href="data:image/png;base64,AAAA"/><image href="data:image/svg+xml;base64,PHN2Zz4="/><image href="https://example.invalid/image.png"/><a href="#local" ping="https://example.invalid/track"><text>local</text></a></svg>`
	got, err := sanitizeDiagramSVG(source)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "data:image/png;base64,AAAA") {
		t.Fatalf("embedded image removed: %s", got)
	}
	for _, bad := range []string{"data:image/svg+xml", "https://", "ping="} {
		if strings.Contains(got, bad) {
			t.Errorf("unsafe reference remains: %s", got)
		}
	}
}

func TestDiagramChromiumViewport(t *testing.T) {
	for _, tc := range []struct {
		svg  string
		w, h int
	}{
		{`<svg width="320px" height="180"/>`, 320, 180},
		{`<svg viewBox="0 0 640 480"/>`, 640, 480},
		{`<svg width="NaN" height="100000000"/>`, 1280, 720},
	} {
		w, h := diagramSVGSize(tc.svg)
		if w != tc.w || h != tc.h {
			t.Errorf("%s: got %d x %d", tc.svg, w, h)
		}
	}
}
