// Package text extracts bounded plain text, Markdown, HTML, CSV, JSON and
// image metadata. It never executes active content or accesses the network.
package text

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

const MaxSize int64 = 64 << 20

var ErrTooLarge = errors.New("text exceeds extraction limit")

// Extractor reads the explicitly selected Format; the default is txt.
type Extractor struct{ Format string }

func New(format string) *Extractor     { return &Extractor{Format: strings.ToLower(format)} }
func (e *Extractor) Formats() []string { return []string{canonical(e.Format)} }
func canonical(format string) string {
	switch format {
	case "", "log":
		return "txt"
	case "markdown":
		return "md"
	case "htm":
		return "html"
	case "jpg":
		return "jpeg"
	}
	return format
}
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_TEXT_READ")))
	return v != "0" && v != "false" && v != "off"
}
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts docextract.Options) (docextract.Document, error) {
	if err := ctx.Err(); err != nil {
		return docextract.Document{}, err
	}
	if !Enabled() {
		return docextract.Document{}, errors.New("native text extraction disabled")
	}
	if size < 0 || size > MaxSize {
		return docextract.Document{}, ErrTooLarge
	}
	format := canonical(e.Format)
	doc := docextract.Document{Format: format, PageCount: 1}
	p := docextract.Page{Number: 1, Method: docextract.MethodNative, Quality: 1}
	input := io.NewSectionReader(r, 0, size)
	if format == "png" || format == "jpeg" {
		config, actual, err := image.DecodeConfig(input)
		if err != nil {
			return doc, err
		}
		if actual != format {
			return doc, fmt.Errorf("image format %s does not match %s", actual, format)
		}
		p.Width = float64(config.Width)
		p.Height = float64(config.Height)
		p.Method = docextract.MethodNone
		p.Quality = 0
	} else {
		data, err := io.ReadAll(&contextReader{ctx: ctx, r: input})
		if err != nil {
			return doc, err
		}
		s := strings.ToValidUTF8(string(data), "\uFFFD")
		add := func(kind, value string, level int) {
			if strings.TrimSpace(value) == "" {
				return
			}
			p.Blocks = append(p.Blocks, docextract.Block{Kind: kind, Text: value, Level: level, Anchor: fmt.Sprintf("p%d", len(p.Blocks)+1)})
		}
		switch format {
		case "txt", "md":
			for lineIndex, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
				if lineIndex >= 100000 {
					return doc, ErrTooLarge
				}
				if err := ctx.Err(); err != nil {
					return doc, err
				}
				t := strings.TrimSpace(line)
				level := 0
				if format == "md" {
					for level < len(t) && level < 6 && t[level] == '#' {
						level++
					}
					if level == len(t) || (level > 0 && t[level] != ' ' && t[level] != '\t') {
						level = 0
					}
				}
				if level > 0 {
					add(docextract.KindHeading, strings.TrimSpace(strings.TrimRight(t[level:], "#")), level)
				} else {
					add(docextract.KindParagraph, line, 0)
				}
			}
		case "html":
			p.Blocks = HTMLBlocks(s)
			if len(p.Blocks) >= 100000 {
				p.Warnings = append(p.Warnings, "block limit reached (100000)")
			}
		case "json":
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, []byte(s), "", "  "); err != nil {
				return doc, err
			}
			add(docextract.KindParagraph, pretty.String(), 0)
		case "csv", "tsv":
			delimiter := byte(',')
			if format == "tsv" {
				delimiter = '\t'
			}
			bounded, clipped := boundedDelimited(s, delimiter)
			if clipped {
				p.Warnings = append(p.Warnings, "cell limit reached (20000)")
			}
			reader := csv.NewReader(strings.NewReader(bounded))
			reader.FieldsPerRecord = -1
			reader.Comma = rune(delimiter)
			var rows [][]string
			count := 0
			for rownum := 1; ; rownum++ {
				if err := ctx.Err(); err != nil {
					return doc, err
				}
				row, err := reader.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					return doc, err
				}
				if rownum <= 200 {
					rows = append(rows, row)
				}
				for col, v := range row {
					if count >= 20000 {
						break
					}
					p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindCell, Text: v, Anchor: "Sheet1!" + column(col+1) + strconv.Itoa(rownum)})
					count++
				}
				if count >= 20000 {
					p.Warnings = append(p.Warnings, "cell limit reached (20000)")
					break
				}
			}
			p.Blocks = append(p.Blocks, docextract.Block{Kind: docextract.KindTable, Text: table(rows), Anchor: "Sheet1!table"})
		default:
			return doc, fmt.Errorf("unsupported text format %q", format)
		}
	}
	for _, b := range p.Blocks {
		if b.Kind == docextract.KindHeading {
			doc.Outline = append(doc.Outline, docextract.OutlineEntry{Title: b.Text, Level: b.Level})
		}
	}
	if len(opts.Pages) == 0 {
		doc.Pages = []docextract.Page{p}
	} else {
		for _, v := range opts.Pages {
			if v == 1 {
				doc.Pages = []docextract.Page{p}
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return doc, err
	}
	if opts.Progress != nil {
		opts.Progress(1, 1)
	}
	return doc, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func column(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}
func table(rows [][]string) string {
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols > 30 {
		cols = 30
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteByte('|')
		for j := 0; j < cols; j++ {
			s := ""
			if j < len(r) {
				s = r[j]
			}
			s = strings.NewReplacer("|", "\\|", "\n", "<br>", "\r", "").Replace(s)
			b.WriteString(" " + s + " |")
		}
		b.WriteByte('\n')
		if i == 0 {
			b.WriteByte('|')
			for j := 0; j < cols; j++ {
				b.WriteString(" --- |")
			}
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// HTMLBlocks strips tags, skips script/style content and retains heading levels.
// HTML is treated as text: links and embedded resources are never followed.
func HTMLBlocks(s string) []docextract.Block {
	var out []docextract.Block
	var b strings.Builder
	level := 0
	skip := ""
	flush := func() {
		if len(out) >= 100000 {
			b.Reset()
			return
		}
		value := strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
		b.Reset()
		if value == "" {
			return
		}
		kind := docextract.KindParagraph
		if level > 0 {
			kind = docextract.KindHeading
		}
		out = append(out, docextract.Block{Kind: kind, Level: level, Text: value, Anchor: fmt.Sprintf("p%d", len(out)+1)})
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			if skip == "" {
				b.WriteString(s[i : i+j])
			}
			i += j
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				break
			}
			i += end + 7
			continue
		}
		j := i + 1
		var quote byte
		for ; j < len(s); j++ {
			c := s[j]
			if quote != 0 {
				if c == quote {
					quote = 0
				}
			} else if c == '\'' || c == '"' {
				quote = c
			} else if c == '>' {
				break
			}
		}
		if j == len(s) {
			if skip == "" {
				b.WriteString(s[i:])
			}
			break
		}
		tag := strings.ToLower(strings.TrimSpace(s[i+1 : j]))
		i = j + 1
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimLeft(tag, "/")
		fields := strings.Fields(tag)
		if len(fields) == 0 {
			continue
		}
		tag = strings.TrimSuffix(fields[0], "/")
		if skip != "" {
			if closing && tag == skip {
				skip = ""
			}
			continue
		}
		if tag == "script" || tag == "style" || tag == "head" {
			flush()
			skip = tag
			continue
		}
		h := len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6'
		if h {
			flush()
			if closing {
				level = 0
			} else {
				level = int(tag[1] - '0')
			}
			continue
		}
		switch tag {
		case "p", "div", "section", "article", "li", "tr", "br", "hr", "body":
			flush()
		case "td", "th":
			b.WriteByte(' ')
		}
	}
	flush()
	return out
}

// Bound a single record before encoding/csv allocates its field slice. The
// extraction loop independently caps cells across all records.
func boundedDelimited(s string, delimiter byte) (string, bool) {
	quoted := false
	fields := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			if quoted && i+1 < len(s) && s[i+1] == '"' {
				i++
				continue
			}
			quoted = !quoted
		case delimiter:
			if !quoted {
				fields++
				if fields > 20000 {
					return s[:i], true
				}
			}
		case '\n', '\r':
			if !quoted {
				fields = 1
			}
		}
	}
	return s, false
}
