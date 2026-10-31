package memory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Admission outcomes.
const (
	ActionMerged      = "merged"
	ActionAdmitted    = "admitted"
	ActionRefined     = "refined"
	ActionDisputed    = "disputed"
	ActionReplaced    = "replaced"
	ActionProposed    = "proposed"
	ActionRejected    = "rejected"
	verdictDuplicate  = "duplicate"
	verdictRefinement = "refinement"
	verdictConflict   = "contradiction"
	verdictNew        = "new"
)

// Write stores one explicitly requested memory ("remember that…", a
// corrected preference, a fact to keep): admission runs at once and
// the caller gets the stored row (Section 10.5 step 1). Empty trust
// defaults to user: an explicit write comes from the user.
func (s *Service) Write(ctx context.Context, c Candidate) (Memory, AdmitOutcome, error) {
	if c.Trust == "" {
		c.Trust = TrustUser
	}
	return s.admit(ctx, c)
}

// Propose stores one candidate fact that stays pending: it is only
// retrievable inside its own conversation or task until
// consolidation admits it (Section 10.5 step 2).
func (s *Service) Propose(ctx context.Context, c Candidate) (Memory, error) {
	if c.Trust == "" {
		c.Trust = TrustAgent
	}
	if err := validKind(c.Kind); err != nil {
		return Memory{}, err
	}
	scopes, err := s.resolveScopes([]string{c.Scope})
	if err != nil {
		return Memory{}, err
	}
	m := Memory{
		ID:         string(types.NewMemoryID()),
		Scope:      scopes[0],
		Kind:       normKind(c.Kind),
		Statement:  strings.TrimSpace(c.Statement),
		Entities:   append([]string(nil), c.Entities...),
		ValidFrom:  c.ValidFrom.UTC(),
		ValidTo:    c.ValidTo.UTC(),
		RecordedAt: s.now().UTC(),
		ExpiresAt:  c.ExpiresAt.UTC(),
		Status:     StatusCandidate,
		Confidence: c.Confidence,
		Importance: c.Importance,
		Sources:    append([]Source(nil), c.Sources...),
		Trust:      c.Trust,
	}
	if m.Statement == "" {
		return Memory{}, &inputError{"memory: candidate statement is empty"}
	}
	// Embed before the porter write so no provider I/O runs inside
	// BEGIN IMMEDIATE (Section 4.3).
	vector, spaceID, err := s.embedStatement(ctx, m.Statement)
	if err != nil {
		return Memory{}, err
	}
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		stored, err := insertMemoryTx(ctx, ex, m, vector, spaceID, s.settings.EmbeddingDimensions)
		if err != nil {
			return err
		}
		m = stored
		if err := setConversationTx(ctx, ex, m.ID, c.Conversation); err != nil {
			return err
		}
		if vector == nil {
			return enqueuePendingTx(ctx, ex, "memory", m.ID, m.Scope, s.nowMS())
		}
		return nil
	})
	if err != nil {
		return Memory{}, err
	}
	return m, nil
}

// Admit runs the Section 10.5 step-4 rules for one candidate inside
// its scope: exact and near duplicates merge provenance, the
// ambiguous band (0.80-0.95) goes to the decider, refinements replace
// with superseded_by and valid_to, and contradictions either replace
// on explicit temporal change or leave both disputed with a queued
// user question. External content never becomes a preference.
func (s *Service) Admit(ctx context.Context, c Candidate) (AdmitOutcome, error) {
	if c.Trust == "" {
		c.Trust = TrustAgent
	}
	_, outcome, err := s.admit(ctx, c)
	return outcome, err
}

func (s *Service) admit(ctx context.Context, c Candidate) (Memory, AdmitOutcome, error) {
	if err := validKind(c.Kind); err != nil {
		return Memory{}, AdmitOutcome{}, err
	}
	if strings.TrimSpace(c.Statement) == "" {
		return Memory{}, AdmitOutcome{}, &inputError{"memory: candidate statement is empty"}
	}
	if c.Trust == TrustExternal && c.Kind == KindPreference {
		return Memory{}, AdmitOutcome{}, ErrExternalIdentity
	}
	scopes, err := s.resolveScopes([]string{c.Scope})
	if err != nil {
		return Memory{}, AdmitOutcome{}, err
	}
	scope := scopes[0]
	kind := normKind(c.Kind)
	now := s.now().UTC()

	// Phase 1 (reads plus model/embedder calls, never inside the
	// porter write): gather the similar set and judge the band.
	best, sim, err := s.findSimilar(ctx, scope, c)
	if err != nil {
		return Memory{}, AdmitOutcome{}, err
	}
	verdict := verdictNew
	if best != nil {
		if normStatement(c.Statement) == normStatement(best.Statement) && sharesEntity(c.Entities, best.Entities) {
			verdict = verdictDuplicate
		} else {
			verdict, err = s.decideBand(ctx, c, *best, sim)
			if err != nil {
				return Memory{}, AdmitOutcome{}, err
			}
		}
	}

	// Embed the new statement before the write so no network I/O
	// ever runs inside BEGIN IMMEDIATE (Section 4.3: quick commands).
	var vector []float32
	var space string
	if verdict != verdictDuplicate {
		vector, space, _ = s.embedStatement(ctx, strings.TrimSpace(c.Statement))
	}

	// Phase 2: one porter transaction applies the judged outcome.
	var (
		outcome AdmitOutcome
		stored  Memory
	)
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		switch verdict {
		case verdictDuplicate:
			merged, err := mergeProvenanceTx(ctx, ex, *best, c.Sources)
			if err != nil {
				return err
			}
			stored = merged
			outcome = AdmitOutcome{Action: ActionMerged, MemoryID: best.ID, Related: best.ID}
		case verdictRefinement:
			nm, err := s.insertActiveTx(ctx, ex, scope, c, kind, now, vector, space)
			if err != nil {
				return err
			}
			if err := supersedeTx(ctx, ex, best.ID, nm.ID, now); err != nil {
				return err
			}
			stored = nm
			outcome = AdmitOutcome{Action: ActionRefined, MemoryID: nm.ID, Related: best.ID}
		case verdictConflict:
			nm, err := s.insertActiveTx(ctx, ex, scope, c, kind, now, vector, space)
			if err != nil {
				return err
			}
			if hasTemporalChangeMarker(c.Statement) {
				if err := supersedeTx(ctx, ex, best.ID, nm.ID, now); err != nil {
					return err
				}
				stored = nm
				outcome = AdmitOutcome{Action: ActionReplaced, MemoryID: nm.ID, Related: best.ID}
			} else {
				if err := disputeTx(ctx, ex, best.ID); err != nil {
					return err
				}
				if err := disputeTx(ctx, ex, nm.ID); err != nil {
					return err
				}
				stored = nm
				outcome = AdmitOutcome{Action: ActionDisputed, MemoryID: nm.ID, Related: best.ID, NeedsUser: true}
			}
		default:
			nm, err := s.insertActiveTx(ctx, ex, scope, c, kind, now, vector, space)
			if err != nil {
				return err
			}
			stored = nm
			outcome = AdmitOutcome{Action: ActionAdmitted, MemoryID: nm.ID}
		}
		return nil
	})
	if err != nil {
		return Memory{}, AdmitOutcome{}, err
	}
	return stored, outcome, nil
}

// decideBand applies the cosine gates: at/above ExactDuplicateAt
// with a shared entity merges, inside the band the decider judges,
// below the band the candidate is new.
func (s *Service) decideBand(ctx context.Context, c Candidate, best Memory, sim float64) (string, error) {
	if sim >= s.settings.ExactDuplicateAt && sharesEntity(c.Entities, best.Entities) {
		return verdictDuplicate, nil
	}
	if sim >= s.settings.ExactDuplicateAt {
		// Above the line but no shared entity: let the decider
		// confirm instead of merging blindly.
		return s.decider.Classify(ctx, c, best)
	}
	if sim >= s.settings.AmbiguousBandLo {
		return s.decider.Classify(ctx, c, best)
	}
	return verdictNew, nil
}

// findSimilar gathers the similar set outside the porter write:
// active rows sharing an entity plus vector neighbors, ranked by
// exact cosine over the shadow vectors (rows without a vector fall
// back to statement overlap).
func (s *Service) findSimilar(ctx context.Context, scope string, c Candidate) (*Memory, float64, error) {
	var actives []Memory
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE scope_id=? AND status=? ORDER BY recorded_ms DESC LIMIT 500`, scope, string(StatusActive))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMemory(rows)
			if err != nil {
				return err
			}
			actives = append(actives, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	cands := map[string]Memory{}
	for _, m := range actives {
		if sharesEntity(c.Entities, m.Entities) {
			cands[m.ID] = m
		}
	}
	var query []float32
	if s.curEmbed() != nil {
		if vecs, err := s.curEmbed().Embed(ctx, []string{c.Statement}); err == nil && len(vecs) > 0 {
			query = fitDims(vecs[0], s.settings.EmbeddingDimensions)
			space := spaceID(s.curEmbed().Model(), s.settings.EmbeddingDimensions)
			var ids []string
			_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
				var err error
				ids, _, err = shadowTopK(ctx, q, "memory", query, []string{scope}, space, 20)
				return err
			})
			for _, id := range ids {
				if _, ok := cands[id]; !ok {
					for _, m := range actives {
						if m.ID == id {
							cands[id] = m
						}
					}
				}
			}
		}
	}
	if len(cands) == 0 {
		return nil, 0, nil
	}
	var shadows map[string][]float32
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		shadows = readShadowSet(ctx, q, "memory", scopeIDs(cands), shadowSpace(s))
		return nil
	})
	cn := normStatement(c.Statement)
	var best *Memory
	bestSim := 0.0
	for _, m := range cands {
		cp := m
		sim := statementOverlap(cn, normStatement(m.Statement))
		if vec, ok := shadows[m.ID]; ok && query != nil {
			sim = Cosine(query, vec)
		}
		if best == nil || sim > bestSim {
			best = &cp
			bestSim = sim
		}
	}
	return best, bestSim, nil
}

// scopeIDs collects the ids of a candidate map.
func scopeIDs(cands map[string]Memory) []string {
	out := make([]string, 0, len(cands))
	for id := range cands {
		out = append(out, id)
	}
	return out
}

// shadowSpace names the active shadow space, or "" when no embedder
// is configured.
func shadowSpace(s *Service) string {
	if s.curEmbed() == nil {
		return ""
	}
	return spaceID(s.curEmbed().Model(), s.settings.EmbeddingDimensions)
}

// readShadowSet bulk-reads exact shadow vectors for ids.
func readShadowSet(ctx context.Context, q store.Queryer, kind string, ids []string, space string) map[string][]float32 {
	out := map[string][]float32{}
	if len(ids) == 0 || space == "" {
		return out
	}
	for _, chunk := range chunkStrings(ids, 200) {
		args := []any{kind, space}
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := q.Query(ctx, `SELECT entity_id, vector FROM embedding_vectors WHERE entity_kind=? AND space_id=? AND entity_id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return out
		}
		for rows.Next() {
			var id string
			var raw []byte
			if err := rows.Scan(&id, &raw); err != nil {
				break
			}
			if vec, err := decodeVector(raw); err == nil {
				out[id] = vec
			}
		}
		rows.Close()
	}
	return out
}

func chunkStrings(in []string, n int) [][]string {
	var out [][]string
	for len(in) > 0 {
		if len(in) < n {
			n = len(in)
		}
		out = append(out, in[:n])
		in = in[n:]
	}
	return out
}

// embedStatement embeds for storage; a failing provider yields a nil
// vector so the row stores without one and queues for later.
func (s *Service) embedStatement(ctx context.Context, text string) ([]float32, string, error) {
	if s.curEmbed() == nil {
		return nil, "", nil
	}
	vecs, err := s.curEmbed().Embed(ctx, []string{text})
	if err != nil {
		return nil, "", nil
	}
	if len(vecs) == 0 {
		return nil, "", nil
	}
	space, err := "", error(nil)
	_ = space
	return fitDims(vecs[0], s.settings.EmbeddingDimensions), spaceID(s.curEmbed().Model(), s.settings.EmbeddingDimensions), err
}

func (s *Service) insertActiveTx(ctx context.Context, ex store.Execer, scope string, c Candidate, kind Kind, now time.Time, vector []float32, space string) (Memory, error) {
	m := Memory{
		ID:         string(types.NewMemoryID()),
		Scope:      scope,
		Kind:       kind,
		Statement:  strings.TrimSpace(c.Statement),
		Entities:   append([]string(nil), c.Entities...),
		ValidFrom:  c.ValidFrom.UTC(),
		ValidTo:    c.ValidTo.UTC(),
		RecordedAt: now,
		ExpiresAt:  c.ExpiresAt.UTC(),
		Status:     StatusActive,
		Confidence: c.Confidence,
		Importance: c.Importance,
		Sources:    append([]Source(nil), c.Sources...),
		Trust:      c.Trust,
	}
	stored, err := insertMemoryTx(ctx, ex, m, vector, space, s.settings.EmbeddingDimensions)
	if err != nil {
		return Memory{}, err
	}
	if err := setConversationTx(ctx, ex, stored.ID, c.Conversation); err != nil {
		return Memory{}, err
	}
	if vector == nil {
		if err := enqueuePendingTx(ctx, ex, "memory", stored.ID, scope, now.UnixMilli()); err != nil {
			return Memory{}, err
		}
	}
	return stored, nil
}

func mergeProvenanceTx(ctx context.Context, ex store.Execer, old Memory, add []Source) (Memory, error) {
	merged := mergeSources(old.Sources, add)
	if _, err := ex.Exec(ctx, `UPDATE memories SET sources_json=?, updated_at=strftime('%s','now')*1000 WHERE id=?`, encodeJSON(merged), old.ID); err != nil {
		return Memory{}, err
	}
	old.Sources = merged
	return old, nil
}

func mergeSources(a, b []Source) []Source {
	seen := map[string]bool{}
	var out []Source
	for _, s := range append(append([]Source(nil), a...), b...) {
		k := s.Kind + "\x00" + s.Ref
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

func supersedeTx(ctx context.Context, ex store.Execer, oldID, newID string, now time.Time) error {
	_, err := ex.Exec(ctx, `UPDATE memories SET status=?, superseded_by=?, valid_to_ms=?, updated_at=strftime('%s','now')*1000 WHERE id=?`,
		string(StatusSuperseded), newID, now.UnixMilli(), oldID)
	return err
}

func disputeTx(ctx context.Context, ex store.Execer, id string) error {
	_, err := ex.Exec(ctx, `UPDATE memories SET status=?, updated_at=strftime('%s','now')*1000 WHERE id=? AND status=?`,
		string(StatusDisputed), id, string(StatusActive))
	return err
}

func setConversationTx(ctx context.Context, ex store.Execer, id, conv string) error {
	if conv == "" {
		return nil
	}
	_, err := ex.Exec(ctx, `UPDATE memories SET conversation=? WHERE id=?`, conv, id)
	return err
}

// PendingDisputes lists disputed memories with their counterparts
// for the master to turn into low-priority user questions
// (Section 10.5): the memory package never writes the questions
// table owned by the orchestrator.
func (s *Service) PendingDisputes(ctx context.Context, scopes []string) ([]Memory, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Memory
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE scope_id IN (`+placeholders(len(scopes))+`) AND status=? ORDER BY recorded_ms DESC LIMIT 100`, append(scopeArgs(scopes), string(StatusDisputed))...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMemory(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func placeholders(n int) string {
	ph := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ph = append(ph, "?")
	}
	return strings.Join(ph, ",")
}

func scopeArgs(scopes []string) []any {
	out := make([]any, 0, len(scopes))
	for _, sc := range scopes {
		out = append(out, sc)
	}
	return out
}

// GetMemory reads one row by id within the scope set.
func (s *Service) GetMemory(ctx context.Context, scopes []string, id string) (Memory, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return Memory{}, err
	}
	var out Memory
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE id=?`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return fmt.Errorf("%w: memory %q", ErrNotFound, id)
		}
		m, err := scanMemory(rows)
		if err != nil {
			return err
		}
		for _, sc := range scopes {
			if m.Scope == sc {
				out = m
				return nil
			}
		}
		return fmt.Errorf("%w: %q", ErrScopeDenied, m.Scope)
	})
	return out, err
}

// ---- statement similarity helpers ----

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

func normStatement(s string) string {
	return strings.Join(wordRe.FindAllString(strings.ToLower(s), -1), " ")
}

func sharesEntity(a, b []string) bool {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	for _, y := range b {
		if set[y] {
			return true
		}
	}
	return false
}

// statementOverlap is the Jaccard index over word sets.
func statementOverlap(a, b string) float64 {
	sa := map[string]bool{}
	for _, w := range strings.Fields(a) {
		sa[w] = true
	}
	sb := map[string]bool{}
	for _, w := range strings.Fields(b) {
		sb[w] = true
	}
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for w := range sa {
		if sb[w] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// isRefinement reports substring containment: the longer statement
// adds detail to the shorter (Section 10.5 refinement).
func isRefinement(a, b string) bool {
	if a == b || a == "" || b == "" {
		return false
	}
	return strings.Contains(a, b) || strings.Contains(b, a)
}

var negationRe = regexp.MustCompile(`\b(not|no|never|n't|without|isn't|aren't|don't|doesn't|can't|won't)\b`)

// contradicts reports negation asymmetry over a shared core:
// overlap above 0.5 with a negation flip.
func contradicts(a, b string) bool {
	if statementOverlap(a, b) < 0.5 {
		return false
	}
	na, nb := negationRe.MatchString(a), negationRe.MatchString(b)
	return na != nb
}

var temporalMarkers = []string{
	"now ", "currently", "moved to", "changed to", "no longer",
	"as of", "from now on", "used to be",
}

// hasTemporalChangeMarker reports an explicit temporal change ("now
// I live in…"): contradictions with a marker replace instead of
// disputing (Section 10.5).
func hasTemporalChangeMarker(statement string) bool {
	n := " " + normStatement(statement) + " "
	for _, m := range temporalMarkers {
		if strings.Contains(n, " "+m) || strings.HasPrefix(n, " "+m) {
			return true
		}
	}
	return false
}

func validKind(k Kind) error {
	switch normKind(k) {
	case KindFact, KindPreference, KindEvent, KindCommitment, KindBelief, KindRelation, KindProcedure:
		return nil
	default:
		return &inputError{fmt.Sprintf("memory: unknown kind %q", string(k))}
	}
}

func normKind(k Kind) Kind {
	if k == "" {
		return KindFact
	}
	return k
}

// inputError marks caller faults the tools translate to input_error.
type inputError struct{ msg string }

func (e *inputError) Error() string { return e.msg }

// IsInputError reports caller faults for tool translation.
func IsInputError(err error) bool {
	var ie *inputError
	return errors.As(err, &ie)
}
