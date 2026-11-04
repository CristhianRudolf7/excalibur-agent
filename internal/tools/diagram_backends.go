package tools

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

const diagramOutputLimit = 16 << 20
const diagramSourceLimit = 4 << 20

// SVGDiagramBackend uses installed renderers, or an explicitly configured
// self-hosted Kroki service. Render returns bytes in a string for compatibility
// with DiagramBackend, including for binary formats.
type SVGDiagramBackend struct {
	LookPath       LookPathFunc
	Timeout        time.Duration
	MaxOutputBytes int64
	TempDir        string
}

func (b *SVGDiagramBackend) lookup(name string) (string, error) {
	if b.LookPath != nil {
		return b.LookPath(name)
	}
	return exec.LookPath(name)
}
func (b *SVGDiagramBackend) limit() int64 {
	if b.MaxOutputBytes > 0 && b.MaxOutputBytes < diagramOutputLimit {
		return b.MaxOutputBytes
	}
	return diagramOutputLimit
}
func (b *SVGDiagramBackend) Render(ctx context.Context, source, dialect, format string) (string, error) {
	data, _, err := b.renderWithReport(ctx, source, dialect, format)
	return data, err
}
func canonicalDiagramDialect(dialect string) string {
	switch dialect = strings.ToLower(strings.TrimSpace(dialect)); dialect {
	case "dot":
		return "graphviz"
	case "draw.io":
		return "drawio"
	}
	return dialect
}
func diagramSourceExtension(dialect string) string {
	return map[string]string{"mermaid": "mmd", "d2": "d2", "graphviz": "dot", "plantuml": "puml", "drawio": "drawio", "svg": "svg"}[canonicalDiagramDialect(dialect)]
}
func (b *SVGDiagramBackend) renderWithReport(ctx context.Context, source, dialect, format string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	dialect = canonicalDiagramDialect(dialect)
	if format == "" {
		format = "svg"
	}
	format = strings.ToLower(format)
	if format != "svg" && format != "png" && format != "pdf" {
		return "", "", fmt.Errorf("unsupported diagram output format %q (want svg, png or pdf)", format)
	}
	if len(source) > diagramSourceLimit {
		return "", "", fmt.Errorf("diagram source exceeds %d bytes", diagramSourceLimit)
	}
	if err := checkDiagramSyntax(source, dialect); err != nil {
		return "", "", err
	}
	timeout := b.Timeout
	if timeout <= 0 || timeout > 45*time.Second {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if dialect == "svg" {
		data, err := finishDiagramOutput(source, "svg", b.limit())
		if err != nil {
			return "", "", err
		}
		if format == "svg" {
			return data, "sanitized SVG", nil
		}
		converted, used, err := b.convertSVG(ctx, data, format)
		return converted, "sanitized SVG -> " + used, err
	}

	dir, err := os.MkdirTemp(b.TempDir, "excalibur-diagram-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	input, output := filepath.Join(dir, "input."+diagramSourceExtension(dialect)), filepath.Join(dir, "output."+format)
	if err := os.WriteFile(input, []byte(source), 0o600); err != nil {
		return "", "", err
	}
	bin, args, pipe := b.command(dialect, format, input, output)
	if bin == "" {
		if endpoint := os.Getenv("EXCALIBUR_KROKI_URL"); endpoint != "" {
			data, err := b.kroki(ctx, endpoint, source, dialect, format)
			return data, "configured Kroki", err
		}
		return "", "", b.missingRenderer(dialect)
	}
	data, isolation, err := b.run(ctx, dir, bin, args, source, output, pipe)
	used := filepath.Base(bin) + " (" + isolation + ")"
	if err != nil {
		if format == "svg" || !diagramFormatUnavailable(err) {
			return "", "", fmt.Errorf("%s: %w", dialect, err)
		}
		// Only missing format support is retried; syntax errors remain actionable.
		svgOutput := filepath.Join(dir, "intermediate.svg")
		svgBin, svgArgs, svgPipe := b.command(dialect, "svg", input, svgOutput)
		data, _, err = b.run(ctx, dir, svgBin, svgArgs, source, svgOutput, svgPipe)
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", dialect, err)
		}
		data, err = finishDiagramOutput(data, "svg", b.limit())
		if err != nil {
			return "", "", err
		}
		converted, converter, err := b.convertSVG(ctx, data, format)
		return converted, used + " SVG -> " + converter, err
	}
	data, err = finishDiagramOutput(data, format, b.limit())
	return data, used, err
}

func (b *SVGDiagramBackend) command(dialect, format, input, output string) (string, []string, bool) {
	name := map[string]string{"mermaid": "mmdc", "d2": "d2", "graphviz": "dot", "plantuml": "plantuml", "drawio": "drawio"}[dialect]
	bin, err := b.lookup(name)
	var prefix []string
	if err != nil && dialect == "plantuml" {
		jar := os.Getenv("EXCALIBUR_PLANTUML_JAR")
		if jar == "" {
			for _, candidate := range plantUMLJars() {
				if st, e := os.Stat(candidate); e == nil && st.Mode().IsRegular() {
					jar, _ = filepath.Abs(candidate)
					break
				}
			}
		}
		if jar != "" {
			if st, e := os.Stat(jar); e == nil && st.Mode().IsRegular() {
				jar, _ = filepath.Abs(jar)
				bin, err = b.lookup(diagramJava())
				prefix = []string{"-Djava.awt.headless=true", "-DPLANTUML_SECURITY_PROFILE=SANDBOX", "-jar", jar}
			}
		}
	}
	if err != nil {
		return "", nil, false
	}
	switch dialect {
	case "mermaid":
		return bin, []string{"-i", input, "-o", output}, false
	case "d2":
		return bin, []string{input, output}, false
	case "graphviz":
		return bin, []string{"-T" + format, input, "-o", output}, false
	case "plantuml":
		return bin, append(prefix, "-pipe", "-t"+format, "-failfast2", "-charset", "UTF-8"), true
	case "drawio":
		return bin, []string{"--export", "--format", format, "--output", output, input}, false
	}
	return "", nil, false
}

// cappedDiagramWriter cancels a producer as soon as its output exceeds its
// budget. It retains only bounded bytes, including when a renderer is noisy.
type cappedDiagramWriter struct {
	buffer   bytes.Buffer
	max      int64
	cancel   context.CancelFunc
	exceeded bool
}

func (w *cappedDiagramWriter) String() string { return w.buffer.String() }
func (w *cappedDiagramWriter) Write(p []byte) (int, error) {
	if int64(w.buffer.Len()+len(p)) > w.max {
		w.exceeded = true
		w.cancel()
		return 0, fmt.Errorf("renderer output exceeds %d bytes", w.max)
	}
	return w.buffer.Write(p)
}
func (b *SVGDiagramBackend) run(ctx context.Context, dir, bin string, args []string, source, output string, pipe bool) (string, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	isolation := "network isolation unavailable"
	// Namespace support depends on host policy, not just the presence of unshare.
	// Probe before rendering so a syntax failure never causes an unisolated retry.
	if namespaceIsolationSupported() {
		if unshare, err := b.lookup("unshare"); err == nil {
			probeCtx, stop := context.WithTimeout(ctx, time.Second)
			probe := exec.CommandContext(probeCtx, unshare, "--user", "--map-current-user", "--net", "--", "true")
			probe.SysProcAttr = sysProcAttr()
			probe.Cancel = func() error { return killDiagramProcess(probe) }
			probe.WaitDelay = 200 * time.Millisecond
			err = probe.Run()
			stop()
			if err == nil {
				args = append([]string{"--user", "--map-current-user", "--net", "--", bin}, args...)
				bin, isolation = unshare, "network namespace"
			}
		}
		if prlimit, err := b.lookup("prlimit"); err == nil {
			args = append([]string{fmt.Sprintf("--fsize=%d", b.limit()), "--", bin}, args...)
			bin = prlimit
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLANTUML_SECURITY_PROFILE=SANDBOX", "ELECTRON_DISABLE_SECURITY_WARNINGS=1")
	cmd.SysProcAttr = sysProcAttr()
	cmd.Cancel = func() error { return killDiagramProcess(cmd) }
	cmd.WaitDelay = time.Second
	stdout := &cappedDiagramWriter{max: b.limit(), cancel: cancel}
	stderr := &cappedDiagramWriter{max: 32 << 10, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if pipe {
		cmd.Stdin = strings.NewReader(source)
	}
	// File-producing renderers are bounded even on hosts without prlimit.
	done, stopped := make(chan struct{}), make(chan struct{})
	safego.Go("tools/diagram-output-limit", nil, func() {
		defer close(stopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if st, err := os.Stat(output); err == nil && st.Size() > b.limit() {
					cancel()
					return
				}
			}
		}
	})
	err := runDiagramCommand(cmd)
	// A renderer can leave children holding output pipes after its leader exits.
	// Stop the remaining process group before cleaning its temporary directory.
	if cmd.Process != nil {
		_ = killDiagramProcess(cmd)
	}
	close(done)
	<-stopped
	if stdout.exceeded || stderr.exceeded {
		return "", isolation, fmt.Errorf("renderer output exceeds size limit")
	}
	if st, e := os.Stat(output); e == nil && st.Size() > b.limit() {
		return "", isolation, fmt.Errorf("diagram output exceeds %d bytes", b.limit())
	}
	if ctx.Err() != nil {
		return "", isolation, fmt.Errorf("renderer interrupted: %w", ctx.Err())
	}
	if err != nil {
		return "", isolation, fmt.Errorf("renderer failed: %w: %s", err, diagramDiagnostic(stderr.String()))
	}
	if pipe {
		return stdout.String(), isolation, nil
	}
	data, err := readDiagramOutput(output, b.limit())
	return data, isolation, err
}
func readDiagramOutput(path string, max int64) (string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("renderer output is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err = f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("renderer output is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > max {
		return "", fmt.Errorf("diagram output exceeds %d bytes", max)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("renderer produced empty output")
	}
	return string(data), nil
}
func (b *SVGDiagramBackend) kroki(ctx context.Context, endpoint, source, dialect, format string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("EXCALIBUR_KROKI_URL must be an HTTP(S) base URL without credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + dialect + "/" + format
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(source))
	if err != nil {
		return "", fmt.Errorf("create Kroki request")
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("configured Kroki request failed")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, b.limit()+1))
	if err != nil {
		return "", fmt.Errorf("read Kroki response")
	}
	if int64(len(data)) > b.limit() {
		return "", fmt.Errorf("kroki output exceeds %d bytes", b.limit())
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kroki returned HTTP %d: %s", res.StatusCode, diagramDiagnostic(string(data)))
	}
	return finishDiagramOutput(string(data), format, b.limit())
}

var diagramSVGElements = strings.Fields("svg g defs symbol use path rect circle ellipse line polyline polygon text tspan textPath title desc marker clipPath mask pattern linearGradient radialGradient stop filter feGaussianBlur feOffset feBlend feColorMatrix feFlood feComposite feMerge feMergeNode image style foreignObject div span p br b i strong em table tbody tr td switch a")

func sanitizeDiagramSVG(source string) (string, error) {
	allowed := make(map[string]bool, len(diagramSVGElements))
	for _, name := range diagramSVGElements {
		allowed[name] = true
	}
	d := xml.NewDecoder(strings.NewReader(source))
	var out bytes.Buffer
	e := xml.NewEncoder(&out)
	depth, skip, roots, styleDepth := 0, 0, 0, 0
	var styleText strings.Builder
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("svg: %s", diagramDiagnostic(err.Error()))
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if skip > 0 {
				skip++
				continue
			}
			if depth == 1 {
				roots++
				if t.Name.Local != "svg" || roots > 1 || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
					return "", fmt.Errorf("svg: want one svg root")
				}
			}
			if !allowed[t.Name.Local] || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg" && t.Name.Space != "http://www.w3.org/1999/xhtml") {
				skip = 1
				continue
			}
			if t.Name.Local == "style" {
				styleDepth = depth
				styleText.Reset()
			}
			ns := t.Name.Space
			t.Name.Space = ""
			attrs := make([]xml.Attr, 0, len(t.Attr))
			if depth == 1 {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: "http://www.w3.org/2000/svg"})
			} else if ns == "http://www.w3.org/1999/xhtml" {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: ns})
			}
			for _, a := range t.Attr {
				name := strings.ToLower(a.Name.Local)
				if name == "xmlns" || a.Name.Space == "xmlns" || strings.HasPrefix(name, "on") || name == "src" || name == "base" || name == "ping" {
					continue
				}
				if name == "href" && !safeDiagramReference(a.Value) {
					continue
				}
				a.Value = sanitizeDiagramCSS(a.Value)
				if a.Value == "" {
					continue
				}
				a.Name.Space = ""
				attrs = append(attrs, a)
			}
			t.Attr = attrs
			if err := e.EncodeToken(t); err != nil {
				return "", err
			}
		case xml.EndElement:
			if depth == styleDepth {
				if css := sanitizeDiagramCSS(styleText.String()); css != "" {
					if err := e.EncodeToken(xml.CharData(css)); err != nil {
						return "", err
					}
				}
				styleDepth = 0
			}
			depth--
			if skip > 0 {
				skip--
				continue
			}
			t.Name.Space = ""
			if err := e.EncodeToken(t); err != nil {
				return "", err
			}
		case xml.CharData:
			if skip == 0 && depth > 0 {
				// CSS is kept only when it cannot import or reference external resources.
				if styleDepth > 0 {
					styleText.Write(t)
					continue
				}
				if err := e.EncodeToken(t); err != nil {
					return "", err
				}
			}
		}
	}
	if roots != 1 {
		return "", fmt.Errorf("svg: missing svg root")
	}
	if err := e.Flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}
