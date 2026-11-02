// Package read extracts native Office, OpenDocument and EPUB content without
// executing embedded code or fetching external resources.
package read

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

const MaxUncompressedSize int64 = 256 << 20
const MaxEntries = 4096

var ErrTooLarge = errors.New("document exceeds extraction limit")

// Extractor reads one of docx, xlsx, pptx, odt, ods, odp or epub.
// An empty Format detects the family from package parts.
type Extractor struct{ Format string }

func New(format string) *Extractor { return &Extractor{Format: strings.ToLower(format)} }
func (e *Extractor) Formats() []string {
	if e.Format != "" {
		return []string{e.Format}
	}
	return []string{"docx", "xlsx", "pptx", "odt", "ods", "odp", "epub"}
}

// Enabled controls native extraction for both library and document tools.
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_OFFICE_READ")))
	return v != "0" && v != "false" && v != "off"
}

type archive struct {
	files    map[string]*zip.File
	ctx      context.Context
	maxPages int
}

func open(ctx context.Context, r io.ReaderAt, size int64) (*archive, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size < 0 || size > MaxUncompressedSize {
		return nil, ErrTooLarge
	}
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	if len(z.File) > MaxEntries {
		return nil, ErrTooLarge
	}
	a := &archive{files: map[string]*zip.File{}, ctx: ctx}
	var total uint64
	for _, f := range z.File {
		n := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(n, "/") || strings.Contains(n, ":") || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe zip entry %q", f.Name)
		}
		for _, p := range strings.Split(n, "/") {
			if p == ".." {
				return nil, fmt.Errorf("unsafe zip entry %q", f.Name)
			}
		}
		if total > uint64(MaxUncompressedSize) || f.UncompressedSize64 > uint64(MaxUncompressedSize)-total {
			return nil, ErrTooLarge
		}
		total += f.UncompressedSize64
		n = path.Clean(n)
		if _, ok := a.files[n]; ok {
			return nil, fmt.Errorf("duplicate zip entry %q", n)
		}
		a.files[n] = f
	}
	return a, nil
}

type node struct {
	name     string
	attrs    []xml.Attr
	children []*node
	content  []any
}

func (n *node) attr(key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
func (n *node) all(name string) []*node {
	var out []*node
	var walk func(*node)
	walk = func(v *node) {
		if v == nil {
			return
		}
		if v.name == name {
			out = append(out, v)
		}
		for _, c := range v.children {
			walk(c)
		}
	}
	walk(n)
	return out
}
func (n *node) first(name string) *node {
	a := n.all(name)
	if len(a) > 0 {
		return a[0]
	}
	return nil
}
func (n *node) text() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for _, v := range n.content {
		switch x := v.(type) {
		case string:
			b.WriteString(x)
		case *node:
			b.WriteString(x.text())
		}
	}
	return b.String()
}
func (n *node) texts(tag string) string {
	var b strings.Builder
	for _, v := range n.all(tag) {
		b.WriteString(v.text())
	}
	return b.String()
}
func (a *archive) xml(name string) (*node, error) {
	f := a.files[name]
	if f == nil {
		return nil, fmt.Errorf("missing package part %s", name)
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return parseXML(a.ctx, io.LimitReader(r, MaxUncompressedSize+1))
}

// The token decoder never resolves entities or loads DTDs. A bounded tree is
// retained for each part so cross references can be resolved deterministically.
func parseXML(ctx context.Context, r io.Reader) (*node, error) {
	d := xml.NewDecoder(&cancelReader{ctx: ctx, r: r})
	d.Entity = xml.HTMLEntity
	root := &node{}
	stack := []*node{root}
	count := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		count++
		if count > 3000000 {
			return nil, ErrTooLarge
		}
		switch v := t.(type) {
		case xml.StartElement:
			if count > 1000000 || len(stack) > 128 {
				return nil, ErrTooLarge
			}
			n := &node{name: v.Name.Local, attrs: v.Attr}
			p := stack[len(stack)-1]
			p.children = append(p.children, n)
			p.content = append(p.content, n)
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			p := stack[len(stack)-1]
			p.content = append(p.content, string(v))
		case xml.Directive:
			if strings.Contains(strings.ToUpper(string(v)), "ENTITY") {
				return nil, errors.New("XML entity declarations are unsupported")
			}
		}
	}
	return root, nil
}
func (a *archive) rels(part string) map[string]string {
	out := map[string]string{}
	n, err := a.xml(path.Join(path.Dir(part), "_rels", path.Base(part)+".rels"))
	if err != nil {
		return out
	}
	for _, r := range n.all("Relationship") {
		if r.attr("TargetMode") == "External" {
			continue
		}
		target := r.attr("Target")
		if strings.Contains(target, "\\") {
			continue
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join(path.Dir(part), target)
		}
		if target != ".." && !strings.HasPrefix(target, "../") {
			out[r.attr("Id")] = target
		}
	}
	return out
}
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts docextract.Options) (docextract.Document, error) {
	if !Enabled() {
		return docextract.Document{}, errors.New("native Office extraction disabled")
	}
	a, err := open(ctx, r, size)
	if err != nil {
		return docextract.Document{}, err
	}
	a.maxPages = opts.MaxPages
	if a.maxPages <= 0 {
		a.maxPages = 2000
	}
	format := e.Format
	if format == "" {
		switch {
		case a.files["word/document.xml"] != nil:
			format = "docx"
		case a.files["xl/workbook.xml"] != nil:
			format = "xlsx"
		case a.files["ppt/presentation.xml"] != nil:
			format = "pptx"
		case a.files["META-INF/container.xml"] != nil:
			format = "epub"
		case a.files["content.xml"] != nil:
			format = "odt"
		}
	}
	doc := docextract.Document{Format: format}
	if core, err := a.xml("docProps/core.xml"); err == nil {
		doc.Title = core.first("title").text()
		doc.Author = core.first("creator").text()
	}
	switch format {
	case "docx":
		err = a.docx(&doc)
	case "xlsx":
		err = a.xlsx(&doc)
	case "pptx":
		err = a.pptx(&doc)
	case "odt", "ods", "odp":
		err = a.odf(&doc)
	case "epub":
		err = a.epub(&doc)
	default:
		err = fmt.Errorf("unsupported Office format %q", format)
	}
	if err != nil {
		return docextract.Document{}, err
	}
	if err = ctx.Err(); err != nil {
		return docextract.Document{}, err
	}
	limit := opts.MaxPages
	if limit <= 0 {
		limit = 2000
	}
	if len(doc.Pages) > limit {
		return docextract.Document{}, ErrTooLarge
	}
	doc.PageCount = len(doc.Pages)
	var selected []docextract.Page
	for i, p := range doc.Pages {
		p.Number = i + 1
		if p.Method == "" {
			p.Method = docextract.MethodNative
			p.Quality = 1
		}
		for _, b := range p.Blocks {
			if b.Kind == docextract.KindHeading {
				doc.Outline = append(doc.Outline, docextract.OutlineEntry{Title: b.Text, Level: b.Level})
			}
		}
		if len(opts.Pages) == 0 || contains(opts.Pages, i+1) {
			selected = append(selected, p)
		}
		if opts.Progress != nil {
			opts.Progress(i+1, doc.PageCount)
		}
	}
	doc.Pages = selected
	return doc, nil
}
func contains(a []int, v int) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}
func integer(s string) int { v, _ := strconv.Atoi(s); return v }
func paragraph(text, anchor string) docextract.Block {
	return docextract.Block{Kind: docextract.KindParagraph, Text: strings.TrimSpace(text), Anchor: anchor}
}
func markdown(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}

	var b strings.Builder
	for i, r := range rows {
		b.WriteString("|")
		// Avoid amplifying a sparse adversarial table into a huge padded grid.
		rowcols := cols
		if len(rows) > 0 && cols > 1000000/len(rows) {
			rowcols = len(r)
		}
		for j := 0; j < rowcols; j++ {
			v := ""
			if j < len(r) {
				v = r[j]
			}
			v = strings.NewReplacer("|", "\\|", "\n", "<br>", "\r", "").Replace(v)
			b.WriteString(" " + v + " |")
		}
		b.WriteByte('\n')
		if i == 0 {
			b.WriteString("|")
			for j := 0; j < cols; j++ {
				b.WriteString(" --- |")
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}
func sortedRelationships(rels map[string]string) []string {
	keys := make([]string, 0, len(rels))
	for k := range rels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, rels[k])
	}
	return out
}

func (n *node) relID() string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return a.Value
		}
	}
	return n.attr("id")
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *cancelReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func (a *archive) checkPages(count int) error {
	if count > a.maxPages {
		return ErrTooLarge
	}
	return nil
}

// runText retains explicit line breaks and tabs between native text runs.
func runText(n *node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*node)
	walk = func(v *node) {
		switch v.name {
		case "t":
			b.WriteString(v.text())
			return
		case "tab":
			b.WriteByte('\t')
			return
		case "br", "cr":
			b.WriteByte('\n')
			return
		}
		for _, c := range v.children {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
