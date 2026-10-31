package memory

import (
	"context"
	"regexp"
	"strings"
)

// HeuristicExtractor is the deterministic batch extractor used when
// no model role is wired: it keeps explicit memory speech
// ("remember that…", "my X is Y", "I prefer…") as candidates with
// msg_id sources (Section 10.5 step 3). The production path plugs the
// cheapest model role via WithExtractor; both feed the same
// admission, which is where quality is enforced (Xiong et al.).
var explicitMemoryRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bremember\s+(?:that\s+)?(.{8,300})`),
	regexp.MustCompile(`(?i)\bmy\s+([a-z][a-z ]{2,60}?)\s+is\s+(.{2,120})`),
	regexp.MustCompile(`(?i)\bi\s+prefer\s+(.{2,120})`),
	regexp.MustCompile(`(?i)\bdon't\s+(?:let\s+me\s+)?forget\s+(?:that\s+)?(.{8,200})`),
	regexp.MustCompile(`(?i)\brecuerda\s+(?:que\s+)?(.{8,300})`),
	regexp.MustCompile(`(?i)\bmi\s+([a-záéíóúñ][a-záéíóúñ ]{2,60}?)\s+es\s+(.{2,120})`),
	regexp.MustCompile(`(?i)\bprefiero\s+(.{2,120})`),
}

// HeuristicExtractor extracts explicit memory speech without a model.
type HeuristicExtractor struct{}

// Extract implements Extractor.
func (HeuristicExtractor) Extract(_ context.Context, _ string, turns []Turn) ([]Candidate, error) {
	var out []Candidate
	for _, t := range turns {
		for _, sent := range splitSentences(t.Text) {
			sent = strings.TrimSpace(sent)
			if sent == "" {
				continue
			}
			for i, re := range explicitMemoryRes {
				m := re.FindStringSubmatch(sent)
				if m == nil {
					continue
				}
				kind := KindFact
				if i == 2 || i == 6 {
					kind = KindPreference
				}
				stmt := m[len(m)-1]
				if i == 1 || i == 5 {
					stmt = "my " + m[1] + " is " + m[2]
				}
				out = append(out, Candidate{
					Kind:         kind,
					Statement:    strings.TrimSpace(stmt),
					Confidence:   0.7,
					Importance:   0.6,
					Sources:      []Source{{Kind: "message", Ref: t.MsgID, Conversation: t.Conversation}},
					Trust:        TrustUser,
					Conversation: t.Conversation,
				})
				break
			}
		}
	}
	return out, nil
}

var sentenceRe = regexp.MustCompile(`[^.!?\n]+[.!?]?`)

// splitSentences cuts a turn into candidate sentences.
func splitSentences(text string) []string {
	var out []string
	for _, m := range sentenceRe.FindAllString(text, -1) {
		if s := strings.TrimSpace(m); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Rewriter rephrases a query once when retrieval finds nothing; the
// production path plugs the cheapest model role (Section 10.7: no
// multi-query expansion by default, one rewrite only on zero
// results, never with a small model inventing intervals).
type Rewriter interface {
	// Rewrite returns one rephrasing of query.
	Rewrite(ctx context.Context, query string) (string, error)
}

// WithRewriter plugs the single-shot no-result rewriter.
func WithRewriter(r Rewriter) Option {
	return func(svc *Service) { svc.rewriter = r }
}
