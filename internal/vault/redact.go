package vault

import (
	"encoding/base64"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Redaction markers of Section 20.1.
const (
	// RedactSecretPrefix wraps a redacted vault value:
	// ‹secret:github-token›.
	RedactSecretPrefix = "‹secret:"
	// RedactPatternPrefix wraps a redacted known format:
	// ‹secret:pattern›.
	RedactPatternMarker = "‹secret:pattern›"
)

// Redactor replaces secret values and known token formats in tool
// output before it reaches the durable journal, context, telemetry,
// ATIF or blobs (step 5 of Section 13.4). Matching is by exact value
// plus its base64 and URL encodings (Aho-Corasick automaton rebuilt on
// unlock and on every value change), and by pattern for known key
// formats even when absent from the vault.
type Redactor struct {
	mu          sync.RWMutex
	root        *acNode
	literalRoot *acNode
	// names maps a secret name to its marker, for stable output.
	names map[string]string
}

// acNode is one Aho-Corasick trie node.
type acNode struct {
	next   map[byte]*acNode
	fail   *acNode
	output []acOut
}

// acOut is one literal ending at a node with its replacement.
type acOut struct {
	length int
	marker string
}

// NewRedactor returns an empty redactor (redacts patterns only).
func NewRedactor() *Redactor {
	r := &Redactor{names: map[string]string{}}
	r.root = newACNode()
	r.build([]acPattern{})
	return r
}

// acPattern is one literal to redact with its replacement.
type acPattern struct {
	literal string
	marker  string
}

// BuildRedactor builds a redactor over live values. values[i] belongs
// to secret names[i]; the slices are not retained. Short values (<8
// bytes) are skipped for exact matching to avoid false positives but
// remain covered by pattern redaction.
func BuildRedactor(values [][]byte, names []string) *Redactor {
	r := buildRedactor(values, names, 8)
	r.literalRoot = buildRedactor(values, names, 1).root
	return r
}

// BuildLiteralRedactor retains even short known values for literal-only redaction.
func BuildLiteralRedactor(values [][]byte, names []string) *Redactor {
	return buildRedactor(values, names, 1)
}

func buildRedactor(values [][]byte, names []string, minLength int) *Redactor {
	r := &Redactor{names: map[string]string{}}
	var pats []acPattern
	for i, val := range values {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		if name == "" {
			continue
		}
		marker := RedactSecretPrefix + name + "›"
		r.names[name] = marker
		if len(val) < minLength {
			continue
		}
		seen := map[string]bool{}
		for _, variant := range valueVariants(string(val)) {
			if variant == "" || seen[variant] {
				continue
			}
			seen[variant] = true
			pats = append(pats, acPattern{literal: variant, marker: marker})
		}
	}
	r.root = newACNode()
	// Deterministic build: sort by literal, then marker, and keep the
	// first marker per literal (two secrets sharing a value redact to
	// the alphabetically first name).
	sort.Slice(pats, func(i, j int) bool {
		if pats[i].literal != pats[j].literal {
			return pats[i].literal < pats[j].literal
		}
		return pats[i].marker < pats[j].marker
	})
	deduped := pats[:0]
	for i, p := range pats {
		if i == 0 || p.literal != pats[i-1].literal {
			deduped = append(deduped, p)
		}
	}
	r.build(deduped)
	return r
}

// valueVariants returns a value plus its habitual encodings: base64
// (standard, raw, URL-safe) and URL query escaping.
func valueVariants(s string) []string {
	out := []string{s}
	out = append(out, base64.StdEncoding.EncodeToString([]byte(s)))
	out = append(out, base64.RawStdEncoding.EncodeToString([]byte(s)))
	out = append(out, base64.URLEncoding.EncodeToString([]byte(s)))
	out = append(out, base64.RawURLEncoding.EncodeToString([]byte(s)))
	out = append(out, url.QueryEscape(s))
	out = append(out, url.PathEscape(s))
	return out
}

func newACNode() *acNode { return &acNode{next: map[byte]*acNode{}} }

// build inserts literals and computes failure links (BFS).
func (r *Redactor) build(pats []acPattern) {
	for _, p := range pats {
		n := r.root
		for i := 0; i < len(p.literal); i++ {
			c := p.literal[i]
			nxt, ok := n.next[c]
			if !ok {
				nxt = newACNode()
				n.next[c] = nxt
			}
			n = nxt
		}
		n.output = append(n.output, acOut{length: len(p.literal), marker: p.marker})
	}
	queue := []*acNode{}
	for _, nxt := range r.root.next {
		nxt.fail = r.root
		queue = append(queue, nxt)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for c, nxt := range n.next {
			f := n.fail
			for f != nil && f.next[c] == nil {
				f = f.fail
			}
			if f == nil {
				nxt.fail = r.root
			} else {
				nxt.fail = f.next[c]
				nxt.output = append(nxt.output, nxt.fail.output...)
			}
			queue = append(queue, nxt)
		}
	}
}

// pattern regexes for known key/token formats (Section 20.1: redaction
// by pattern even when absent from the vault).
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{10,})`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\b(sk-ant-[A-Za-z0-9\-_]{10,}|sk-[A-Za-z0-9]{16,})`),
	regexp.MustCompile(`\bxai-[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z\-_]{20,}\b`),
	regexp.MustCompile(`(?i)\bxox[baprs]-[A-Za-z0-9\-]{8,}\b`),
	regexp.MustCompile(`(?i)\b(sk_live_[A-Za-z0-9]{10,}|rk_live_[A-Za-z0-9]{10,})\b`),
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----[\s\S]*?-----END (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(glpat-[A-Za-z0-9\-_]{10,}|ghp_[A-Za-z0-9]{10,})\b`),
}

// ContainsRedactionMarker reports whether s carries a redaction marker:
// a vault value or a known key format that was replaced for the model.
// The marker stands for a hidden value, so write paths refuse text that
// contains one instead of persisting the placeholder into a file.
func ContainsRedactionMarker(s string) bool {
	return strings.Contains(s, RedactSecretPrefix)
}

// MarkerFor reports the marker used for a secret name.
func (r *Redactor) MarkerFor(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.names[name]; ok {
		return m
	}
	return RedactSecretPrefix + name + "›"
}

type match struct {
	start, end int
	marker     string
}

// Redact replaces secret values (exact plus base64/URL variants) and
// known token patterns. Value matches win over pattern matches on
// overlap; output is deterministic.
func (r *Redactor) Redact(s string) string { return r.redact(s, true) }

// RedactLiterals replaces only known literal values and their encodings.
func (r *Redactor) RedactLiterals(s string) string { return r.redact(s, false) }

func (r *Redactor) redact(s string, patterns bool) string {
	r.mu.RLock()
	root := r.root
	if !patterns && r.literalRoot != nil {
		root = r.literalRoot
	}
	r.mu.RUnlock()

	var hits []match
	// Linear Aho-Corasick scan: at each end position keep the longest
	// literal ending there.
	n := root
	for i := 0; i < len(s); i++ {
		c := s[i]
		for n != root && n.next[c] == nil {
			n = n.fail
		}
		if nxt, ok := n.next[c]; ok {
			n = nxt
		} else {
			n = root
			continue
		}
		longest := -1
		for _, o := range n.output {
			if o.length > longest {
				longest = o.length
			}
		}
		if longest > 0 {
			// All outputs at this node share the end position; emit
			// the longest (same marker per literal set).
			marker := n.output[0].marker
			for _, o := range n.output {
				if o.length == longest {
					marker = o.marker
				}
			}
			hits = append(hits, match{start: i - longest + 1, end: i + 1, marker: marker})
		}
	}
	if patterns {
		for _, re := range secretPatterns {
			for _, loc := range re.FindAllStringIndex(s, -1) {
				hits = append(hits, match{start: loc[0], end: loc[1], marker: RedactPatternMarker})
			}
		}
	}
	if len(hits) == 0 {
		return s
	}
	// Prefer longer matches starting earlier (value beats pattern on
	// overlap); then sweep left to right.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].start != hits[j].start {
			return hits[i].start < hits[j].start
		}
		return (hits[i].end - hits[i].start) > (hits[j].end - hits[j].start)
	})
	var b strings.Builder
	b.Grow(len(s))
	pos := 0
	for _, h := range hits {
		if h.start < pos {
			continue
		}
		b.WriteString(s[pos:h.start])
		b.WriteString(h.marker)
		pos = h.end
	}
	b.WriteString(s[pos:])
	return b.String()
}

// Redact returns the vault redaction of s (nil-safe: patterns only
// when locked).
func (v *Vault) Redact(s string) string {
	v.mu.Lock()
	r := v.redactor
	v.mu.Unlock()
	if r == nil {
		r = NewRedactor()
	}
	return r.Redact(s)
}

// RedactLiterals uses the vault's live values without candidate-pattern matching.
func (v *Vault) RedactLiterals(s string) string {
	v.mu.Lock()
	r := v.redactor
	v.mu.Unlock()
	if r == nil {
		return s
	}
	return r.RedactLiterals(s)
}
