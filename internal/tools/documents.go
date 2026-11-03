package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

// Document and media backends: the harness side of the documents and
// media capabilities (Section 14.7). PDF structure uses a pure-Go backend;
// optional reader and renderer collaborators supply document fidelity.

// DocumentBackend extracts text and structure with page, section and
// paragraph anchors (document_read).
type DocumentBackend interface {
	Read(ctx context.Context, path string) (DocumentText, error)
}

// DocumentText is the extracted structure.
type DocumentText struct {
	Path     string         `json:"path"`
	Format   string         `json:"format"`
	Pages    []DocumentPage `json:"pages"`
	Sections []string       `json:"sections,omitempty"`
}

// DocumentPage is one paginated chunk with anchors.
type DocumentPage struct {
	Page       int      `json:"page"`
	Paragraphs []string `json:"paragraphs"`
	Text       string   `json:"text"`
}

// VisionBackend renders text, layout or descriptions from images or
// document pages (Section 14.4, vision_extract contract).
type VisionBackend interface {
	Extract(ctx context.Context, input string, mode string, pages []int) (VisionResult, error)
}

// VisionResult is one vision extraction per page.
type VisionResult struct {
	Model string       `json:"model"`
	Pages []VisionPage `json:"pages"`
}

// VisionPage carries text, boxes and provenance.
type VisionPage struct {
	Page        int      `json:"page"`
	Text        string   `json:"text"`
	Boxes       []string `json:"boxes,omitempty"`
	Description string   `json:"description,omitempty"`
}

// PDFBackend implements structural PDF operations (pdf_ops).
type PDFBackend interface {
	Merge(inputs []string, output string) error
	Split(input string, outputDir string) ([]string, error)
	ExtractPages(input string, pages []int, output string) error
	Rotate(input string, angle int, output string) error
	Annotate(input string, notes []string, output string) error
}

// ConvertBackend converts between office formats (file_convert).
type ConvertBackend interface {
	// Pairs lists supported "from->to" pairs from the converter
	// manifest (Section 14.7).
	Pairs() []string
	Convert(ctx context.Context, input, output, from, to string) error
}

// DiagramBackend renders diagrams to SVG, PNG or PDF (diagram_render).
type DiagramBackend interface {
	Render(ctx context.Context, source, dialect, outputFormat string) (string, error)
}

// ChartBackend renders charts from tabular data (chart_render).
type ChartBackend interface {
	Render(ctx context.Context, spec ChartSpec, outputFormat string) (string, error)
}

// ChartSpec is the chart input shape.
type ChartSpec struct {
	Kind   string        `json:"kind"`
	Title  string        `json:"title,omitempty"`
	X      []string      `json:"x,omitempty"`
	Series []ChartSeries `json:"series"`
}

// ChartSeries is one data series.
type ChartSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

// MediaDeps wires every document/media backend.
type MediaDeps struct {
	// Workdir confines Office and PDF paths to the run workspace when set,
	// resolving relative paths against it.
	Workdir   string
	Documents DocumentBackend
	Vision    VisionBackend
	PDF       PDFBackend
	// PDFText and PDFRender are optional reading hooks supplied by the PDF reader.
	PDFText   docextract.Extractor
	PDFRender docextract.Renderer
	// PDFSecret resolves secret:// password references privately at invocation.
	PDFSecret func(context.Context, string) (string, error)
	Convert   ConvertBackend
	Diagram   DiagramBackend
	Chart     ChartBackend
	Now       func() time.Time
}

// ---- default backends ----

// FileDocumentBackend reads text-family formats with anchors.
type FileDocumentBackend struct{}

func docFormat(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return "txt"
	}
	return ext
}

var supportedDocFormats = map[string]bool{
	"txt": true, "md": true, "markdown": true, "json": true,
	"html": true, "htm": true, "csv": true, "log": true,
}

// Read implements DocumentBackend.
func (b *FileDocumentBackend) Read(ctx context.Context, path string) (DocumentText, error) {
	format := docFormat(path)
	if nativeDocumentFormat(format) {
		return (&OfficeDocumentBackend{}).Read(ctx, path)
	}
	if !supportedDocFormats[format] {
		return DocumentText{}, fmt.Errorf("unsupported format %q (want txt, md, json, html, csv or log)", format)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DocumentText{}, fmt.Errorf("read %s: %w", path, err)
	}
	text := string(data)
	if format == "html" || format == "htm" {
		text = stripHTML(text)
	}
	sections := extractSections(text, format)
	pages := paginate(text, 2000)
	return DocumentText{Path: path, Format: format, Pages: pages, Sections: sections}, nil
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteByte(' ')
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	fields := strings.Fields(b.String())
	return strings.Join(fields, " ")
}

func extractSections(text, format string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if format == "md" || format == "markdown" {
			if strings.HasPrefix(t, "#") {
				title := strings.TrimSpace(strings.TrimLeft(t, "#"))
				if title != "" {
					out = append(out, title)
				}
			}
			continue
		}
		if t != "" && len(t) < 120 && strings.ToUpper(t) == t && len(strings.Fields(t)) <= 8 {
			out = append(out, t)
		}
	}
	return out
}

func paginate(text string, pageSize int) []DocumentPage {
	if text == "" {
		return []DocumentPage{{Page: 1, Text: ""}}
	}
	paras := strings.Split(text, "\n\n")
	var pages []DocumentPage
	cur := strings.Builder{}
	curParas := []string{}
	pageNo := 1
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		pages = append(pages, DocumentPage{Page: pageNo, Paragraphs: append([]string(nil), curParas...), Text: cur.String()})
		pageNo++
		cur.Reset()
		curParas = nil
	}
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if cur.Len()+len(p)+2 > pageSize && cur.Len() > 0 {
			flush()
		}
		if cur.Len() > 0 {
			cur.WriteString("\n\n")
		}
		cur.WriteString(p)
		curParas = append(curParas, p)
	}
	flush()
	if len(pages) == 0 {
		pages = append(pages, DocumentPage{Page: 1, Text: text, Paragraphs: []string{text}})
	}
	return pages
}

// MemoryVisionBackend is the vision_extract contract fake: deterministic
// placeholder text, boxes and provenance without calling a model. It is
// never the default backend; production runs without a vision model get
// an explicit error from VisionExtractTool.
type MemoryVisionBackend struct {
	Model string
}

func (b *MemoryVisionBackend) model() string {
	if b != nil && b.Model != "" {
		return b.Model
	}
	return "vision-fake-1"
}

// Extract implements VisionBackend.
func (b *MemoryVisionBackend) Extract(_ context.Context, input string, mode string, pages []int) (VisionResult, error) {
	switch mode {
	case "text", "layout", "describe":
	default:
		return VisionResult{}, fmt.Errorf("unknown mode %q (want text, layout or describe)", mode)
	}
	if strings.TrimSpace(input) == "" {
		return VisionResult{}, fmt.Errorf("input is required")
	}
	data, readErr := os.ReadFile(input)
	sum := sha256.Sum256([]byte(input))
	if readErr == nil {
		sum = sha256.Sum256(data)
	}
	hash := hex.EncodeToString(sum[:])[:12]
	if len(pages) == 0 {
		pages = []int{1}
	}
	out := VisionResult{Model: b.model()}
	for _, p := range pages {
		vp := VisionPage{Page: p, Text: fmt.Sprintf("[%s p%d %s] extracted text placeholder (content %s)", mode, p, filepath.Base(input), hash)}
		if mode == "layout" {
			vp.Boxes = []string{fmt.Sprintf("box p%d x=0.05 y=0.05 w=0.90 h=0.12 text=%s", p, hash)}
		}
		if mode == "describe" {
			vp.Description = fmt.Sprintf("image %s page %d: placeholder description (content %s)", filepath.Base(input), p, hash)
		}
		out.Pages = append(out.Pages, vp)
	}
	return out, nil
}

// FilePDFBackend is a legacy text-fixture fake for PDFBackend tests. It does
// not process actual PDF objects and must never be selected by default.
type FilePDFBackend struct{}

func pdfPages(data []byte) [][]byte {
	text := string(data)
	chunks := strings.Split(text, "\f")
	var out [][]byte
	for _, c := range chunks {
		c = strings.Trim(c, "\n")
		if c == "" {
			continue
		}
		out = append(out, []byte(c))
	}
	if len(out) == 0 {
		return [][]byte{data}
	}
	return out
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pdfop-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Merge implements PDFBackend.
func (b *FilePDFBackend) Merge(inputs []string, output string) error {
	if len(inputs) < 2 {
		return fmt.Errorf("merge needs at least 2 inputs")
	}
	var bld strings.Builder
	for i, in := range inputs {
		data, err := os.ReadFile(in)
		if err != nil {
			return fmt.Errorf("read %s: %w", in, err)
		}
		if i > 0 {
			bld.WriteString("\n\f\n")
		}
		bld.Write(data)
	}
	bld.WriteString(fmt.Sprintf("\n%% merged %d inputs\n", len(inputs)))
	return writeAtomic(output, []byte(bld.String()))
}

// Split implements PDFBackend.
func (b *FilePDFBackend) Split(input string, outputDir string) ([]string, error) {
	data, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", input, err)
	}
	pages := pdfPages(data)
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	var outs []string
	for i, p := range pages {
		out := filepath.Join(outputDir, fmt.Sprintf("%s-p%02d.pdf", base, i+1))
		if err := writeAtomic(out, p); err != nil {
			return nil, err
		}
		outs = append(outs, out)
	}
	return outs, nil
}

// ExtractPages implements PDFBackend.
func (b *FilePDFBackend) ExtractPages(input string, pages []int, output string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	all := pdfPages(data)
	var sel [][]byte
	for _, p := range pages {
		if p < 1 || p > len(all) {
			return fmt.Errorf("page %d out of range (1..%d)", p, len(all))
		}
		sel = append(sel, all[p-1])
	}
	joined := strings.Join(bytesToStrings(sel), "\n\f\n")
	return writeAtomic(output, []byte(joined))
}

// Rotate implements PDFBackend.
func (b *FilePDFBackend) Rotate(input string, angle int, output string) error {
	if angle%90 != 0 {
		return fmt.Errorf("angle %d must be a multiple of 90", angle)
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	out := string(data) + fmt.Sprintf("\n%% rotated %d deg\n", angle%360)
	return writeAtomic(output, []byte(out))
}

// Annotate implements PDFBackend.
func (b *FilePDFBackend) Annotate(input string, notes []string, output string) error {
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	var bld strings.Builder
	bld.Write(data)
	bld.WriteString("\n%% annotations:\n")
	for _, n := range notes {
		bld.WriteString("%% - " + n + "\n")
	}
	return writeAtomic(output, []byte(bld.String()))
}

func bytesToStrings(in [][]byte) []string {
	out := make([]string, 0, len(in))
	for _, b := range in {
		out = append(out, string(b))
	}
	return out
}

// FileConvertBackend converts text-family formats without supposing
// universal compatibility: only listed pairs run.
type FileConvertBackend struct{}

var convertPairs = []string{
	"md->html", "txt->md", "md->txt", "json->md", "csv->json", "html->md",
}

// Pairs implements ConvertBackend.
func (b *FileConvertBackend) Pairs() []string { return append([]string(nil), convertPairs...) }

func convertPairSupported(from, to string) bool {
	want := strings.ToLower(from) + "->" + strings.ToLower(to)
	for _, p := range convertPairs {
		if p == want {
			return true
		}
	}
	return false
}

// Convert implements ConvertBackend.
func (b *FileConvertBackend) Convert(_ context.Context, input, output, from, to string) error {
	if from == "" {
		from = docFormat(input)
	}
	if to == "" {
		to = docFormat(output)
	}
	from = strings.ToLower(strings.TrimPrefix(from, "."))
	to = strings.ToLower(strings.TrimPrefix(to, "."))
	if !convertPairSupported(from, to) {
		return fmt.Errorf("unsupported pair %s->%s (supported: %s)", from, to, strings.Join(convertPairs, ", "))
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return fmt.Errorf("read %s: %w", input, err)
	}
	var out string
	switch from + "->" + to {
	case "md->html":
		out = "<html><body>\n" + mdToHTML(string(data)) + "\n</body></html>\n"
	case "html->md":
		out = stripHTML(string(data)) + "\n"
	case "txt->md":
		out = string(data)
		if !strings.HasPrefix(out, "# ") {
			out = "# Converted\n\n" + out
		}
	case "md->txt":
		out = stripMarkdown(string(data))
	case "json->md":
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("parse json: %w", err)
		}
		pretty, _ := json.MarshalIndent(v, "", "  ")
		out = "# JSON document\n\n```json\n" + string(pretty) + "\n```\n"
	case "csv->json":
		out = csvToJSON(string(data))
	default:
		return fmt.Errorf("unsupported pair %s->%s", from, to)
	}
	return writeAtomic(output, []byte(out))
}

func mdToHTML(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "### "):
			fmt.Fprintf(&b, "<h3>%s</h3>\n", strings.TrimSpace(t[4:]))
		case strings.HasPrefix(t, "## "):
			fmt.Fprintf(&b, "<h2>%s</h2>\n", strings.TrimSpace(t[3:]))
		case strings.HasPrefix(t, "# "):
			fmt.Fprintf(&b, "<h1>%s</h1>\n", strings.TrimSpace(t[2:]))
		case t == "":
			b.WriteString("\n")
		default:
			fmt.Fprintf(&b, "<p>%s</p>\n", t)
		}
	}
	return b.String()
}

func stripMarkdown(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		t = strings.TrimLeft(t, "#>*- ")
		b.WriteString(t + "\n")
	}
	return b.String()
}

func csvToJSON(csv string) string {
	lines := strings.Split(strings.TrimSpace(csv), "\n")
	if len(lines) < 2 {
		return "[]\n"
	}
	headers := strings.Split(lines[0], ",")
	for i := range headers {
		headers[i] = strings.TrimSpace(headers[i])
	}
	rows := make([]map[string]string, 0, len(lines)-1)
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		cells := strings.Split(ln, ",")
		row := map[string]string{}
		for i, h := range headers {
			v := ""
			if i < len(cells) {
				v = strings.TrimSpace(cells[i])
			}
			row[h] = v
		}
		rows = append(rows, row)
	}
	raw, _ := json.MarshalIndent(rows, "", "  ")
	return string(raw) + "\n"
}

// PlaceholderDiagramBackend is an explicit dry-run/test backend. Production
// always uses SVGDiagramBackend and never falls back to a source-text box.
type PlaceholderDiagramBackend struct{}

func (*PlaceholderDiagramBackend) Render(_ context.Context, source, dialect, format string) (string, error) {
	if format != "" && format != "svg" {
		return "", fmt.Errorf("dry-run backend only supports svg")
	}
	if err := checkDiagramSyntax(source, dialect); err != nil {
		return "", err
	}
	return `<svg xmlns="http://www.w3.org/2000/svg"><text>` + xmlEscape(source) + `</text></svg>`, nil
}

func checkDiagramSyntax(source, dialect string) error {
	s := strings.TrimSpace(source)
	if s == "" {
		return fmt.Errorf("%s: empty diagram source", dialect)
	}
	switch dialect {
	case "mermaid":
		lower := strings.ToLower(s)
		known := []string{"graph ", "flowchart ", "sequencediagram", "sequenceDiagram", "classdiagram", "classDiagram", "statediagram", "stateDiagram", "gantt", "pie", "erdiagram", "erDiagram"}
		for _, k := range known {
			if strings.Contains(lower, strings.ToLower(k)) {
				return nil
			}
		}
		return fmt.Errorf("mermaid: want a diagram type (graph, flowchart, sequenceDiagram, classDiagram, stateDiagram, gantt, pie, erDiagram), got %q", firstLine(s))
	case "d2":
		// D2 accepts a single node as well as connections; its parser validates the grammar.
		return nil
	case "plantuml":
		if !strings.Contains(s, "@startuml") || !strings.Contains(s, "@enduml") {
			return fmt.Errorf("plantuml: want @startuml … @enduml, got %q", firstLine(s))
		}
		return nil
	case "graphviz", "dot":
		if !strings.Contains(strings.ToLower(s), "digraph") && !strings.Contains(strings.ToLower(s), "graph") {
			return fmt.Errorf("graphviz: want digraph or graph { … }, got %q", firstLine(s))
		}
		return nil
	case "svg":
		if !strings.Contains(s, "<svg") {
			return fmt.Errorf("svg: want an <svg> document, got %q", firstLine(s))
		}
		return nil
	case "drawio", "draw.io":
		if !strings.Contains(s, "<mxfile") && !strings.Contains(s, "<diagram") {
			return fmt.Errorf("drawio: want <mxfile> or <diagram>, got %q", firstLine(s))
		}
		return nil
	default:
		return fmt.Errorf("unknown dialect %q (want mermaid, d2, plantuml, graphviz, svg or drawio)", dialect)
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	esc := r.Replace(s)
	if len(esc) > 2000 {
		esc = esc[:2000] + "…"
	}
	return esc
}

// SVGChartBackend renders bar and line charts to SVG.
type SVGChartBackend struct{}

// Render implements ChartBackend.
func (b *SVGChartBackend) Render(_ context.Context, spec ChartSpec, outputFormat string) (string, error) {
	if outputFormat == "" {
		outputFormat = "svg"
	}
	if strings.ToLower(outputFormat) != "svg" {
		return "", fmt.Errorf("output format %q needs an external rasterizer; harness renders svg", outputFormat)
	}
	switch strings.ToLower(spec.Kind) {
	case "bar", "line":
	default:
		return "", fmt.Errorf("unknown chart kind %q (want bar or line)", spec.Kind)
	}
	if len(spec.Series) == 0 {
		return "", fmt.Errorf("chart needs at least one series")
	}
	maxV := 0.0
	for _, s := range spec.Series {
		if len(s.Values) == 0 {
			return "", fmt.Errorf("series %q has no values", s.Name)
		}
		for _, v := range s.Values {
			if v > maxV {
				maxV = v
			}
		}
	}
	if maxV <= 0 {
		maxV = 1
	}
	const W, H = 640, 360
	const padL, padB, padT, padR = 50, 40, 30, 20
	plotW := W - padL - padR
	plotH := H - padT - padB
	n := 0
	for _, s := range spec.Series {
		if len(s.Values) > n {
			n = len(s.Values)
		}
	}
	var bld strings.Builder
	fmt.Fprintf(&bld, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" role="img">`+"\n", W, H)
	fmt.Fprintf(&bld, `<rect width="%d" height="%d" fill="white" stroke="black"/>"`+"\n", W, H)
	if spec.Title != "" {
		fmt.Fprintf(&bld, `<text x="%d" y="20" text-anchor="middle" font-size="14">%s</text>`+"\n", W/2, xmlEscape(spec.Title))
	}
	fmt.Fprintf(&bld, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black"/>`+"\n", padL, padT, padL, H-padB)
	fmt.Fprintf(&bld, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="black"/>`+"\n", padL, H-padB, W-padR, H-padB)
	colors := []string{"#2563eb", "#dc2626", "#059669", "#d97706", "#7c3aed"}
	if strings.ToLower(spec.Kind) == "bar" {
		groups := float64(n)
		barW := float64(plotW) / (groups * float64(len(spec.Series)+1))
		for si, s := range spec.Series {
			for i, v := range s.Values {
				x := float64(padL) + (float64(i)+0.5*float64(si+1)/float64(len(spec.Series)+1))*float64(plotW)/groups
				h := v / maxV * float64(plotH)
				y := float64(H-padB) - h
				fmt.Fprintf(&bld, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %.2f</title></rect>`+"\n",
					x, y, barW, h, colors[si%len(colors)], xmlEscape(s.Name), v)
			}
		}
	} else {
		for si, s := range spec.Series {
			var pts []string
			for i, v := range s.Values {
				x := float64(padL) + float64(i)*float64(plotW)/float64(maxInt(n-1, 1))
				y := float64(H-padB) - v/maxV*float64(plotH)
				pts = append(pts, fmt.Sprintf("%.1f,%.1f", x, y))
			}
			fmt.Fprintf(&bld, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`+"\n",
				strings.Join(pts, " "), colors[si%len(colors)])
		}
	}
	bld.WriteString("</svg>\n")
	return bld.String(), nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- tool definitions ----

var documentReadSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":      map[string]any{"type": "string", "minLength": 1},
		"page":      map[string]any{"type": "integer", "minimum": 1},
		"cursor":    map[string]any{"type": "string"},
		"max_chars": map[string]any{"type": "integer", "minimum": 1, "maximum": 100000},
	},
	"required":             []string{"path"},
	"additionalProperties": false,
})

var pdfOpsSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"action":      map[string]any{"type": "string", "enum": []string{"info", "merge", "split", "extract_pages", "delete_pages", "reorder", "rotate", "stamp", "watermark", "page_numbers", "encrypt", "decrypt", "optimize", "validate", "form_fields", "form_fill", "fill_form", "form_lock", "form_flatten", "flatten", "attachments", "bookmarks", "text", "render", "annotate", "redact"}},
		"inputs":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
		"output":      map[string]any{"type": "string"},
		"output_dir":  map[string]any{"type": "string"},
		"overwrite":   map[string]any{"type": "boolean", "default": false},
		"password":    map[string]any{"type": "string", "description": "Password or secret:// reference; prefer a reference"},
		"text":        map[string]any{"type": "string"},
		"ranges":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"fields":      map[string]any{"type": "object", "additionalProperties": map[string]any{"type": []string{"string", "boolean", "array"}}},
		"field_names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"page":        map[string]any{"type": "integer", "minimum": 1},
		"dpi":         map[string]any{"type": "integer", "minimum": 36, "maximum": 300},
		"pages":       map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
		"angle":       map[string]any{"type": "integer"},
		"notes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required":             []string{"action", "inputs"},
	"additionalProperties": false,
})

var fileConvertSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"input":  map[string]any{"type": "string", "minLength": 1},
		"output": map[string]any{"type": "string", "minLength": 1},
		"from":   map[string]any{"type": "string"},
		"to":     map[string]any{"type": "string"},
		"list":   map[string]any{"type": "boolean"},
	},
	"required":             []string{"input", "output"},
	"additionalProperties": false,
})

var diagramRenderSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"title":         map[string]any{"type": "string"},
		"source":        map[string]any{"type": "string", "minLength": 1},
		"dialect":       map[string]any{"type": "string", "enum": []string{"mermaid", "d2", "plantuml", "graphviz", "svg", "drawio"}},
		"output_format": map[string]any{"type": "string", "enum": []string{"svg", "png", "pdf"}},
		"output":        map[string]any{"type": "string"},
	},
	"required":             []string{"source", "dialect"},
	"additionalProperties": false,
})

var chartRenderSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"kind":  map[string]any{"type": "string", "enum": []string{"bar", "line"}},
		"title": map[string]any{"type": "string"},
		"x":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"series": map[string]any{
			"type": "array", "minItems": 1,
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"values": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 1},
				},
				"required": []string{"name", "values"},
			},
		},
		"output_format": map[string]any{"type": "string", "enum": []string{"svg", "png", "pdf"}},
		"output":        map[string]any{"type": "string"},
	},
	"required":             []string{"kind", "series"},
	"additionalProperties": false,
})

var visionExtractSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"input": map[string]any{"type": "string", "minLength": 1},
		"mode":  map[string]any{"type": "string", "enum": []string{"text", "layout", "describe"}},
		"pages": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "minimum": 1}},
		"model": map[string]any{"type": "string"},
	},
	"required":             []string{"input"},
	"additionalProperties": false,
})

// DocumentReadTool implements document_read.
type DocumentReadTool struct{ Deps *MediaDeps }

func (t *DocumentReadTool) backend() DocumentBackend {
	if t.Deps != nil && t.Deps.Documents != nil {
		return t.Deps.Documents
	}
	return &FileDocumentBackend{}
}

func (t *DocumentReadTool) Definition() Definition {
	return Definition{
		Name: "document_read", Description: "Extract text and structure from any supported format with page, section and paragraph anchors",
		InputSchema: documentReadSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "documents", Tags: []string{"document", "read"}, Keywords: []string{"document", "read", "extract", "text", "pdf", "format", "anchors"},
	}
}

func (t *DocumentReadTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Path     string `json:"path"`
		Page     int    `json:"page"`
		Cursor   string `json:"cursor"`
		MaxChars int    `json:"max_chars"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return Result{Status: StatusInputError, Output: "path is required"}, nil
	}
	doc, err := t.backend().Read(ctx, filepath.Clean(a.Path))
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}

	window, err := documentReadWindow(doc, a.Page, a.Cursor, a.MaxChars)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	raw, _ := json.MarshalIndent(window, "", "  ")
	out := string(raw)
	return Result{Status: StatusOK, Output: out}, nil
}

func (t *DocumentReadTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + filepath.Clean(a.Path)}, nil
}

// PDFOpsTool implements pdf_ops.
type PDFOpsTool struct{ Deps *MediaDeps }

func (t *PDFOpsTool) backend() PDFBackend {
	if t.Deps != nil && t.Deps.PDF != nil {
		return t.Deps.PDF
	}
	return &CPUPDFBackend{}
}

func (t *PDFOpsTool) Definition() Definition {
	return Definition{
		Name: "pdf_ops", Description: "Inspect and edit PDFs, fill AcroForms, stamp text, encrypt, validate and optimize",
		InputSchema: pdfOpsSchema, Effect: "write_local", Idempotency: "non_idempotent",
		Cancel: "after_completion", Timeout: 120_000_000_000, OutputBudget: DefaultOutputBudget,

		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "documents", Tags: []string{"pdf", "document"}, Keywords: []string{"pdf", "merge", "split", "rotate", "extract", "annotate", "redact", "form"},
	}
}

func (t *PDFOpsTool) invokeLegacy(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Action    string   `json:"action"`
		Inputs    []string `json:"inputs"`
		Output    string   `json:"output"`
		OutputDir string   `json:"output_dir"`
		Pages     []int    `json:"pages"`
		Angle     int      `json:"angle"`
		Notes     []string `json:"notes"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if len(a.Inputs) == 0 {
		return Result{Status: StatusInputError, Output: "inputs must not be empty"}, nil
	}
	backend := t.backend()
	switch a.Action {
	case "merge":
		if a.Output == "" {
			return Result{Status: StatusInputError, Output: "output is required for merge"}, nil
		}
		if err := backend.Merge(a.Inputs, a.Output); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("merged %d inputs into %s", len(a.Inputs), a.Output)}, nil
	case "split":
		dir := a.OutputDir
		if dir == "" {
			dir = "."
		}
		outs, err := backend.Split(a.Inputs[0], dir)
		if err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		raw, _ := json.Marshal(map[string]any{"outputs": outs})
		return Result{Status: StatusOK, Output: string(raw)}, nil
	case "extract_pages":
		if a.Output == "" {
			return Result{Status: StatusInputError, Output: "output is required for extract_pages"}, nil
		}
		if len(a.Pages) == 0 {
			return Result{Status: StatusInputError, Output: "pages is required for extract_pages"}, nil
		}
		if err := backend.ExtractPages(a.Inputs[0], a.Pages, a.Output); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("extracted %d page(s) from %s into %s", len(a.Pages), a.Inputs[0], a.Output)}, nil
	case "rotate":
		if a.Output == "" {
			return Result{Status: StatusInputError, Output: "output is required for rotate"}, nil
		}
		if err := backend.Rotate(a.Inputs[0], a.Angle, a.Output); err != nil {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("rotated %s by %d deg into %s", a.Inputs[0], a.Angle, a.Output)}, nil
	case "fill_form", "annotate", "redact":
		if a.Output == "" {
			return Result{Status: StatusInputError, Output: "output is required"}, nil
		}
		notes := a.Notes
		if a.Action == "redact" && len(notes) == 0 {
			return Result{Status: StatusInputError, Output: "notes (redaction marks) are required for redact"}, nil
		}
		if a.Action == "fill_form" && len(notes) == 0 {
			notes = []string{"form fill recorded"}
		}
		if err := backend.Annotate(a.Inputs[0], append([]string{a.Action}, notes...), a.Output); err != nil {
			return Result{Status: StatusToolError, Output: err.Error()}, nil
		}
		return Result{Status: StatusOK, Output: fmt.Sprintf("%s %s into %s", a.Action, a.Inputs[0], a.Output)}, nil
	default:
		return Result{Status: StatusInputError, Output: fmt.Sprintf("unknown action %q", a.Action)}, nil
	}
}

// FileConvertTool implements file_convert.
type FileConvertTool struct{ Deps *MediaDeps }

func (t *FileConvertTool) backend() ConvertBackend {
	if t.Deps != nil && t.Deps.Convert != nil {
		return t.Deps.Convert
	}
	return &FileConvertBackend{}
}

func (t *FileConvertTool) Definition() Definition {
	return Definition{
		Name: "file_convert", Description: "Convert between office formats with the pairs declared in the converter manifest",
		InputSchema: fileConvertSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 120_000_000_000, OutputBudget: DefaultOutputBudget,
		Inverse: "revert_edit", Probe: "output file exists",
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "documents", Tags: []string{"convert", "document"}, Keywords: []string{"convert", "format", "markdown", "html", "json", "csv", "office"},
	}
}

func (t *FileConvertTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Input  string `json:"input"`
		Output string `json:"output"`
		From   string `json:"from"`
		To     string `json:"to"`
		List   bool   `json:"list"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	backend := t.backend()
	if a.List || (a.Input == "" && a.Output == "") {
		raw, _ := json.Marshal(map[string]any{"pairs": backend.Pairs()})
		return Result{Status: StatusOK, Output: string(raw)}, nil
	}
	if a.Input == "" || a.Output == "" {
		return Result{Status: StatusInputError, Output: "input and output are required (or list:true)"}, nil
	}
	if err := backend.Convert(ctx, a.Input, a.Output, a.From, a.To); err != nil {
		if strings.HasPrefix(err.Error(), "unsupported pair") {
			return Result{Status: StatusInputError, Output: err.Error()}, nil
		}
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return Result{Status: StatusOK, Output: fmt.Sprintf("converted %s into %s", a.Input, a.Output)}, nil
}

func (t *FileConvertTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + filepath.Clean(a.Input), "file:" + filepath.Clean(a.Output)}, nil
}

// DiagramRenderTool implements diagram_render.
type DiagramRenderTool struct{ Deps *MediaDeps }

func (t *DiagramRenderTool) backend() DiagramBackend {
	if t.Deps != nil && t.Deps.Diagram != nil {
		return t.Deps.Diagram
	}
	return &SVGDiagramBackend{}
}

func (t *DiagramRenderTool) Definition() Definition {
	return Definition{
		Name: "diagram_render", Description: "Use this tool to produce diagrams from Mermaid, D2, Graphviz, PlantUML, SVG or draw.io. Saves rendered SVG, PNG or PDF artifacts and reports syntax errors.",
		InputSchema: diagramRenderSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "media", Tags: []string{"diagram"}, Keywords: []string{"diagram", "render", "mermaid", "d2", "plantuml", "graphviz", "svg", "drawio", "visualize", "flowchart", "sequence", "architecture", "diagrama", "gráfico", "esquema", "flujo"},
	}
}

func (t *DiagramRenderTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a diagramRenderArguments
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	artifact, err := a.artifact(t.Deps)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	backend := t.backend()
	used := "configured diagram backend"
	var data string
	if reporter, ok := backend.(interface {
		renderWithReport(context.Context, string, string, string) (string, string, error)
	}); ok {
		data, used, err = reporter.renderWithReport(ctx, a.Source, artifact.Dialect, artifact.Format)
	} else {
		data, err = backend.Render(ctx, a.Source, artifact.Dialect, artifact.Format)
		if _, dryRun := backend.(*PlaceholderDiagramBackend); dryRun {
			used = "explicit dry-run backend"
		}
	}
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if err := saveVisualArtifact(artifact, data, a.Source); err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	output := fmt.Sprintf("rendered %s diagram (%d bytes) into %s using %s; source: %s", artifact.Dialect, len(data), artifact.Path, used, artifact.SourcePath)
	if a.Output == "" && artifact.Format == "svg" {
		output += "\n" + data
	}
	return Result{Status: StatusOK, Output: output, Artifacts: []ArtifactRef{artifact}}, nil
}

func (t *DiagramRenderTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a diagramRenderArguments
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	artifact, err := a.artifact(t.Deps)
	if err != nil {
		return nil, err
	}
	return visualArtifactKeys(artifact), nil
}

// ChartRenderTool implements chart_render.
type ChartRenderTool struct{ Deps *MediaDeps }

func (t *ChartRenderTool) backend() ChartBackend {
	if t.Deps != nil && t.Deps.Chart != nil {
		return t.Deps.Chart
	}
	return &SVGChartBackend{}
}

func (t *ChartRenderTool) Definition() Definition {
	return Definition{
		Name: "chart_render", Description: "Generate a chart from tabular data and a specification",
		InputSchema: chartRenderSchema, Effect: "write_local", Idempotency: "idempotent",
		Cancel: "after_completion", Timeout: 60_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "media", Tags: []string{"chart"}, Keywords: []string{"chart", "render", "graph", "bar", "line", "data", "plot", "visualize", "gráfico"},
	}
}

type chartRenderArguments struct {
	Kind         string        `json:"kind"`
	Title        string        `json:"title"`
	X            []string      `json:"x"`
	Series       []ChartSeries `json:"series"`
	OutputFormat string        `json:"output_format"`
	Output       string        `json:"output"`
}

func (a chartRenderArguments) spec() ChartSpec {
	return ChartSpec{Kind: a.Kind, Title: a.Title, X: a.X, Series: a.Series}
}
func (a chartRenderArguments) artifact(deps *MediaDeps) (ArtifactRef, string, error) {
	format, err := visualOutputFormat(a.OutputFormat, a.Output)
	if err != nil {
		return ArtifactRef{}, "", err
	}
	source, err := json.MarshalIndent(a.spec(), "", "  ")
	if err != nil {
		return ArtifactRef{}, "", err
	}
	title := a.Title
	if title == "" {
		title = a.Kind + " chart"
	}
	artifact, err := visualArtifactPaths(deps, a.Output, "chart", title, "", format, string(source))
	return artifact, string(source), err
}
func (t *ChartRenderTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a chartRenderArguments
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	artifact, source, err := a.artifact(t.Deps)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	data, err := t.backend().Render(ctx, a.spec(), artifact.Format)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if err := saveVisualArtifact(artifact, data, source); err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	output := fmt.Sprintf("rendered %s chart (%d bytes) into %s; source: %s", a.Kind, len(data), artifact.Path, artifact.SourcePath)
	if a.Output == "" && artifact.Format == "svg" {
		output += "\n" + data
	}
	return Result{Status: StatusOK, Output: output, Artifacts: []ArtifactRef{artifact}}, nil
}

func (t *ChartRenderTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a chartRenderArguments
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	artifact, _, err := a.artifact(t.Deps)
	if err != nil {
		return nil, err
	}
	return visualArtifactKeys(artifact), nil
}

// VisionExtractTool implements the vision_extract contract (14.4).
type VisionExtractTool struct{ Deps *MediaDeps }

// backend returns the configured vision model. With none configured it
// returns nil, and the tool reports that instead of inventing text
// (EXCALIBUR_VISION_HONEST). The placeholder backend remains only for
// the switch-off ablation and for tests that set it explicitly.
func (t *VisionExtractTool) backend() VisionBackend {
	if t.Deps != nil && t.Deps.Vision != nil {
		return t.Deps.Vision
	}
	if !VisionHonestEnabled() {
		return &MemoryVisionBackend{}
	}
	return nil
}

func (t *VisionExtractTool) Definition() Definition {
	return Definition{
		Name: "vision_extract", Description: "Send images or document pages to the vision model and get text, layout boxes or a description per page",
		InputSchema: visionExtractSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 120_000_000_000, OutputBudget: DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "media", Tags: []string{"vision"}, Keywords: []string{"vision", "extract", "image", "ocr", "layout", "describe", "capture"},
	}
}

func (t *VisionExtractTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Input string `json:"input"`
		Mode  string `json:"mode"`
		Pages []int  `json:"pages"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	mode := a.Mode
	if mode == "" {
		mode = "text"
	}
	backend := t.backend()
	if backend == nil {
		return Result{Status: StatusToolError, Output: "vision_extract: no image model is configured in this run; use local tools (OCR, image libraries) instead"}, nil
	}
	res, err := backend.Extract(ctx, a.Input, mode, a.Pages)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if a.Model != "" {
		// Preserve the serving model for real extraction; legacy fakes accept labels.
		if _, real := backend.(*ModelVisionBackend); !real {
			res.Model = a.Model
		}
	}
	raw, _ := json.MarshalIndent(res, "", "  ")
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

func (t *VisionExtractTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"file:" + filepath.Clean(a.Input)}, nil
}

// DeferredMediaTools returns the harness-owned deferred document and media
// tools of Section 13.3.
func DeferredMediaTools(deps *MediaDeps) []Tool {
	if deps == nil {
		deps = &MediaDeps{}
	}
	return []Tool{
		&DocumentReadTool{Deps: deps},
		&DocumentCreateTool{Deps: deps},
		&DocumentEditTool{Deps: deps},
		&PDFOpsTool{Deps: deps},
		&FileConvertTool{Deps: deps},
		&DiagramRenderTool{Deps: deps},
		&ChartRenderTool{Deps: deps},
		&VisionExtractTool{Deps: deps},
	}
}

// RegisterDeferredMedia registers the harness media tools as deferred.
func RegisterDeferredMedia(reg *MapRegistry, cat *DeferredCatalog, deps *MediaDeps) error {
	for _, t := range DeferredMediaTools(deps) {
		if err := cat.RegisterDeferred(t); err != nil {
			return err
		}
	}
	return nil
}
