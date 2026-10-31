package memory

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Retrieval thresholds and budgets.
const (
	// defaultRetrieveK caps packaged items when the caller sets no K.
	defaultRetrieveK = 20
	// defaultMaxTokens caps the packaged context.
	defaultMaxTokens = 2000
	// rerankCloseGap triggers the optional reranker only when the
	// top three sit within 15% of each other (Section 10.7).
	rerankCloseGap = 0.15
	// rerankTopN caps the reranked head.
	rerankTopN = 20
)

// halfLives in days per kind (Appendix B, GBrain starting values).
var halfLives = map[Kind]float64{
	KindEvent:      7,
	KindPreference: 90,
	KindCommitment: 90,
	KindFact:       365,
	KindBelief:     365,
	KindRelation:   365,
	KindProcedure:  365,
}

// Retrieve runs the Section 10.7 pipeline: deterministic query
// analysis, four parallel candidate lists, RRF fusion (k=60),
// deterministic adjustments, optional rerank, sufficiency notices
// and budgeted packaging. Every filter lands in SQL; scopes are
// mandatory. When nothing matches and a rewriter is plugged, the
// query is rephrased once and the pipeline runs again; multi-query
// expansion stays off by default.
func (s *Service) Retrieve(ctx context.Context, q Query) (Retrieval, error) {
	ret, err := s.retrieveOnce(ctx, q)
	if err != nil {
		return Retrieval{}, err
	}
	if len(ret.Items) == 0 && s.rewriter != nil && strings.TrimSpace(q.Text) != "" {
		rephrased, rerr := s.rewriter.Rewrite(ctx, q.Text)
		if rerr == nil && strings.TrimSpace(rephrased) != "" && !strings.EqualFold(strings.TrimSpace(rephrased), strings.TrimSpace(q.Text)) {
			q2 := q
			q2.Text = rephrased
			ret2, err2 := s.retrieveOnce(ctx, q2)
			if err2 == nil && len(ret2.Items) > 0 {
				ret2.Notices = append(ret2.Notices, Notice{Code: "rewritten", Detail: "no result for the original phrasing; showing results for a rephrasing"})
				return ret2, nil
			}
		}
	}
	return ret, nil
}

func (s *Service) retrieveOnce(ctx context.Context, q Query) (Retrieval, error) {
	scopes, err := s.resolveScopes(q.Scopes)
	if err != nil {
		return Retrieval{}, err
	}
	analysis := analyzeQuery(ctx, s, q.Text, scopes)
	if q.Historical || analysis.historical {
		q.Historical = true
	}
	k := q.K
	if k <= 0 {
		k = defaultRetrieveK
	}
	perList := s.settings.CandidatesPerList
	if perList <= 0 {
		perList = 50
	}

	// Candidate lists run sequentially against the read pool; each
	// is short and releases its connection before the next starts
	// (Section 4.3: short read transactions).
	lexical, err := s.lexicalList(ctx, scopes, q, analysis, perList)
	if err != nil {
		return Retrieval{}, err
	}
	semantic, semanticOK := s.semanticList(ctx, scopes, q, perList)
	relational, err := s.relationalList(ctx, scopes, q, analysis, perList)
	if err != nil {
		return Retrieval{}, err
	}
	temporal := s.temporalList(ctx, scopes, q, analysis, perList)

	fused := fuseRRF(s.settings.RRFFusedK, lexical, semantic, relational, temporal)
	items := s.adjustAndPackage(ctx, scopes, q, fused, analysis)

	if s.rerank != nil && len(items) >= 3 && closeTop(items) {
		head := items
		if len(head) > rerankTopN {
			head = head[:rerankTopN]
		}
		reranked, err := s.rerank.Rerank(ctx, q.Text, head)
		if err == nil && len(reranked) == len(head) {
			items = append(reranked, items[len(head):]...)
		}
	}

	if k < len(items) {
		items = items[:k]
	}
	tokens := q.MaxTokens
	if tokens <= 0 {
		tokens = defaultMaxTokens
	}
	items = packBudget(items, tokens)
	notices := sufficiency(items, q, analysis)

	if len(items) > 0 {
		s.touchUsage(ctx, items)
	}
	return Retrieval{Items: items, Notices: notices, Semantic: semanticOK}, nil
}

// ---- query analysis ----

type queryAnalysis struct {
	required   []string
	from       time.Time
	to         time.Time
	hasTime    bool
	seeds      []string
	historical bool
	ambiguous  []string
}

// analyzeQuery is fully deterministic (Section 10.7 step 1): quoted
// phrases become mandatory lexical matches, dates and intervals
// become validity filters in the user zone, known entity aliases
// become relational seeds, and past-tense cues widen to non-active
// rows. Spanish and English cues both apply.
func analyzeQuery(ctx context.Context, s *Service, text string, scopes []string) queryAnalysis {
	var a queryAnalysis
	a.required = quotedPhrases(text)
	a.from, a.to, a.hasTime = parseTimeFilter(text, s.location(), s.now())
	a.historical = hasHistoricalCue(text)
	a.seeds, a.ambiguous = entitySeeds(ctx, s, text, scopes)
	return a
}

var quoteRe = regexp.MustCompile(`"([^"]+)"`)

func quotedPhrases(text string) []string {
	var out []string
	for _, m := range quoteRe.FindAllStringSubmatch(text, -1) {
		if p := strings.TrimSpace(m[1]); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var historicalCues = []string{
	"used to", "before", "previously", "history", "historical", "what did",
	"what was", "former", "old ", "back then", "pensaba", "historial",
	"historico", "histórico", "antes", "antiguo", "solía",
}

func hasHistoricalCue(text string) bool {
	l := " " + strings.ToLower(text) + " "
	for _, c := range historicalCues {
		if strings.Contains(l, c) {
			return true
		}
	}
	return false
}

// entitySeeds matches known aliases as substrings and reports names
// claimed by entities in more than one queried scope.
func entitySeeds(ctx context.Context, s *Service, text string, scopes []string) ([]string, []string) {
	var all []Entity
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := scopeArgs(scopes)
		rows, err := q.Query(ctx, `SELECT id, scope_id, type, canonical, aliases_json FROM entities WHERE scope_id IN (`+placeholders(len(scopes))+`)`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEntity(rows)
			if err != nil {
				return err
			}
			all = append(all, e)
		}
		return rows.Err()
	})
	l := strings.ToLower(text)
	byName := map[string][]string{}
	var seeds []string
	seen := map[string]bool{}
	for _, e := range all {
		names := append([]string{e.Canonical}, e.Aliases...)
		for _, n := range names {
			nl := strings.ToLower(strings.TrimSpace(n))
			if nl == "" {
				continue
			}
			if strings.Contains(l, nl) {
				byName[nl] = append(byName[nl], e.Scope)
				if !seen[e.ID] {
					seen[e.ID] = true
					seeds = append(seeds, e.ID)
				}
			}
		}
	}
	var ambiguous []string
	for name, sc := range byName {
		uniq := map[string]bool{}
		for _, x := range sc {
			uniq[x] = true
		}
		if len(uniq) > 1 {
			ambiguous = append(ambiguous, name)
		}
	}
	sort.Strings(ambiguous)
	return seeds, ambiguous
}

// ---- temporal parsing (Spanish + English) ----

var monthNames = map[string]time.Month{
	"january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6,
	"july": 7, "august": 8, "september": 9, "october": 10, "november": 11, "december": 12,
	"enero": 1, "febrero": 2, "marzo": 3, "abril": 4, "mayo": 5, "junio": 6,
	"julio": 7, "agosto": 8, "septiembre": 9, "setiembre": 9, "octubre": 10, "noviembre": 11, "diciembre": 12,
}

var (
	dateRe    = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	inMonthRe = regexp.MustCompile(`(?i)\b(?:in|en)\s+([a-záéíóú]+)(?:\s+(?:de\s+)?(\d{4}))?`)
	agoRe     = regexp.MustCompile(`(?i)\b(?:hace\s+(\d+)\s+(días?|dias?|semanas?|meses?|años?|anos?)|(\d+)\s+(days?|weeks?|months?|years?)\s+ago)\b`)
	relRe     = regexp.MustCompile(`(?i)\b(today|yesterday|hoy|ayer|last week|this week|la semana pasada|esta semana|last month|el mes pasado|este mes|last year|el a[ñn]o pasado|este a[ñn]o)\b`)
)

// parseTimeFilter turns relative and absolute date language into a
// validity window in the user zone. It returns hasTime=false when the
// query carries no temporal cue.
func parseTimeFilter(text string, loc *time.Location, now time.Time) (time.Time, time.Time, bool) {
	now = now.In(loc)
	l := strings.ToLower(text)
	if m := dateRe.FindStringSubmatch(l); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		day := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc)
		return day, day.AddDate(0, 0, 1), true
	}
	if m := inMonthRe.FindStringSubmatch(l); m != nil {
		if mo, ok := monthNames[strings.ToLower(m[1])]; ok {
			y := now.Year()
			if m[2] != "" {
				y, _ = strconv.Atoi(m[2])
			}
			start := time.Date(y, mo, 1, 0, 0, 0, 0, loc)
			return start, start.AddDate(0, 1, 0), true
		}
	}
	if m := agoRe.FindStringSubmatch(l); m != nil {
		var n int
		var unit string
		if m[1] != "" {
			n, _ = strconv.Atoi(m[1])
			unit = strings.ToLower(m[2])
		} else {
			n, _ = strconv.Atoi(m[3])
			unit = strings.ToLower(m[4])
		}
		var days int
		switch {
		case strings.HasPrefix(unit, "días") || strings.HasPrefix(unit, "dias") || strings.HasPrefix(unit, "day"):
			days = n
		case strings.HasPrefix(unit, "seman") || strings.HasPrefix(unit, "week"):
			days = 7 * n
		case strings.HasPrefix(unit, "mes") || strings.HasPrefix(unit, "month"):
			days = 30 * n
		case strings.HasPrefix(unit, "año") || strings.HasPrefix(unit, "ano") || strings.HasPrefix(unit, "year"):
			days = 365 * n
		}
		if days > 0 {
			end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
			return end.AddDate(0, 0, -days), end, true
		}
	}
	if m := relRe.FindStringSubmatch(l); m != nil {
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		switch strings.ToLower(m[1]) {
		case "today", "hoy":
			return day, day.AddDate(0, 0, 1), true
		case "yesterday", "ayer":
			return day.AddDate(0, 0, -1), day, true
		case "last week", "la semana pasada":
			wd := int(day.Weekday())
			sinceSunday := wd
			thisSunday := day.AddDate(0, 0, -sinceSunday)
			return thisSunday.AddDate(0, 0, -7), thisSunday, true
		case "this week", "esta semana":
			wd := int(day.Weekday())
			return day.AddDate(0, 0, -wd), day.AddDate(0, 0, 7-wd), true
		case "last month", "el mes pasado":
			first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
			return first.AddDate(0, -1, 0), first, true
		case "este mes":
			first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
			return first, first.AddDate(0, 1, 0), true
		case "last year", "el año pasado", "el ano pasado":
			return time.Date(now.Year()-1, 1, 1, 0, 0, 0, 0, loc), time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc), true
		case "este año", "este ano":
			return time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc), time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, loc), true
		}
	}
	return time.Time{}, time.Time{}, false
}

// ---- candidate lists ----

// ftsEscape quotes one literal for an FTS5 MATCH expression.
func ftsEscape(term string) string {
	return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
}

// ftsOr builds a recall-oriented OR query from free words plus
// quoted phrases; required phrases filter afterwards in Go so one
// slow phrase never kills the list.
func ftsOr(text string, required []string) string {
	words := wordRe.FindAllString(strings.ToLower(text), -1)
	seen := map[string]bool{}
	var parts []string
	for _, w := range words {
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		parts = append(parts, ftsEscape(w)+`*`)
	}
	for _, r := range required {
		parts = append(parts, ftsEscape(r))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " OR ")
}

func (s *Service) lexicalList(ctx context.Context, scopes []string, q Query, a queryAnalysis, limit int) ([]string, error) {
	match := ftsOr(q.Text, a.required)
	if match == "" {
		return nil, nil
	}
	statuses := listStatuses(q)
	var ids []string
	err := s.read(ctx, func(ctx context.Context, rd store.Queryer) error {
		args := []any{match}
		args = append(args, scopeArgs(scopes)...)
		query := `SELECT m.id FROM memories_fts f JOIN memories m ON m.id = f.id
			WHERE memories_fts MATCH ? AND m.scope_id IN (` + placeholders(len(scopes)) + `)`
		if len(statuses) > 0 {
			query += ` AND m.status IN (` + placeholders(len(statuses)) + `)`
			for _, st := range statuses {
				args = append(args, string(st))
			}
		}
		if len(q.Kinds) > 0 {
			query += ` AND m.kind IN (` + placeholders(len(q.Kinds)) + `)`
			for _, k := range q.Kinds {
				args = append(args, string(k))
			}
		}
		query += ` ORDER BY bm25(memories_fts) LIMIT ?`
		args = append(args, limit)
		rows, err := rd.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return filterRequired(ctx, s, ids, a.required), nil
}

// filterRequired enforces quoted-phrase matches in Go.
func filterRequired(ctx context.Context, s *Service, ids []string, required []string) []string {
	if len(required) == 0 || len(ids) == 0 {
		return ids
	}
	stmts := map[string]string{}
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for _, chunk := range chunkStrings(ids, 200) {
			args := make([]any, 0, len(chunk))
			for _, id := range chunk {
				args = append(args, id)
			}
			rows, err := q.Query(ctx, `SELECT id, statement FROM memories WHERE id IN (`+placeholders(len(chunk))+`)`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, st string
				if err := rows.Scan(&id, &st); err != nil {
					rows.Close()
					return err
				}
				stmts[id] = strings.ToLower(st)
			}
			rows.Close()
		}
		return nil
	})
	var out []string
	for _, id := range ids {
		st := stmts[id]
		ok := true
		for _, r := range required {
			if !strings.Contains(st, strings.ToLower(r)) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, id)
		}
	}
	return out
}

// semanticList embeds the query once per configured space and
// returns ranked neighbors. It reports false when no embedder is
// available or every embedding fails: the caller still fuses the
// lexical, temporal and relational lists and says so (Section 10.8).
func (s *Service) semanticList(ctx context.Context, scopes []string, q Query, limit int) ([]string, bool) {
	cur, prev, prevSpace := s.embedSnapshot()
	if cur == nil {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	push := func(space string, query []float32, ids []string) {
		above := aboveFloor(ctx, s, "memory", query, space, ids)
		for _, id := range above {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	ok := false
	if vecs, err := cur.Embed(ctx, []string{q.Text}); err == nil && len(vecs) > 0 {
		query := fitDims(vecs[0], s.settings.EmbeddingDimensions)
		space := spaceID(cur.Model(), s.settings.EmbeddingDimensions)
		if ids := s.neighborsOrFallback(ctx, "memory", query, scopes, space, limit); len(ids) > 0 {
			push(space, query, ids)
			ok = true
		}
	}
	// Dual read while a model change reindexes: the retiring space
	// still answers for rows the new model has not covered
	// (Section 10.8).
	if prev != nil && prevSpace != "" {
		if vecs, err := prev.Embed(ctx, []string{q.Text}); err == nil && len(vecs) > 0 {
			query := fitDims(vecs[0], s.settings.EmbeddingDimensions)
			if ids := s.neighborsOrFallback(ctx, "memory", query, scopes, prevSpace, limit); len(ids) > 0 {
				push(prevSpace, query, ids)
				ok = true
			}
		}
	}
	if !ok || len(out) == 0 {
		return nil, false
	}
	return filterStatus(ctx, s, out, listStatuses(q)), true
}

// semanticFloor is the minimum cosine for a semantic neighbor to
// count: below it the list reports nothing, so sufficiency and the
// single rewrite trigger honestly instead of dressing noise as
// recall (Section 10.7 step 6).
const semanticFloor = 0.2

// aboveFloor keeps neighbor ids whose exact shadow cosine clears
// the floor. Rows without a readable vector cannot be verified and
// do not count as semantic evidence.
func aboveFloor(ctx context.Context, s *Service, kind string, query []float32, space string, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	var shadows map[string][]float32
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		shadows = readShadowSet(ctx, q, kind, ids, space)
		return nil
	})
	var out []string
	for _, id := range ids {
		if vec, ok := shadows[id]; ok && Cosine(query, vec) >= semanticFloor {
			out = append(out, id)
		}
	}
	return out
}

// neighborsOrFallback prefers vec0 KNN and falls back to exact
// shadow search when the module is absent (Section 10.8).
func (s *Service) neighborsOrFallback(ctx context.Context, kind string, query []float32, scopes []string, space string, limit int) []string {
	ids, err := s.vectorNeighbors(ctx, kind, query, scopes, space, limit)
	if err == nil {
		return ids
	}
	var fallback []string
	_ = s.read(ctx, func(ctx context.Context, rd store.Queryer) error {
		var err error
		fallback, _, err = shadowTopK(ctx, rd, kind, query, scopes, space, limit)
		return err
	})
	return fallback
}

// vectorNeighbors prefers vec0 KNN and falls back to exact shadow
// search when the module is absent (Section 10.8 degradation).
func (s *Service) vectorNeighbors(ctx context.Context, kind string, query []float32, scopes []string, space string, limit int) ([]string, error) {
	available := false
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		available, err = store.VecAvailable(ctx, q)
		return err
	})
	if !available {
		var ids []string
		err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
			var err error
			ids, _, err = shadowTopK(ctx, q, kind, query, scopes, space, limit)
			return err
		})
		return ids, err
	}
	ids, _, err := s.db.KNN(ctx, kind, query, limit, scopes)
	if err != nil {
		var ids []string
		ferr := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
			var err error
			ids, _, err = shadowTopK(ctx, q, kind, query, scopes, space, limit)
			return err
		})
		if ferr != nil {
			return nil, err
		}
		return ids, nil
	}
	return ids, nil
}

func filterStatus(ctx context.Context, s *Service, ids []string, statuses []Status) []string {
	if len(ids) == 0 || len(statuses) == 0 {
		return ids
	}
	allowed := map[string]bool{}
	for _, st := range statuses {
		allowed[string(st)] = true
	}
	keep := map[string]bool{}
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for _, chunk := range chunkStrings(ids, 200) {
			args := make([]any, 0, len(chunk))
			for _, id := range chunk {
				args = append(args, id)
			}
			rows, err := q.Query(ctx, `SELECT id, status FROM memories WHERE id IN (`+placeholders(len(chunk))+`)`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id, st string
				if err := rows.Scan(&id, &st); err != nil {
					rows.Close()
					return err
				}
				if allowed[st] {
					keep[id] = true
				}
			}
			rows.Close()
		}
		return nil
	})
	var out []string
	for _, id := range ids {
		if keep[id] {
			out = append(out, id)
		}
	}
	return out
}

// relationalList walks 1-2 hops from entity seeds and adds memories
// linked to those entities directly (Section 10.7).
func (s *Service) relationalList(ctx context.Context, scopes []string, q Query, a queryAnalysis, limit int) ([]string, error) {
	seeds := append([]string(nil), a.seeds...)
	seeds = append(seeds, q.Entities...)
	if len(seeds) == 0 {
		return nil, nil
	}
	var ids []string
	err := s.read(ctx, func(ctx context.Context, rd store.Queryer) error {
		hops, err := neighborIDsTx(ctx, rd, scopes, seeds, 2)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range hops {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		for _, seed := range seeds {
			rows, err := rd.Query(ctx, `SELECT id FROM memories WHERE scope_id IN (`+placeholders(len(scopes))+`) AND entities_json LIKE ? LIMIT ?`,
				append(scopeArgs(scopes), `%`+seed+`%`, limit)...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return filterStatus(ctx, s, ids, listStatuses(q)), nil
}

// temporalList returns memories whose validity window overlaps the
// parsed interval, or recorded inside it when no validity is known.
func (s *Service) temporalList(ctx context.Context, scopes []string, q Query, a queryAnalysis, limit int) []string {
	from, to := a.from, a.to
	if !a.hasTime {
		if !q.From.IsZero() || !q.To.IsZero() {
			from, to = q.From, q.To
		} else {
			return nil
		}
	}
	fromMS, toMS := ms(from.UTC()), ms(to.UTC())
	var ids []string
	_ = s.read(ctx, func(ctx context.Context, rd store.Queryer) error {
		args := scopeArgs(scopes)
		query := `SELECT id FROM memories WHERE scope_id IN (` + placeholders(len(scopes)) + `)
			AND ((valid_from_ms != 0 AND valid_from_ms < ? AND (valid_to_ms = 0 OR valid_to_ms >= ?))
				OR (valid_from_ms = 0 AND recorded_ms >= ? AND recorded_ms < ?))`
		args = append(args, toMS, fromMS, fromMS, toMS)
		if st := listStatuses(q); len(st) > 0 {
			query += ` AND status IN (` + placeholders(len(st)) + `)`
			for _, x := range st {
				args = append(args, string(x))
			}
		}
		query += ` ORDER BY recorded_ms DESC LIMIT ?`
		args = append(args, limit)
		rows, err := rd.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids
}

// activeStatuses resolves the validity filter: active rows by
// default, everything but withdrawn when the question is about the
// past (Section 10.7 step 4).
func activeStatuses(q Query) []Status {
	if q.Historical {
		return []Status{StatusActive, StatusExpired, StatusSuperseded, StatusDisputed}
	}
	// Disputed rows stay visible with a warning: the retriever
	// reports the contradiction instead of silently picking one.
	return []Status{StatusActive, StatusDisputed}
}

// listStatuses widens the validity filter with candidates when the
// caller scopes them to their proposing conversation; packaging
// still gates each candidate on its conversation (Section 10.5).
func listStatuses(q Query) []Status {
	st := activeStatuses(q)
	if q.Conversation != "" {
		st = append(st, StatusCandidate)
	}
	return st
}

// ---- RRF fusion ----

// fuseRRF merges ranked lists with Reciprocal Rank Fusion, k=60:
// score(id) = Σ 1/(k+rank). k=60 is near-optimal but not critical
// (Section 10.7, Cormack et al.).
func fuseRRF(k float64, lists ...[]string) []fusedHit {
	if k <= 0 {
		k = 60
	}
	scores := map[string]float64{}
	first := map[string]int{}
	pos := 0
	for _, list := range lists {
		for rank, id := range list {
			scores[id] += 1 / (k + float64(rank+1))
			if _, ok := first[id]; !ok {
				first[id] = pos
			}
			pos++
		}
	}
	out := make([]fusedHit, 0, len(scores))
	for id, sc := range scores {
		out = append(out, fusedHit{id: id, score: sc, order: first[id]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].order < out[j].order
	})
	return out
}

type fusedHit struct {
	id    string
	score float64
	order int
}

// adjustAndPackage applies validity, decay, importance and
// confidence (step 4), resolves packaging scope labels (step 7) and
// loads full rows for the fused order.
func (s *Service) adjustAndPackage(ctx context.Context, scopes []string, q Query, fused []fusedHit, a queryAnalysis) []ResultItem {
	if len(fused) == 0 {
		return nil
	}
	now := s.now()
	byID := map[string]Memory{}
	_ = s.read(ctx, func(ctx context.Context, rd store.Queryer) error {
		for _, chunk := range chunkStrings(fusedIDs(fused), 200) {
			args := make([]any, 0, len(chunk))
			for _, id := range chunk {
				args = append(args, id)
			}
			rows, err := rd.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id IN (`+placeholders(len(chunk))+`)`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				m, err := scanMemory(rows)
				if err != nil {
					rows.Close()
					return err
				}
				byID[m.ID] = m
			}
			rows.Close()
		}
		return nil
	})
	var items []ResultItem
	wfrom, wto, hasWindow := windowOf(q, a)
	for _, h := range fused {
		m, ok := byID[h.id]
		if !ok {
			continue
		}
		if !hasRequired(m.Statement, a.required) {
			// Quoted phrases are mandatory lexical matches
			// (Section 10.7 step 1).
			continue
		}
		if m.Status == StatusCandidate && (q.Conversation == "" || !candidateVisible(ctx, s, m.ID, q.Conversation)) {
			continue
		}
		if m.Status == StatusRetired {
			continue
		}
		if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) && !q.Historical {
			// Expired rows never read as active (Section 10.3);
			// the nightly review flips the stored status later.
			continue
		}
		if hasWindow && !overlapsWindow(m, wfrom, wto) {
			continue
		}
		if !q.KnownAt.IsZero() && m.RecordedAt.UTC().After(q.KnownAt.UTC()) {
			// Bitemporal record axis: unknown at T (Section 10.3).
			continue
		}
		score := h.score * decay(m, now) * confidenceWeight(m)
		items = append(items, ResultItem{
			ID: m.ID, Scope: m.Scope, Kind: m.Kind, Statement: m.Statement,
			Status: m.Status, ShortCode: m.ShortCode,
			ValidFrom: m.ValidFrom.UTC(), ValidTo: m.ValidTo.UTC(),
			Sources: append([]Source(nil), m.Sources...), Score: score,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func fusedIDs(fused []fusedHit) []string {
	out := make([]string, 0, len(fused))
	for _, h := range fused {
		out = append(out, h.id)
	}
	return out
}

// hasRequired reports whether the statement carries every quoted
// phrase of the query.
func hasRequired(statement string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	l := strings.ToLower(statement)
	for _, r := range required {
		if !strings.Contains(l, strings.ToLower(r)) {
			return false
		}
	}
	return true
}

// windowOf resolves the validity window: explicit query bounds win
// over the interval parsed from the question text.
func windowOf(q Query, a queryAnalysis) (time.Time, time.Time, bool) {
	if !q.From.IsZero() || !q.To.IsZero() {
		return q.From.UTC(), q.To.UTC(), true
	}
	if a.hasTime {
		return a.from.UTC(), a.to.UTC(), true
	}
	return time.Time{}, time.Time{}, false
}

// overlapsWindow keeps memories whose validity overlaps the window;
// rows without known validity fall back to their recorded date.
func overlapsWindow(m Memory, from, to time.Time) bool {
	start := m.RecordedAt.UTC()
	end := m.RecordedAt.UTC()
	if !m.ValidFrom.IsZero() {
		start = m.ValidFrom.UTC()
	}
	if !m.ValidTo.IsZero() {
		end = m.ValidTo.UTC()
	}
	if !from.IsZero() && !end.After(from) {
		return false
	}
	if !to.IsZero() && !start.Before(to) {
		return false
	}
	return true
}

// candidateVisible checks the proposing-conversation gate without
// loading the row twice: candidates surface only inside their own
// conversation or task (Section 10.5).
func candidateVisible(ctx context.Context, s *Service, id, conv string) bool {
	visible := false
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var stored string
		if err := q.QueryRow(ctx, `SELECT conversation FROM memories WHERE id=?`, id).Scan(&stored); err != nil {
			return err
		}
		visible = stored != "" && stored == conv
		return nil
	})
	return visible
}

// decay applies the per-kind half-life from the recorded date.
func decay(m Memory, now time.Time) float64 {
	hl, ok := halfLives[m.Kind]
	if !ok || hl <= 0 {
		return 1
	}
	age := now.Sub(m.RecordedAt.UTC()).Hours() / 24
	if age < 0 {
		age = 0
	}
	return math.Pow(2, -age/hl)
}

// confidenceWeight blends confidence and importance around 0.5 so
// unrated rows neither vanish nor dominate.
func confidenceWeight(m Memory) float64 {
	c := clamp01(m.Confidence)
	i := clamp01(m.Importance)
	return (0.5 + 0.5*c) * (0.5 + 0.5*i)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func closeTop(items []ResultItem) bool {
	if len(items) < 3 || items[0].Score <= 0 {
		return false
	}
	return (items[0].Score-items[2].Score)/items[0].Score < rerankCloseGap
}

// packBudget truncates the item list to the token budget, grouping
// by scope with labels (Section 9.4: never mixed without labels).
// Tokens approximate to 4 chars; statements truncate with a marker.
func packBudget(items []ResultItem, maxTokens int) []ResultItem {
	if maxTokens <= 0 {
		return items
	}
	budget := maxTokens * 4
	var out []ResultItem
	for _, it := range items {
		cost := len(it.Statement) + len(it.Scope) + 64
		if budget-cost < 0 && len(out) > 0 {
			break
		}
		budget -= cost
		out = append(out, it)
	}
	return out
}

// sufficiency reports insufficient evidence, live contradictions
// and cross-scope ambiguity instead of a silent short list
// (Section 10.7 step 6).
func sufficiency(items []ResultItem, q Query, a queryAnalysis) []Notice {
	var notices []Notice
	if len(items) == 0 {
		return []Notice{{Code: "insufficient", Detail: "no memory matched all constraints; evidence insufficient"}}
	}
	_ = q
	var disputed []string
	for _, it := range items {
		if it.Status == StatusDisputed {
			code := it.ShortCode
			if code == "" {
				code = it.ID
			}
			disputed = append(disputed, code)
		}
	}
	if len(disputed) > 0 {
		sort.Strings(disputed)
		notices = append(notices, Notice{Code: "contradiction", Detail: "disputed memories in scope: " + strings.Join(disputed, ", ")})
	}
	if len(a.ambiguous) > 0 {
		notices = append(notices, Notice{Code: "ambiguous_scope", Detail: "name matches entities in several scopes: " + strings.Join(a.ambiguous, ", ")})
	}
	return notices
}

// touchUsage records use for decay and archiving (Section 10.3).
func (s *Service) touchUsage(ctx context.Context, items []ResultItem) {
	now := s.nowMS()
	_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		for _, it := range items {
			_, _ = ex.Exec(ctx, `UPDATE memories SET use_count=use_count+1, last_used_ms=? WHERE id=?`, now, it.ID)
		}
		return nil
	})
}
