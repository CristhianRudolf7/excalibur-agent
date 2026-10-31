package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Review scheduling constants (Sections 10.6, Appendix B).
const (
	// reviewLookbackDays sizes the activity histogram.
	reviewLookbackDays = 14
	// reviewWindowMinutes is the default quiet-window length; the
	// estimate step refines it per scope workload.
	reviewWindowMinutes = 60
	// fallbackHour/fallbackMinute is the 03:30 local fallback before
	// day five, gated on two idle hours.
	fallbackHour, fallbackMinute = 3, 30
	// idleGateHours is the required inactivity before the fallback.
	idleGateHours = 2
	// reviewBatchTurns checkpoints the watermark per batch so a
	// yielded review resumes from its last batch (Section 10.6).
	reviewBatchTurns = 50
)

// SetEmbedder swaps the active embedder outside a reindex (sidecar
// load and unload, Section 10.8). It clears any retiring state; use
// StartReindex for model changes.
func (s *Service) SetEmbedder(e Embedder) {
	s.embeds.Store(&embedSet{cur: e})
}

// Background returns a Service twin whose writes run on the
// background lane: the nightly review never contends with
// interactive or journal writes (Section 10.6). The twin shares the
// store and seams; only the porter lane differs.
func (s *Service) Background() *Service {
	c := NewService(s.db,
		WithSettings(s.settings),
		WithScopes(s.scopes...),
		WithClock(s.now),
		WithDecider(s.decider),
		WithExtractor(s.extract),
		WithReranker(s.rerank),
		WithRewriter(s.rewriter),
		WithVision(s.vision),
		WithTelemetry(s.tele),
	)
	if set := s.embeds.Load(); set != nil {
		c.embeds.Store(&embedSet{cur: set.cur, prev: set.prev, prevSpace: set.prevSpace})
	}
	c.prio = store.PriorityBackground
	return c
}

// reviewPrio is the porter lane of ctxWrite; Reviews use Background.
func (s *Service) writePrio() store.Priority {
	if s.prio != 0 {
		return s.prio
	}
	return store.PriorityNormal
}

// ShouldReview decides whether the nightly pass runs now for scope.
// Before the fifth active day only the 03:30 local fallback with two
// idle hours applies; from day five the computed quiet window
// applies (Section 10.6 activation).
func (s *Service) ShouldReview(ctx context.Context, scope string, now time.Time) (bool, string) {
	now = now.UTC()
	st, err := s.loadReviewState(ctx, scope)
	if err != nil {
		return false, "state unavailable: " + err.Error()
	}
	idle := s.idleDuration(ctx, scope, now)
	if st.ActiveDays < s.settings.ConsolidationIdleDays {
		if inFallbackWindow(now.In(s.location()), fallbackHour, fallbackMinute) && idle >= idleGateHours*time.Hour {
			return true, "fallback 03:30 window with 2h idle"
		}
		return false, "light consolidation only before day 5"
	}
	if inWindow(now.In(s.location()), st.WindowStart, st.WindowLength) {
		return true, "quiet window"
	}
	return false, "outside quiet window"
}

// inFallbackWindow reports the hour starting at 03:30 local.
func inFallbackWindow(local time.Time, hour, min int) bool {
	start := time.Date(local.Year(), local.Month(), local.Day(), hour, min, 0, 0, local.Location())
	return !local.Before(start) && local.Before(start.Add(time.Hour))
}

// inWindow reports a minute-of-day window, wrapping midnight.
func inWindow(local time.Time, startMin, lenMin int) bool {
	if lenMin <= 0 {
		return false
	}
	m := local.Hour()*60 + local.Minute()
	end := (startMin + lenMin) % (24 * 60)
	if startMin+lenMin <= 24*60 {
		return m >= startMin && m < startMin+lenMin
	}
	return m >= startMin || m < end
}

// NextWindow recomputes the quiet window from the trailing-14-day
// activity histogram in the user zone and persists it with the
// weekly cadence the Agenda drives (Section 10.6 step 2).
func (s *Service) NextWindow(ctx context.Context, scope string, now time.Time) (startMin, lengthMin int, err error) {
	counts, err := s.hourHistogram(ctx, scope, now)
	if err != nil {
		return 0, 0, err
	}
	startMin = quietStart(counts, reviewWindowMinutes)
	st, err := s.loadReviewState(ctx, scope)
	if err != nil {
		return 0, 0, err
	}
	st.WindowStart, st.WindowLength = startMin, reviewWindowMinutes
	if err := s.saveReviewState(ctx, st); err != nil {
		return 0, 0, err
	}
	return startMin, reviewWindowMinutes, nil
}

// hourHistogram counts user messages and user-started runs per local
// hour over the trailing 14 days.
func (s *Service) hourHistogram(ctx context.Context, scope string, now time.Time) ([24]int, error) {
	var counts [24]int
	loc := s.location()
	since := now.AddDate(0, 0, -reviewLookbackDays).UnixMilli()
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT sent_at_ms FROM messages WHERE scope_id=? AND role='user' AND sent_at_ms>=?`, scope, since)
		if err != nil {
			return err
		}
		for rows.Next() {
			var at int64
			if err := rows.Scan(&at); err != nil {
				rows.Close()
				return err
			}
			counts[fromMS(at).In(loc).Hour()]++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rrows, err := q.Query(ctx, `SELECT created_at FROM runs WHERE scope_id=? AND created_at>=?`, scope, since)
		if err != nil {
			return err
		}
		defer rrows.Close()
		for rrows.Next() {
			var at int64
			if err := rrows.Scan(&at); err != nil {
				return err
			}
			counts[fromMS(at).In(loc).Hour()]++
		}
		return rrows.Err()
	})
	return counts, err
}

// quietStart finds the contiguous window of lengthMin with the least
// activity; ties prefer the earliest after midnight local.
func quietStart(counts [24]int, lengthMin int) int {
	hours := (lengthMin + 59) / 60
	if hours < 1 {
		hours = 1
	}
	best, bestSum := 0, -1
	for start := 0; start < 24; start++ {
		sum := 0
		for h := 0; h < hours; h++ {
			sum += counts[(start+h)%24]
		}
		if bestSum < 0 || sum < bestSum {
			best, bestSum = start, sum
		}
	}
	return best * 60
}

// idleDuration measures silence since the last user message or run.
func (s *Service) idleDuration(ctx context.Context, scope string, now time.Time) time.Duration {
	latest := int64(0)
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var m, r int64
		_ = q.QueryRow(ctx, `SELECT COALESCE(MAX(sent_at_ms),0) FROM messages WHERE scope_id=?`, scope).Scan(&m)
		_ = q.QueryRow(ctx, `SELECT COALESCE(MAX(created_at),0) FROM runs WHERE scope_id=?`, scope).Scan(&r)
		if m > latest {
			latest = m
		}
		if r > latest {
			latest = r
		}
		return nil
	})
	if latest == 0 {
		return 24 * time.Hour
	}
	d := now.Sub(fromMS(latest))
	if d < 0 {
		return 0
	}
	return d
}

// ReviewOptions bounds one nightly pass per scope.
type ReviewOptions struct {
	// TokenBudget caps extraction input; turns beyond it wait for
	// the next night.
	TokenBudget int
	// Light runs only extraction, admission and expirations (the
	// pre-day-five pass, Section 10.6).
	Light bool
}

// ReviewScope runs the Section 10.6 steps for one scope: gather
// episodes since the watermark, extract and admit, expire, verify
// provenance and validity, resolve contradictions, link the graph,
// compact notes, confirm profile proposals and persist the report.
// Batches checkpoint the watermark; a canceled context yields and
// the next night resumes from the last batch (P3 lane behavior).
func (s *Service) ReviewScope(ctx context.Context, scope string, opts ReviewOptions) (ReviewReport, error) {
	bg := s.Background()
	scopes, err := bg.resolveScopes([]string{scope})
	if err != nil {
		return ReviewReport{}, err
	}
	scope = scopes[0]
	rep := ReviewReport{Scope: scope, StartedAt: bg.now().UTC()}
	if err := ctx.Err(); err != nil {
		// Already canceled: yield immediately with the checkpoint
		// intact instead of failing the night (Section 10.6).
		rep.Warnings = append(rep.Warnings, "yielded to user activity at entry: "+err.Error())
		return rep, nil
	}
	budget := opts.TokenBudget
	if budget <= 0 {
		budget = 12000
	}
	st, err := bg.loadReviewState(ctx, scope)
	if err != nil {
		return ReviewReport{}, err
	}
	bg.countActiveDay(ctx, &st, scope)

	// Step 1+2: episodes since the watermark, in checkpointed
	// batches. One context per scope only: turns never cross scopes
	// (Section 9.5).
	turns, err := bg.turnsSince(ctx, scope, st.Watermark, budget)
	if err != nil {
		if ctx.Err() != nil {
			// Yielded mid-gather: the per-batch checkpoints
			// already persisted; the partial report stays in
			// the return value since the canceled context
			// cannot reliably write it.
			rep.Warnings = append(rep.Warnings, "yielded to user activity during gather: "+ctx.Err().Error())
			return rep, nil
		}
		return ReviewReport{}, err
	}
	admitted, merged := 0, 0
	for _, batch := range batchTurns(turns, reviewBatchTurns) {
		if err := ctx.Err(); err != nil {
			rep.Warnings = append(rep.Warnings, "yielded to user activity; resumes from last batch: "+err.Error())
			break
		}
		var cands []Candidate
		if bg.extract != nil {
			cands, err = bg.extract.Extract(ctx, scope, batch)
			if err != nil {
				rep.Warnings = append(rep.Warnings, "extraction failed for one batch: "+err.Error())
				continue
			}
		}
		for _, c := range cands {
			c.Scope = scope
			outcome, err := bg.Admit(ctx, c)
			if err != nil {
				if err == ErrExternalIdentity {
					rep.Warnings = append(rep.Warnings, "external candidate refused as preference: "+truncate(c.Statement, 80))
					continue
				}
				rep.Warnings = append(rep.Warnings, "admission failed: "+err.Error())
				continue
			}
			switch outcome.Action {
			case ActionAdmitted, ActionRefined, ActionReplaced:
				admitted++
			case ActionMerged:
				merged++
			case ActionDisputed:
				rep.Disputed++
				rep.Disputes = append(rep.Disputes, outcome.MemoryID)
			}
		}
		st.Watermark = batchWatermark(batch, st.Watermark)
		if err := bg.saveReviewState(ctx, st); err != nil {
			return ReviewReport{}, err
		}
	}
	rep.Admitted, rep.Merged = admitted, merged

	// Step 3: expirations flip to expired, history preserved.
	expired, err := bg.expireDue(ctx, scope)
	if err != nil {
		return ReviewReport{}, err
	}
	rep.Expired = expired

	if !opts.Light {
		// Step 4: provenance and validity.
		retired, warnings, err := bg.verifyProvenance(ctx, scope)
		if err != nil {
			return ReviewReport{}, err
		}
		rep.Retired = retired
		rep.Warnings = append(rep.Warnings, warnings...)

		// Step 5: contradictions.
		disputed, err := bg.resolveContradictions(ctx, scope)
		if err != nil {
			return ReviewReport{}, err
		}
		rep.Disputed += disputed

		// Step 6: contextual graph.
		if warnings := bg.linkGraph(ctx, scope); len(warnings) > 0 {
			rep.Warnings = append(rep.Warnings, warnings...)
		}

		// Step 7: note compaction with the 25% safeguard.
		rejected, err := bg.compactNotes(ctx, scope)
		if err != nil {
			return ReviewReport{}, err
		}
		rep.NotesRejected = rejected

		// Step 8: profile proposals with repeated evidence.
		if n, err := bg.confirmProposals(ctx, scope); err == nil && n > 0 {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d profile proposals confirmed by repeated evidence", n))
		}

		// Relation-type needs surface as warnings for the
		// memory-graph-modeling skill (Section 10.4).
		if needs := bg.relationNeeds(ctx, scope); len(needs) > 0 {
			rep.Warnings = append(rep.Warnings, needs...)
		}
	}

	// Step 9: the consultable report.
	st.LastRun = bg.nowMS()
	if err := bg.saveReviewState(ctx, st); err != nil {
		return ReviewReport{}, err
	}
	if err := bg.saveReport(ctx, rep); err != nil {
		return ReviewReport{}, err
	}
	return rep, nil
}

func batchTurns(turns []Turn, n int) [][]Turn {
	var out [][]Turn
	for len(turns) > 0 {
		if len(turns) < n {
			n = len(turns)
		}
		out = append(out, turns[:n])
		turns = turns[n:]
	}
	return out
}

func batchWatermark(batch []Turn, prev int64) int64 {
	max := prev
	for _, t := range batch {
		if ms := t.At.UnixMilli(); ms > max {
			max = ms
		}
	}
	return max
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// turnsSince reads fresh episode turns capped by the token budget
// (~40 chars per token of message text).
func (s *Service) turnsSince(ctx context.Context, scope string, watermark int64, budget int) ([]Turn, error) {
	var turns []Turn
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id, conversation_id, text, sent_at_ms FROM messages WHERE scope_id=? AND sent_at_ms>? ORDER BY sent_at_ms LIMIT 500`, scope, watermark)
		if err != nil {
			return err
		}
		defer rows.Close()
		chars := 0
		for rows.Next() {
			var t Turn
			var at int64
			if err := rows.Scan(&t.MsgID, &t.Conversation, &t.Text, &at); err != nil {
				return err
			}
			t.At = fromMS(at)
			chars += len(t.Text)
			if chars > budget*4 && len(turns) > 0 {
				break
			}
			turns = append(turns, t)
		}
		return rows.Err()
	})
	return turns, err
}

// expireDue flips due rows to expired; readers already exclude them
// (Section 10.3).
func (s *Service) expireDue(ctx context.Context, scope string) (int, error) {
	n := 0
	err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `UPDATE memories SET status=?, updated_at=strftime('%s','now')*1000 WHERE scope_id=? AND status=? AND expires_at_ms != 0 AND expires_at_ms <= ?`,
			string(StatusExpired), scope, string(StatusActive), s.nowMS())
		if err != nil {
			return err
		}
		aff, _ := res.RowsAffected()
		n = int(aff)
		return nil
	})
	return n, err
}

// verifyProvenance retires memories whose sources all died and
// re-checks rows depending on replaced sources. User and agent rows
// without sources stay: explicit writes need no external backing
// (Section 10.6 step 4).
func (s *Service) verifyProvenance(ctx context.Context, scope string) (int, []string, error) {
	var actives []Memory
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE scope_id=? AND status=?`, scope, string(StatusActive))
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
		return 0, nil, err
	}
	retired := 0
	var warnings []string
	for _, m := range actives {
		alive, dead := s.checkSources(ctx, m)
		if len(dead) > 0 && len(alive) == 0 && m.Trust == TrustExternal {
			if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
				_, err := ex.Exec(ctx, `UPDATE memories SET status=?, updated_at=strftime('%s','now')*1000 WHERE id=?`, string(StatusRetired), m.ID)
				return err
			}); err != nil {
				return 0, nil, err
			}
			retired++
			continue
		}
		if len(dead) > 0 {
			pruned := alive
			if len(alive) == 0 {
				pruned = m.Sources
			}
			if len(alive) > 0 {
				_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
					_, err := ex.Exec(ctx, `UPDATE memories SET sources_json=? WHERE id=?`, encodeJSON(pruned), m.ID)
					return err
				})
			}
			warnings = append(warnings, fmt.Sprintf("%s lost %d dead sources", shortOf(m), len(dead)))
		}
	}
	return retired, warnings, nil
}

func shortOf(m Memory) string {
	if m.ShortCode != "" {
		return m.ShortCode
	}
	return m.ID
}

// checkSources splits sources into live and dead: message sources
// must still exist; tool, document and page sources are external
// and assumed live here (their own stores own them).
func (s *Service) checkSources(ctx context.Context, m Memory) (alive, dead []Source) {
	for _, src := range m.Sources {
		if src.Kind != "message" {
			alive = append(alive, src)
			continue
		}
		exists := false
		_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
			var n int
			if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE id=?`, src.Ref).Scan(&n); err == nil && n > 0 {
				exists = true
			}
			return nil
		})
		if exists {
			alive = append(alive, src)
		} else {
			dead = append(dead, src)
		}
	}
	return alive, dead
}

// resolveContradictions settles same-entity, same-kind pairs: the
// newest explicit statement with a temporal marker wins and
// supersedes the older; undecidable pairs stay disputed with a
// pending user question (Section 10.6 step 5). Systems evaluated on
// multi-step updates resolve at most 7% (MemoryAgentBench), so the
// harness marks uncertainty rather than resolving badly.
func (s *Service) resolveContradictions(ctx context.Context, scope string) (int, error) {
	var actives []Memory
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE scope_id=? AND status=? ORDER BY recorded_ms`, scope, string(StatusActive))
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
		return 0, err
	}
	disputed := 0
	done := map[string]bool{}
	for i := 0; i < len(actives); i++ {
		for j := i + 1; j < len(actives); j++ {
			a, b := actives[i], actives[j]
			if done[a.ID] || done[b.ID] || a.Kind != b.Kind || !sharesEntity(a.Entities, b.Entities) {
				continue
			}
			if !contradicts(normStatement(a.Statement), normStatement(b.Statement)) {
				continue
			}
			older, newer := a, b
			if b.RecordedAt.Before(a.RecordedAt) {
				older, newer = b, a
			}
			if hasTemporalChangeMarker(newer.Statement) {
				if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
					return supersedeTx(ctx, ex, older.ID, newer.ID, s.now().UTC())
				}); err != nil {
					return disputed, err
				}
			} else {
				if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
					if err := disputeTx(ctx, ex, older.ID); err != nil {
						return err
					}
					return disputeTx(ctx, ex, newer.ID)
				}); err != nil {
					return disputed, err
				}
				disputed += 2
			}
			done[a.ID], done[b.ID] = true, true
		}
	}
	return disputed, nil
}

// linkGraph ensures entity rows for referenced ids and records
// memory→entity mention edges, plus relation edges that name a
// known relation type with both endpoints resolved (Section 10.6
// step 6). Explicit mentions (known aliases, ids) link
// deterministically; implicit relations wait for the model during
// consolidation (Section 10.4).
func (s *Service) linkGraph(ctx context.Context, scope string) []string {
	var warnings []string
	var mems []Memory
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT `+memoryColumns+` FROM memories WHERE scope_id=? AND status IN (?,?)`, scope, string(StatusActive), string(StatusDisputed))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMemory(rows)
			if err != nil {
				return err
			}
			mems = append(mems, m)
		}
		return rows.Err()
	})
	_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		if err := ensureSeedRelationTypesTx(ctx, ex); err != nil {
			warnings = append(warnings, "seed relation types: "+err.Error())
			return nil
		}
		for _, m := range mems {
			for _, eid := range m.Entities {
				var n int
				if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM entities WHERE id=?`, eid).Scan(&n); err != nil || n > 0 {
					if err != nil {
						warnings = append(warnings, "entity check: "+err.Error())
					}
				} else {
					if _, err := ex.Exec(ctx, `INSERT INTO entities(id, scope_id, type, canonical, aliases_json) VALUES(?,?,?,?,?)`,
						eid, scope, "concept", eid, encodeJSON([]string{})); err != nil {
						warnings = append(warnings, "entity placeholder: "+err.Error())
					}
				}
				var en int
				_ = ex.QueryRow(ctx, `SELECT COUNT(*) FROM edges WHERE layer='memory' AND from_id=? AND to_id=? AND rel='mentions'`, m.ID, eid).Scan(&en)
				if en == 0 {
					if err := insertEdgeTx(ctx, ex, Edge{ID: newEdgeID(), FromType: "memory", FromID: m.ID, Rel: "mentions", ToType: "entity", ToID: eid, Scope: scope}); err != nil {
						warnings = append(warnings, "mention edge: "+err.Error())
					}
				}
			}
		}
		return nil
	})
	return warnings
}

// compactNotes summarizes notebook fragments older than 30 days into
// a compiled truth plus an append-only timeline, one line per
// removed fragment (GBrain pages, Section 10.6 step 7). The rewrite
// is refused when it would drop fragments without preserving them
// as timeline lines: never remove more entries than preserved.
func (s *Service) compactNotes(ctx context.Context, scope string) (int, error) {
	rejected := 0
	err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		doc, err := ensureDocTx(ctx, ex, scope, DocNotebook, docTitle(DocNotebook))
		if err != nil {
			return err
		}
		cutoff := s.nowMS() - int64(30*24*time.Hour/time.Millisecond)
		rows, err := ex.Query(ctx, `SELECT id, doc_id, heading_path, position, text, version, hash FROM doc_fragments WHERE doc_id=? AND created_at < ? ORDER BY position`, doc.ID, cutoff)
		if err != nil {
			return err
		}
		type oldFrag struct {
			id, heading, text string
			version           int
		}
		var olds []oldFrag
		for rows.Next() {
			var o oldFrag
			var docID, hash string
			var pos int
			if err := rows.Scan(&o.id, &docID, &o.heading, &pos, &o.text, &o.version, &hash); err != nil {
				rows.Close()
				return err
			}
			olds = append(olds, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(olds) == 0 {
			return nil
		}
		var timeline []string
		var truths []string
		for _, o := range olds {
			first := strings.SplitN(strings.TrimSpace(o.text), "\n", 2)[0]
			timeline = append(timeline, first)
			if len(truths) < 5 {
				truths = append(truths, first)
			}
		}
		// Safeguard: every removed fragment must survive as a
		// timeline line (Section 10.6; the OpenClaw 25% rule
		// generalized to lossless compaction).
		if len(timeline) < len(olds) {
			rejected++
			return nil
		}
		compiled := "Compiled truth (" + time.Now().UTC().Format("2006-01-02") + "):\n- " + strings.Join(truths, "\n- ")
		if _, err := ex.Exec(ctx, `INSERT INTO doc_fragments(id, doc_id, heading_path, position, text, version, hash) VALUES(?,?,?,?,?,?,?)`,
			newFragmentID(), doc.ID, "compiled", 0, compiled, 1, fragmentHash(compiled)); err != nil {
			return err
		}
		for _, line := range timeline {
			t := "timeline: " + line
			if _, err := ex.Exec(ctx, `INSERT INTO doc_fragments(id, doc_id, heading_path, position, text, version, hash) VALUES(?,?,?,?,?,?,?)`,
				newFragmentID(), doc.ID, "timeline", 0, t, 1, fragmentHash(t)); err != nil {
				return err
			}
		}
		for _, o := range olds {
			if _, err := ex.Exec(ctx, `DELETE FROM doc_fragments WHERE id=?`, o.id); err != nil {
				return err
			}
		}
		return nil
	})
	return rejected, err
}

// confirmProposals clears proposal flags backed by repeated
// evidence: a normalized proposal matching an admitted active
// memory in scope (Section 10.2).
func (s *Service) confirmProposals(ctx context.Context, scope string) (int, error) {
	var props []Fragment
	var actives []string
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT f.id, f.text FROM doc_fragments f JOIN identity_docs d ON d.id=f.doc_id WHERE d.scope_id=? AND f.proposal=1`, scope)
		if err != nil {
			return err
		}
		for rows.Next() {
			var f Fragment
			if err := rows.Scan(&f.ID, &f.Text); err != nil {
				rows.Close()
				return err
			}
			props = append(props, f)
		}
		rows.Close()
		mrows, err := q.Query(ctx, `SELECT statement FROM memories WHERE scope_id=? AND status=?`, scope, string(StatusActive))
		if err != nil {
			return err
		}
		defer mrows.Close()
		for mrows.Next() {
			var st string
			if err := mrows.Scan(&st); err != nil {
				return err
			}
			actives = append(actives, normStatement(st))
		}
		return mrows.Err()
	})
	if err != nil {
		return 0, err
	}
	confirmed := 0
	for _, p := range props {
		pn := normStatement(p.Text)
		matches := 0
		for _, a := range actives {
			if a == pn || statementOverlap(pn, a) >= 0.8 {
				matches++
			}
		}
		if matches >= 2 {
			if err := s.ConfirmProposal(ctx, p.ID); err == nil {
				confirmed++
			}
		}
	}
	return confirmed, nil
}

// relationNeeds surfaces unlinked entity pairs recurring in recent
// facts: five or more facts of the last two weeks sharing two
// entities without an edge between them signal a missing relation
// type for the memory-graph-modeling skill (Section 10.4).
func (s *Service) relationNeeds(ctx context.Context, scope string) []string {
	type pair struct{ a, b string }
	counts := map[pair]int{}
	statements := map[pair][]string{}
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		cutoff := s.nowMS() - int64(14*24*time.Hour/time.Millisecond)
		rows, err := q.Query(ctx, `SELECT id, statement, entities_json FROM memories WHERE scope_id=? AND status=? AND recorded_ms>=?`, scope, string(StatusActive), cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, st, ej string
			if err := rows.Scan(&id, &st, &ej); err != nil {
				return err
			}
			ents := decodeStrings(ej)
			for i := 0; i < len(ents); i++ {
				for j := i + 1; j < len(ents); j++ {
					a, b := ents[i], ents[j]
					if a > b {
						a, b = b, a
					}
					p := pair{a, b}
					counts[p]++
					if len(statements[p]) < 3 {
						statements[p] = append(statements[p], st)
					}
				}
			}
		}
		return rows.Err()
	})
	// Drop pairs already linked by any edge.
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for p := range counts {
			var n int
			if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM edges WHERE ((from_id=? AND to_id=?) OR (from_id=? AND to_id=?)) AND scope_id=?`,
				p.a, p.b, p.b, p.a, scope).Scan(&n); err == nil && n > 0 {
				delete(counts, p)
			}
		}
		return nil
	})
	var needs []string
	for p, n := range counts {
		if n >= 5 {
			names := statements[p]
			needs = append(needs, fmt.Sprintf("relation needed: %d recent facts link %s and %s without an edge (e.g. %q); propose a learned type",
				n, p.a, p.b, truncate(firstOf(names), 80)))
		}
	}
	sort.Strings(needs)
	return needs
}

func firstOf(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// ProposeRelationType files a learned-type proposal for user or
// skill confirmation (Section 10.4).
func (s *Service) ProposeRelationType(ctx context.Context, name, description, domain, rng, inverse string) (RelationType, error) {
	r := RelationType{ID: newRelTypeID(), Name: name, Description: description, Domain: domain, Range: rng, Inverse: inverse, Origin: "learned", Status: "proposed"}
	err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO relation_types(id, name, description, domain, range, inverse, origin, use_count, status, scope_id) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.Name, r.Description, r.Domain, r.Range, r.Inverse, r.Origin, 0, r.Status, "general")
		return err
	})
	return r, err
}

// ListRelationTypes returns the registry: seeds plus learned types.
func (s *Service) ListRelationTypes(ctx context.Context) ([]RelationType, error) {
	var out []RelationType
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		out, err = listRelationTypesTx(ctx, q)
		return err
	})
	return out, err
}

// ---- review state persistence ----

func (s *Service) loadReviewState(ctx context.Context, scope string) (ReviewState, error) {
	var st ReviewState
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		st, err = getReviewStateTx(ctx, q, scope)
		return err
	})
	return st, err
}

func (s *Service) saveReviewState(ctx context.Context, st ReviewState) error {
	return s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		return saveReviewStateTx(ctx, ex, st)
	})
}

// countActiveDay bumps the active-day counter once per calendar day
// with user activity (Section 10.6 day counting).
func (s *Service) countActiveDay(ctx context.Context, st *ReviewState, scope string) {
	today := s.now().In(s.location()).Format("2006-01-02")
	lastRun := fromMS(st.LastRun).In(s.location()).Format("2006-01-02")
	if lastRun == today && st.LastRun != 0 {
		return
	}
	active := false
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		dayStart := dayStartMS(s.now().In(s.location()))
		var m, r int64
		_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE scope_id=? AND sent_at_ms>=?`, scope, dayStart).Scan(&m)
		_ = q.QueryRow(ctx, `SELECT COUNT(*) FROM runs WHERE scope_id=? AND created_at>=?`, scope, dayStart).Scan(&r)
		active = m+r > 0
		return nil
	})
	if active {
		st.ActiveDays++
	}
}

func dayStartMS(local time.Time) int64 {
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location()).UnixMilli()
}

// saveReport persists the consultable nightly record (Section 10.6
// step 9).
func (s *Service) saveReport(ctx context.Context, rep ReviewReport) error {
	raw, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	return s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO review_reports(id, scope_id, started_ms, report_json) VALUES(?,?,?,?)`,
			reportPrefix+types.NewULID(), rep.Scope, rep.StartedAt.UnixMilli(), string(raw))
		return err
	})
}

// ListReports returns persisted nightly reports for a scope.
func (s *Service) ListReports(ctx context.Context, scopes []string, limit int) ([]ReviewReport, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 10
	}
	var out []ReviewReport
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT report_json FROM review_reports WHERE scope_id IN (`+placeholders(len(scopes))+`) ORDER BY started_ms DESC LIMIT ?`,
			append(scopeArgs(scopes), limit)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var rep ReviewReport
			if err := json.Unmarshal([]byte(raw), &rep); err != nil {
				continue
			}
			out = append(out, rep)
		}
		return rows.Err()
	})
	return out, err
}

// DrainPending embeds every queued vector with the current provider
// and writes it, deleting the job (Section 10.8). Jobs whose provider
// still fails stay queued with backoff.
func (s *Service) DrainPending(ctx context.Context, limit int) (int, error) {
	if s.curEmbed() == nil || limit <= 0 {
		return 0, nil
	}
	var jobs []pendingItem
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		jobs, err = listPendingTx(ctx, q, s.nowMS(), limit)
		return err
	})
	if err != nil {
		return 0, err
	}
	done := 0
	space := shadowSpace(s)
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			break
		}
		text, ok := s.pendingText(ctx, job)
		if !ok {
			_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
				_, err := ex.Exec(ctx, `DELETE FROM pending_vectors WHERE kind=? AND id=?`, job.kind, job.id)
				return err
			})
			continue
		}
		vecs, err := s.curEmbed().Embed(ctx, []string{text})
		if err != nil || len(vecs) == 0 {
			_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
				_, err := ex.Exec(ctx, `UPDATE pending_vectors SET attempts=attempts+1, next_ms=? WHERE kind=? AND id=?`,
					s.nowMS()+backoffFor(job), job.kind, job.id)
				return err
			})
			continue
		}
		vec := fitDims(vecs[0], s.settings.EmbeddingDimensions)
		err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
			if err := store.PutEmbedding(ctx, ex, job.kind, job.id, space, job.scope, s.settings.EmbeddingDimensions, vec); err != nil {
				return err
			}
			_, err := ex.Exec(ctx, `DELETE FROM pending_vectors WHERE kind=? AND id=?`, job.kind, job.id)
			return err
		})
		if err == nil {
			done++
		}
	}
	return done, nil
}

func backoffFor(job pendingItem) int64 {
	return 5 * 60 * 1000
}

func (s *Service) pendingText(ctx context.Context, job pendingItem) (string, bool) {
	text := ""
	ok := false
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		switch job.kind {
		case "memory":
			if err := q.QueryRow(ctx, `SELECT statement FROM memories WHERE id=?`, job.id).Scan(&text); err == nil {
				ok = true
			}
		case "doc_chunk":
			var stored string
			if err := q.QueryRow(ctx, `SELECT text FROM doc_chunks WHERE id=?`, job.id).Scan(&stored); err == nil {
				t, cx := splitChunkText(stored)
				text = t
				if cx != "" {
					text = cx + "\n" + t
				}
				ok = true
			}
		}
		return nil
	})
	return text, ok
}
