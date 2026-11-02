package memory

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// SourceRef is the shared event/history representation.
type SourceRef = api.SourceRef

var variantRef = regexp.MustCompile(`(?:\[\[?|【|［［?|\()\s*doc\s*:\s*([^\]】］)\n]+)(?:\]\]?|】|］］?|\))`)
var citationPunctuationSpace = regexp.MustCompile(`[ \t]+([.!?])`)
var chunkRange = regexp.MustCompile(`^([a-zA-Z_]*)([0-9]+)\s*[-–]\s*([a-zA-Z_]*)([0-9]+)$`)
var canonicalRef = regexp.MustCompile(`\[\[doc:([^\]#]*)(?:#([^\]]*))?\]\]`)

// Normalize canonicalizes bracket variants and expands chunk lists and numeric
// ranges. Range expansion is capped at 100 refs; opaque chunk IDs stay literal.
func Normalize(text string) string {
	return variantRef.ReplaceAllStringFunc(text, func(ref string) string {
		body := variantRef.FindStringSubmatch(ref)[1]
		pair := strings.SplitN(body, "#", 2)
		if len(pair) != 2 {
			return "[[doc:" + strings.TrimSpace(body) + "]]"
		}
		doc := strings.TrimSpace(pair[0])
		var out strings.Builder
		for _, item := range strings.FieldsFunc(pair[1], func(r rune) bool { return r == ',' || r == ';' || r == '、' }) {
			item = strings.TrimSpace(item)
			item = strings.TrimPrefix(item, "#")
			itemDoc := doc
			if parts := strings.SplitN(item, "#", 2); len(parts) == 2 {
				itemDoc = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[0]), "doc:"))
				item = strings.TrimSpace(parts[1])
			}
			if m := chunkRange.FindStringSubmatch(item); m != nil && (m[3] == "" || m[1] == m[3]) {
				lo, e1 := strconv.Atoi(m[2])
				hi, e2 := strconv.Atoi(m[4])
				if e1 == nil && e2 == nil && hi >= lo && hi-lo < 100 {
					for delta := 0; delta <= hi-lo; delta++ {
						out.WriteString("[[doc:" + itemDoc + "#" + m[1] + strconv.Itoa(lo+delta) + "]]")
					}
					continue
				}
			}
			out.WriteString("[[doc:" + itemDoc + "#" + strings.Join(strings.Fields(item), "") + "]]")
		}
		if out.Len() == 0 {
			return "[[doc:" + doc + "#]]"
		}
		return out.String()
	})
}

// StreamHold keeps an unfinished [[ marker private across delta boundaries.
// Each stream owns a zero-value buffer. More than 200 runes releases malformed
// text rather than retaining unbounded output. Flush releases the final tail.
type StreamHold struct{ pending string }

func (h *StreamHold) Write(delta string) string {
	text := h.pending + delta
	h.pending = ""
	start := 0
	for {
		i := strings.Index(text[start:], "[[")
		if i < 0 {
			break
		}
		i += start
		j := strings.Index(text[i+2:], "]]")
		if j < 0 {
			if utf8.RuneCountInString(text[i:]) <= 200 {
				h.pending = text[i:]
				return text[:i]
			}
			break
		}
		start = i + 2 + j + 2
	}
	if strings.HasSuffix(text, "[") {
		h.pending = "["
		return text[:len(text)-1]
	}
	return text
}
func (h *StreamHold) Flush() string { out := h.pending; h.pending = ""; return out }

// CitationLibrary validates retrieval provenance and supplies trusted metadata.
// Implementations must fail closed when validation cannot read its store.
type CitationLibrary interface {
	ValidateCitations(context.Context, []string, []string) ([]Citation, []string)
	CitationSource(context.Context, string, string) (SourceRef, error)
	AnswerSources(context.Context, []string) ([]SourceEvent, error)
}

// CiteFlow processes complete answers. It does not rewrite internal child
// reports: their exact refs must survive until the user's final answer.
type CiteFlow struct{ Library CitationLibrary }

func CitationsPipelineEnabled() bool { return ctxcompiler.CitationsPipelineEnabled() }

// Process returns rendered text, sources in first-appearance order and rejected
// refs. Every sentence whose refs all fail receives an unsourced marker.
func (f CiteFlow) Process(ctx context.Context, text string, tree []string) (string, []SourceRef, []string) {
	if !CitationsPipelineEnabled() {
		return text, nil, nil
	}
	text = Normalize(text)
	refs := canonicalRef.FindAllString(text, -1)
	valid := map[string]Citation{}
	if f.Library != nil {
		citations, _ := f.Library.ValidateCitations(ctx, refs, tree)
		for _, c := range citations {
			valid[c.DocID+"#"+c.ChunkID] = c
		}
	}
	var sources []SourceRef
	var dropped []string
	numbers := map[string]int{}
	// Keep citation groups after sentence punctuation with the preceding claim.
	var out strings.Builder
	for _, sentence := range citationSentences(text) {
		accepted, rejected := 0, 0
		rendered := canonicalRef.ReplaceAllStringFunc(sentence, func(ref string) string {
			m := canonicalRef.FindStringSubmatch(ref)
			key := m[1] + "#" + m[2]
			c, ok := valid[key]
			if !ok {
				dropped = append(dropped, ref)
				rejected++
				return ""
			}
			n := numbers[key]
			if n == 0 {
				source, err := f.Library.CitationSource(ctx, c.DocID, c.ChunkID)
				if err != nil {
					dropped = append(dropped, ref)
					rejected++
					return ""
				}
				n = len(sources) + 1
				numbers[key] = n
				source.N, source.Kind = n, "document"
				source.DocumentID, source.ChunkID = c.DocID, c.ChunkID
				source.Excerpt = truncateExcerpt(source.Excerpt)
				sources = append(sources, source)
			}
			accepted++
			return "[" + strconv.Itoa(n) + "]"
		})
		if rejected > 0 {
			rendered = citationPunctuationSpace.ReplaceAllString(rendered, "$1")
		}
		if rejected > 0 && accepted == 0 {
			tail := sentence[len(strings.TrimRight(sentence, " \t\r\n")):]
			rendered = strings.TrimRight(rendered, " \t\r\n") + " [unsourced]" + tail
		}
		out.WriteString(rendered)
	}
	if f.Library != nil {
		web, _ := f.Library.AnswerSources(ctx, tree)
		seen := map[string]bool{}
		for _, ev := range web {
			if ev.URL == "" || seen[ev.URL] {
				continue
			}
			seen[ev.URL] = true
			sources = append(sources, SourceRef{N: len(sources) + 1, Kind: "web", URL: ev.URL, Title: ev.Title})
		}
	}
	return out.String(), sources, dropped
}

func truncateExcerpt(s string) string {
	r := []rune(s)
	if len(r) > 300 {
		return string(r[:300])
	}
	return s
}

func citationSentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		if strings.HasPrefix(text[i:], "[[doc:") {
			if j := strings.Index(text[i:], "]]"); j >= 0 {
				i += j + 1
				continue
			}
		}
		if text[i] == '\n' {
			out = append(out, text[start:i+1])
			start = i + 1
			continue
		}
		if strings.ContainsRune(".!?", rune(text[i])) {
			end := i + 1
			// References after a period still belong to that sentence.
			for {
				pos := end
				for pos < len(text) && (text[pos] == ' ' || text[pos] == '\t') {
					pos++
				}
				if loc := canonicalRef.FindStringIndex(prefixCitation(text[pos:])); loc != nil && loc[0] == 0 {
					end = pos + loc[1]
					continue
				}
				break
			}
			if end == len(text) || text[end] == ' ' || text[end] == '\n' || text[end] == '\t' {
				out = append(out, text[start:end])
				start = end
				i = end - 1
			}
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

// CitationSource reads trusted chunk text and document metadata after validation.
// Positions come from the indexed chunk, never from model-authored text.
func (s *Service) CitationSource(ctx context.Context, docID, chunkID string) (SourceRef, error) {
	var out SourceRef
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		d, err := getLibraryDocTx(ctx, q, docID)
		if err != nil {
			return err
		}
		c, err := getChunkTx(ctx, q, docID, chunkID)
		if err != nil {
			return err
		}
		var positions string
		if err := q.QueryRow(ctx, "SELECT positions_json FROM doc_chunks WHERE doc_id=? AND id=?", docID, chunkID).Scan(&positions); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(positions), &out.Boxes); err != nil {
			return err
		}
		cit := RenderCitation(d, c)
		out = SourceRef{Boxes: out.Boxes, PageLabel: c.PageLabel, DocumentID: d.ID, ChunkID: c.ID, Title: d.Title, Page: c.Page, Paragraph: c.Paragraph, SectionPath: c.Section, Excerpt: truncateExcerpt(c.Text), MIME: d.MIME, BlobDigest: d.Digest, Verbatim: cit.Verbatim}
		return nil
	})
	return out, err
}

// Avoid scanning the remaining answer for every punctuation mark.
func prefixCitation(s string) string {
	if !strings.HasPrefix(s, "[[doc:") {
		return ""
	}
	if end := strings.Index(s, "]]"); end >= 0 {
		return s[:end+2]
	}
	return ""
}
