package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// EnsureSchema creates the memory-owned auxiliary tables and columns
// idempotently inside the caller's database. Core memory tables come
// from the shared migrations (v3/v4); the auxiliaries below exist so
// milestone H11 never edits shared migration files owned in parallel
// by other milestones (see docs/decisions/0023-memory-schema-seam.md):
//
//   - doc_fragments.provenance and .proposal (profile provenance and
//     unconfirmed proposals, Section 10.2);
//   - memories.short (per-scope M-codes for stable model references);
//   - memories.conversation (candidate visibility inside the proposing
//     conversation or task, Section 10.5);
//   - pending_vectors (embeddings queued while the provider is down,
//     Section 10.8);
//   - documents.title;
//   - doc_fragment_history (every fragment version, Section 10.2);
//   - doc_edit_conflicts (consecutive hash mismatches, Section 10.2);
//   - review_state and review_reports (watermarks, quiet windows and
//     the consultable nightly record, Section 10.6).
func EnsureSchema(ctx context.Context, db *store.DB) error {
	return db.Write(ctx, store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
		return ensureSchemaTx(ctx, ex)
	})
}

func ensureSchemaTx(ctx context.Context, ex store.Execer) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS doc_fragment_history(
			fragment_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			hash TEXT NOT NULL DEFAULT '',
			provenance TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000),
			PRIMARY KEY(fragment_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS doc_edit_conflicts(
			fragment_id TEXT PRIMARY KEY,
			consecutive INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE TABLE IF NOT EXISTS review_state(
			scope_id TEXT PRIMARY KEY,
			watermark_ms INTEGER NOT NULL DEFAULT 0,
			active_days INTEGER NOT NULL DEFAULT 0,
			last_run_ms INTEGER NOT NULL DEFAULT 0,
			window_start_min INTEGER NOT NULL DEFAULT 210,
			window_len_min INTEGER NOT NULL DEFAULT 60
		)`,
		`CREATE TABLE IF NOT EXISTS review_reports(
			id TEXT PRIMARY KEY,
			scope_id TEXT NOT NULL,
			started_ms INTEGER NOT NULL,
			report_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')*1000)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_review_reports_scope ON review_reports(scope_id, started_ms)`,
		`CREATE TABLE IF NOT EXISTS pending_vectors(
			kind TEXT NOT NULL,
			id TEXT NOT NULL,
			scope_id TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			next_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(kind, id)
		)`,
		`CREATE TABLE IF NOT EXISTS citation_runs(
			chunk_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			at_ms INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY(chunk_id, run_id)
		)`,
	} {
		if _, err := ex.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("memory: ensure schema: %w", err)
		}
	}
	for _, col := range []struct{ table, name, ddl string }{
		{"doc_fragments", "provenance", "TEXT NOT NULL DEFAULT ''"},
		{"doc_fragments", "proposal", "INTEGER NOT NULL DEFAULT 0"},
		{"documents", "title", "TEXT NOT NULL DEFAULT ''"},
		{"memories", "short", "TEXT NOT NULL DEFAULT ''"},
		{"memories", "conversation", "TEXT NOT NULL DEFAULT ''"},
	} {
		has, err := hasColumn(ctx, ex, col.table, col.name)
		if err != nil {
			return err
		}
		if !has {
			stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", col.table, col.name, col.ddl)
			if _, err := ex.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("memory: add column %s.%s: %w", col.table, col.name, err)
			}
		}
	}
	return nil
}

func hasColumn(ctx context.Context, ex store.Execer, table, column string) (bool, error) {
	rows, err := ex.Query(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// ---- JSON helpers ----

func encodeJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeSources(raw string) []Source {
	if raw == "" {
		return nil
	}
	var out []Source
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func decodeStrings(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// ---- memories ----

const memoryColumns = `id, scope_id, kind, statement, entities_json, valid_from_ms, valid_to_ms, recorded_ms, expires_at_ms, status, superseded_by, confidence, importance, sources_json, trust, last_used_ms, use_count, short`

func scanMemory(rows store.Rows) (Memory, error) {
	var m Memory
	var kind, status, trust, entitiesRaw, sourcesRaw, short string
	var validFrom, validTo, recorded, expires, lastUsed int64
	var confidence, importance float64
	var useCount int64
	var id, scope, statement, superseded string
	err := rows.Scan(&id, &scope, &kind, &statement, &entitiesRaw, &validFrom, &validTo, &recorded, &expires, &status, &superseded, &confidence, &importance, &sourcesRaw, &trust, &lastUsed, &useCount, &short)
	if err != nil {
		return Memory{}, err
	}
	m = Memory{
		ID: id, Scope: scope, Kind: Kind(kind), Statement: statement,
		Entities:  decodeStrings(entitiesRaw),
		ValidFrom: fromMS(validFrom), ValidTo: fromMS(validTo),
		RecordedAt: fromMS(recorded), ExpiresAt: fromMS(expires),
		Status: Status(status), Superseded: superseded,
		Confidence: confidence, Importance: importance,
		Sources: decodeSources(sourcesRaw), Trust: Trust(trust),
		LastUsedAt: fromMS(lastUsed), UseCount: useCount, ShortCode: short,
	}
	return m, nil
}

// nextShortCode allocates the next per-scope M-code (M12, M40) from
// the shared short_counters table, series "M". Codes never repeat
// after deletion (Section 4.4): the counter only moves forward.
func nextShortCode(ctx context.Context, ex store.Execer, scopeID string) (string, error) {
	var n int64
	err := ex.QueryRow(ctx, `SELECT next_n FROM short_counters WHERE scope_id=? AND series='M'`, scopeID).Scan(&n)
	if err != nil {
		if _, err := ex.Exec(ctx, `INSERT INTO short_counters(scope_id, series, next_n) VALUES(?,'M',2)`, scopeID); err != nil {
			return "", err
		}
		return "M1", nil
	}
	if _, err := ex.Exec(ctx, `UPDATE short_counters SET next_n=next_n+1 WHERE scope_id=? AND series='M'`, scopeID); err != nil {
		return "", err
	}
	return fmt.Sprintf("M%d", n), nil
}

// insertMemoryTx writes one memory row with a fresh per-scope short
// code and mirrors a pending vector when one is supplied.
func insertMemoryTx(ctx context.Context, ex store.Execer, m Memory, vector []float32, spaceID string, dims int) (Memory, error) {
	code, err := nextShortCode(ctx, ex, m.Scope)
	if err != nil {
		return Memory{}, err
	}
	m.ShortCode = code
	if _, err := ex.Exec(ctx, `INSERT INTO memories(id, scope_id, kind, statement, entities_json, valid_from_ms, valid_to_ms, recorded_ms, expires_at_ms, status, superseded_by, confidence, importance, sources_json, trust, last_used_ms, use_count, short)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.Scope, string(m.Kind), m.Statement, encodeJSON(m.Entities),
		ms(m.ValidFrom), ms(m.ValidTo), ms(m.RecordedAt), ms(m.ExpiresAt),
		string(m.Status), m.Superseded, m.Confidence, m.Importance,
		encodeJSON(m.Sources), string(m.Trust), ms(m.LastUsedAt), m.UseCount, m.ShortCode,
	); err != nil {
		return Memory{}, err
	}
	if vector != nil {
		if err := store.PutEmbedding(ctx, ex, "memory", m.ID, spaceID, m.Scope, dims, vector); err != nil {
			return Memory{}, err
		}
	}
	return m, nil
}

// MemoryFilter narrows a scoped listing.
type MemoryFilter struct {
	Kinds      []Kind
	Statuses   []Status
	Entity     string
	RecordedGt int64
}

func listMemoriesTx(ctx context.Context, q store.Queryer, scopes []string, f MemoryFilter, limit int) ([]Memory, error) {
	var sb strings.Builder
	args := []any{}
	sb.WriteString(`SELECT ` + memoryColumns + ` FROM memories WHERE scope_id IN (`)
	ph, args := scopePlaceholders(scopes, args)
	sb.WriteString(ph + `)`)
	if len(f.Kinds) > 0 {
		sb.WriteString(` AND kind IN (`)
		for i, k := range f.Kinds {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
			args = append(args, string(k))
		}
		sb.WriteString(`)`)
	}
	if len(f.Statuses) > 0 {
		sb.WriteString(` AND status IN (`)
		for i, st := range f.Statuses {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
			args = append(args, string(st))
		}
		sb.WriteString(`)`)
	}
	if f.Entity != "" {
		sb.WriteString(` AND entities_json LIKE ?`)
		args = append(args, `%`+f.Entity+`%`)
	}
	if f.RecordedGt > 0 {
		sb.WriteString(` AND recorded_ms > ?`)
		args = append(args, f.RecordedGt)
	}
	sb.WriteString(` ORDER BY recorded_ms DESC`)
	if limit > 0 {
		sb.WriteString(fmt.Sprintf(` LIMIT %d`, limit))
	}
	rows, err := q.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---- entities ----

func scanEntity(rows store.Rows) (Entity, error) {
	var e Entity
	var aliasesRaw string
	if err := rows.Scan(&e.ID, &e.Scope, &e.Type, &e.Canonical, &aliasesRaw); err != nil {
		return Entity{}, err
	}
	e.Aliases = decodeStrings(aliasesRaw)
	return e, nil
}

func listEntitiesTx(ctx context.Context, q store.Queryer, scopes []string) ([]Entity, error) {
	args := []any{}
	ph, args := scopePlaceholders(scopes, args)
	rows, err := q.Query(ctx, `SELECT id, scope_id, type, canonical, aliases_json FROM entities WHERE scope_id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// upsertEntityTx matches by canonical name (case-insensitive) inside
// the scope, merges aliases, or inserts a fresh row.
func upsertEntityTx(ctx context.Context, ex store.Execer, scope, typ, canonical string, aliases []string) (Entity, error) {
	rows, err := ex.Query(ctx, `SELECT id, scope_id, type, canonical, aliases_json FROM entities WHERE scope_id=? AND lower(canonical)=lower(?)`, scope, canonical)
	if err != nil {
		return Entity{}, err
	}
	var found *Entity
	if rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			rows.Close()
			return Entity{}, err
		}
		found = &e
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Entity{}, err
	}
	if found == nil {
		e := Entity{ID: newEntityID(), Scope: scope, Type: typ, Canonical: canonical, Aliases: aliases}
		if _, err := ex.Exec(ctx, `INSERT INTO entities(id, scope_id, type, canonical, aliases_json) VALUES(?,?,?,?,?)`,
			e.ID, e.Scope, e.Type, e.Canonical, encodeJSON(e.Aliases)); err != nil {
			return Entity{}, err
		}
		return e, nil
	}
	merged := mergeAliases(found.Aliases, append([]string{canonical}, aliases...))
	if _, err := ex.Exec(ctx, `UPDATE entities SET aliases_json=?, updated_at=strftime('%s','now')*1000 WHERE id=?`, encodeJSON(merged), found.ID); err != nil {
		return Entity{}, err
	}
	found.Aliases = merged
	return *found, nil
}

func mergeAliases(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string(nil), a...), b...) {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// ---- relation types ----

func scanRelationType(rows store.Rows) (RelationType, error) {
	var r RelationType
	var useCount int64
	if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Domain, &r.Range, &r.Inverse, &r.Origin, &useCount, &r.Status); err != nil {
		return RelationType{}, err
	}
	r.UseCount = useCount
	return r, nil
}

// seedRelationTypes are the general starting vocabulary (Section
// 10.4). They live in the general scope: one registry per user.
var seedRelationTypes = []RelationType{
	{Name: "works_at", Description: "person is employed by or works at organization", Domain: "person", Range: "organization", Inverse: "employs"},
	{Name: "participates_in", Description: "entity takes part in an event or project", Domain: "person", Range: "project", Inverse: "has_participant"},
	{Name: "prefers", Description: "user preference for an option or style", Domain: "person", Range: "concept", Inverse: "preferred_by"},
	{Name: "depends_on", Description: "task, module or decision depends on another", Domain: "concept", Range: "concept", Inverse: "blocks"},
	{Name: "decided", Description: "person or group made a decision", Domain: "person", Range: "concept", Inverse: "decided_by"},
	{Name: "part_of", Description: "component belongs to a larger whole", Domain: "concept", Range: "concept", Inverse: "contains"},
	{Name: "cites", Description: "document or memory cites another source", Domain: "document", Range: "document", Inverse: "cited_by"},
	// mentions is the structural link between a memory and the
	// entities it references (Section 10.6 step 6).
	{Name: "mentions", Description: "memory references an entity", Domain: "memory", Range: "entity", Inverse: "mentioned_in"},
}

func ensureSeedRelationTypesTx(ctx context.Context, ex store.Execer) error {
	for _, s := range seedRelationTypes {
		var n int
		if err := ex.QueryRow(ctx, `SELECT COUNT(*) FROM relation_types WHERE name=? AND scope_id='general'`, s.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if _, err := ex.Exec(ctx, `INSERT INTO relation_types(id, name, description, domain, range, inverse, origin, use_count, status, scope_id) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			newRelTypeID(), s.Name, s.Description, s.Domain, s.Range, s.Inverse, "seed", 0, "active", "general"); err != nil {
			return err
		}
	}
	return nil
}

func listRelationTypesTx(ctx context.Context, q store.Queryer) ([]RelationType, error) {
	rows, err := q.Query(ctx, `SELECT id, name, description, domain, range, inverse, origin, use_count, status FROM relation_types WHERE scope_id='general' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelationType
	for rows.Next() {
		r, err := scanRelationType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- edges ----

func scanEdge(rows store.Rows) (Edge, error) {
	var e Edge
	var provRaw string
	var vf, vt int64
	if err := rows.Scan(&e.ID, &e.FromType, &e.FromID, &e.Rel, &e.ToType, &e.ToID, &e.Scope, &provRaw, &vf, &vt); err != nil {
		return Edge{}, err
	}
	e.ValidFrom, e.ValidTo = fromMS(vf), fromMS(vt)
	e.Provenance = map[string]string{}
	if provRaw != "" {
		_ = json.Unmarshal([]byte(provRaw), &e.Provenance)
	}
	return e, nil
}

func insertEdgeTx(ctx context.Context, ex store.Execer, e Edge) error {
	_, err := ex.Exec(ctx, `INSERT INTO edges(id, layer, from_type, from_id, rel, to_type, to_id, scope_id, provenance_json, valid_from_ms, valid_to_ms)
		VALUES(?,'memory',?,?,?,?,?,?,?,?,?)`,
		e.ID, e.FromType, e.FromID, e.Rel, e.ToType, e.ToID, e.Scope, encodeJSON(e.Provenance), ms(e.ValidFrom), ms(e.ValidTo))
	return err
}

// neighborIDsTx returns memory ids linked to seeds within depth
// hops (Section 10.7 relational list, 1-2 hops).
func neighborIDsTx(ctx context.Context, rd store.Queryer, scopes []string, seeds []string, depth int) ([]string, error) {
	if len(seeds) == 0 || depth < 1 {
		return nil, nil
	}
	args := []any{}
	scopePH, args := scopePlaceholders(scopes, args)
	seen := map[string]bool{}
	var out []string
	frontier := append([]string(nil), seeds...)
	for d := 0; d < depth; d++ {
		if len(frontier) == 0 {
			break
		}
		ph := make([]string, 0, len(frontier))
		fargs := append([]any{}, args...)
		for _, f := range frontier {
			ph = append(ph, "?")
			fargs = append(fargs, f)
		}
		inList := strings.Join(ph, ",")
		rows, err := rd.Query(ctx, `SELECT DISTINCT from_id, to_id FROM edges WHERE layer='memory' AND scope_id IN (`+scopePH+`) AND (from_id IN (`+inList+`) OR to_id IN (`+inList+`))`,
			append(append([]any{}, fargs...), fargs[len(args):]...)...)
		if err != nil {
			return nil, err
		}
		var next []string
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				rows.Close()
				return nil, err
			}
			for _, id := range []string{a, b} {
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				out = append(out, id)
				next = append(next, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		frontier = next
	}
	return out, nil
}

// ---- identity docs ----

// ensureDocTx returns the (scope, kind) document, creating it with
// the notebook's per-scope sections when absent.
func ensureDocTx(ctx context.Context, ex store.Execer, scope, kind, title string) (IdentityDoc, error) {
	var id string
	err := ex.QueryRow(ctx, `SELECT id FROM identity_docs WHERE scope_id=? AND kind=?`, scope, kind).Scan(&id)
	if err == nil {
		return getDocTx(ctx, ex, id)
	}
	doc := IdentityDoc{ID: newIdentityID(), Scope: scope, Kind: kind, Title: title}
	if _, err := ex.Exec(ctx, `INSERT INTO identity_docs(id, scope_id, kind, title) VALUES(?,?,?,?)`, doc.ID, doc.Scope, doc.Kind, doc.Title); err != nil {
		return IdentityDoc{}, err
	}
	return doc, nil
}

func getDocTx(ctx context.Context, ex store.Execer, id string) (IdentityDoc, error) {
	var doc IdentityDoc
	if err := ex.QueryRow(ctx, `SELECT id, scope_id, kind, title FROM identity_docs WHERE id=?`, id).Scan(&doc.ID, &doc.Scope, &doc.Kind, &doc.Title); err != nil {
		return IdentityDoc{}, fmt.Errorf("%w: document %q", ErrNotFound, id)
	}
	frags, err := listFragmentsTx(ctx, ex, id)
	if err != nil {
		return IdentityDoc{}, err
	}
	doc.Frags = frags
	return doc, nil
}

func listFragmentsTx(ctx context.Context, ex store.Execer, docID string) ([]Fragment, error) {
	rows, err := ex.Query(ctx, `SELECT id, doc_id, heading_path, position, text, version, hash, provenance, proposal FROM doc_fragments WHERE doc_id=? ORDER BY position`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Fragment
	for rows.Next() {
		var f Fragment
		var proposal int
		if err := rows.Scan(&f.ID, &f.Doc, &f.Heading, &f.Position, &f.Text, &f.Version, &f.Hash, &f.Provenance, &proposal); err != nil {
			return nil, err
		}
		f.Proposal = proposal != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---- library ----

func getLibraryDocTx(ctx context.Context, q store.Queryer, id string) (LibraryDoc, error) {
	var d LibraryDoc
	if err := q.QueryRow(ctx, `SELECT id, scope_id, version, blob_digest, mime, origin, title, format, paginated FROM documents WHERE id=?`, id).
		Scan(&d.ID, &d.Scope, &d.Version, &d.Digest, &d.MIME, &d.Origin, &d.Title, &d.Format, &d.Paginated); err != nil {
		return LibraryDoc{}, fmt.Errorf("%w: document %q", ErrNotFound, id)
	}
	return d, nil
}

func scanChunk(rows store.Rows) (Chunk, error) {
	var c Chunk
	if err := rows.Scan(&c.ID, &c.Doc, &c.Version, &c.Page, &c.Paragraph, &c.CharStart, &c.CharEnd, &c.Section, &c.Text, &c.PageLabel, &c.Anchor); err != nil {
		return Chunk{}, err
	}
	return c, nil
}

func listChunksTx(ctx context.Context, q store.Queryer, docID string, version int) ([]Chunk, error) {
	rows, err := q.Query(ctx, `SELECT id, doc_id, version, page, paragraph, char_start, char_end, section_path, text, page_label, anchor FROM doc_chunks WHERE doc_id=? AND version=? ORDER BY page, paragraph`, docID, version)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		c, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- review state ----

// ReviewState is the persisted per-scope consolidation cursor.
type ReviewState struct {
	Scope        string
	Watermark    int64
	ActiveDays   int
	LastRun      int64
	WindowStart  int
	WindowLength int
}

func getReviewStateTx(ctx context.Context, q store.Queryer, scope string) (ReviewState, error) {
	st := ReviewState{Scope: scope, WindowStart: 210, WindowLength: 60}
	err := q.QueryRow(ctx, `SELECT watermark_ms, active_days, last_run_ms, window_start_min, window_len_min FROM review_state WHERE scope_id=?`, scope).
		Scan(&st.Watermark, &st.ActiveDays, &st.LastRun, &st.WindowStart, &st.WindowLength)
	if err != nil {
		return st, nil
	}
	return st, nil
}

func saveReviewStateTx(ctx context.Context, ex store.Execer, st ReviewState) error {
	_, err := ex.Exec(ctx, `INSERT INTO review_state(scope_id, watermark_ms, active_days, last_run_ms, window_start_min, window_len_min)
		VALUES(?,?,?,?,?,?) ON CONFLICT(scope_id) DO UPDATE SET watermark_ms=excluded.watermark_ms, active_days=excluded.active_days, last_run_ms=excluded.last_run_ms, window_start_min=excluded.window_start_min, window_len_min=excluded.window_len_min`,
		st.Scope, st.Watermark, st.ActiveDays, st.LastRun, st.WindowStart, st.WindowLength)
	return err
}
