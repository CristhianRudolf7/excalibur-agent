package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Logical document kinds (Section 10.2): the agent profile, the user
// profile and the notebook. Personas are agent-profile variants
// addressed as persona:<name>; they change style and identity, never
// the user's memory.
const (
	DocAgent    = "agent"
	DocUser     = "user"
	DocNotebook = "notebook"
)

// maxEditConflicts is the Appendix B fingerprint-mismatch budget
// before escalation (Section 13.6 sets 3 for file edits; identity
// documents escalate after 2 consecutive mismatches, Section 10.2).
const maxEditConflicts = 2

// fragmentHash is the edit guard fingerprint of one fragment.
func fragmentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// HashMismatchError carries the current fragment so the agent redoes
// the edit on top of it (Section 10.2).
type HashMismatchError struct {
	Current Fragment
	// Consecutive counts this run of mismatches on the fragment.
	Consecutive int
}

func (e *HashMismatchError) Error() string {
	return fmt.Sprintf("%v: fragment %q changed (version %d); redo the edit on the current text",
		ErrHashMismatch, e.Current.ID, e.Current.Version)
}

func (e *HashMismatchError) Unwrap() error { return ErrHashMismatch }

// ResolveDoc returns the document id for scope and kind, creating
// the document on first use.
func (s *Service) ResolveDoc(ctx context.Context, scope, kind string) (string, error) {
	scopes, err := s.resolveScopes([]string{scope})
	if err != nil {
		return "", err
	}
	var id string
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		doc, err := ensureDocTx(ctx, ex, scopes[0], kind, docTitle(kind))
		if err != nil {
			return err
		}
		id = doc.ID
		return nil
	})
	return id, err
}

func docTitle(kind string) string {
	switch {
	case kind == DocAgent:
		return "Agent profile"
	case kind == DocUser:
		return "User profile"
	case kind == DocNotebook:
		return "Notebook"
	case strings.HasPrefix(kind, "persona:"):
		return "Persona " + strings.TrimPrefix(kind, "persona:")
	default:
		return kind
	}
}

// ReadDoc implements doc_read: empty selector returns the heading
// index, section:<path> returns one section, search:<text> returns
// matching fragments, all without loading the whole document.
func (s *Service) ReadDoc(ctx context.Context, scopes []string, docID, selector string) (IdentityDoc, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return IdentityDoc{}, err
	}
	var doc IdentityDoc
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var scopeID, kind, title string
		if err := q.QueryRow(ctx, `SELECT scope_id, kind, title FROM identity_docs WHERE id=?`, docID).
			Scan(&scopeID, &kind, &title); err != nil {
			return fmt.Errorf("%w: document %q", ErrNotFound, docID)
		}
		allowed := false
		for _, sc := range scopes {
			if sc == scopeID {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q", ErrScopeDenied, scopeID)
		}
		doc = IdentityDoc{ID: docID, Scope: scopeID, Kind: kind, Title: title}
		return nil
	})
	if err != nil {
		return IdentityDoc{}, err
	}
	sel, arg := splitSelector(selector)
	var frags []Fragment
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		frags, err = selectFragments(ctx, q, docID, sel, arg)
		return err
	})
	if err != nil {
		return IdentityDoc{}, err
	}
	doc.Frags = frags
	return doc, nil
}

func splitSelector(selector string) (string, string) {
	if selector == "" {
		return "index", ""
	}
	if strings.HasPrefix(selector, "section:") {
		return "section", strings.TrimPrefix(selector, "section:")
	}
	if strings.HasPrefix(selector, "search:") {
		return "search", strings.TrimPrefix(selector, "search:")
	}
	return "section", selector
}

func selectFragments(ctx context.Context, q store.Queryer, docID, sel, arg string) ([]Fragment, error) {
	switch sel {
	case "index":
		rows, err := q.Query(ctx, `SELECT DISTINCT heading_path FROM doc_fragments WHERE doc_id=? ORDER BY position`, docID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Fragment
		pos := 0
		for rows.Next() {
			var h string
			if err := rows.Scan(&h); err != nil {
				return nil, err
			}
			out = append(out, Fragment{Doc: docID, Heading: h, Position: pos})
			pos++
		}
		return out, rows.Err()
	case "section":
		rows, err := q.Query(ctx, `SELECT id, doc_id, heading_path, position, text, version, hash, provenance, proposal FROM doc_fragments WHERE doc_id=? AND heading_path=? ORDER BY position`, docID, arg)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanFragments(rows)
	case "search":
		rows, err := q.Query(ctx, `SELECT f.id, f.doc_id, f.heading_path, f.position, f.text, f.version, f.hash, f.provenance, f.proposal
			FROM doc_fragments_fts x JOIN doc_fragments f ON f.id = x.id WHERE f.doc_id=? AND doc_fragments_fts MATCH ? ORDER BY bm25(doc_fragments_fts) LIMIT 20`, docID, ftsEscape(arg)+`*`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return scanFragments(rows)
	default:
		return nil, fmt.Errorf("memory: unknown doc selector %q", sel)
	}
}

func scanFragments(rows store.Rows) ([]Fragment, error) {
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

// EditFragment implements doc_edit with the hash guard (Section
// 10.2): a stale expected_hash applies nothing and returns the
// current fragment; after two consecutive mismatches the run must
// escalate instead of insisting. Every version is preserved for
// history and revert.
func (s *Service) EditFragment(ctx context.Context, scopes []string, edit DocEdit) (Fragment, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return Fragment{}, err
	}
	if edit.Doc == "" || edit.FragmentID == "" {
		return Fragment{}, &inputError{"memory: doc and fragment_id are required"}
	}
	switch edit.Op {
	case OpReplace, OpInsert, OpDelete:
	default:
		return Fragment{}, &inputError{fmt.Sprintf("memory: unknown doc op %q", string(edit.Op))}
	}
	isProfile, err := s.docIsProfile(ctx, scopes, edit.Doc)
	if err != nil {
		return Fragment{}, err
	}
	if isProfile && edit.Op != OpDelete && strings.TrimSpace(edit.Provenance) == "" && edit.Text != "" {
		// Profile identity and preference changes always carry the
		// motivating user message (Section 10.2).
		return Fragment{}, ErrNoSource
	}
	var result Fragment
	var guarded error
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		var cur Fragment
		if err := getFragmentTx(ctx, ex, edit.FragmentID, &cur); err != nil {
			return err
		}
		if cur.Doc != edit.Doc {
			return fmt.Errorf("%w: fragment %q", ErrNotFound, edit.FragmentID)
		}
		if edit.ExpectedHash != cur.Hash {
			// The guard trip must persist even though the edit
			// itself does not: returning an error here would roll
			// the counter increment back to its savepoint, so the
			// outcome travels out-of-band and the transaction
			// commits.
			n, err := bumpConflictTx(ctx, ex, cur.ID)
			if err != nil {
				return err
			}
			if n >= maxEditConflicts {
				guarded = fmt.Errorf("%w: fragment %q mismatch %d in a row", ErrEscalated, cur.ID, n)
			} else {
				guarded = &HashMismatchError{Current: cur, Consecutive: n}
			}
			return nil
		}
		if err := resetConflictTx(ctx, ex, cur.ID); err != nil {
			return err
		}
		proposal := isProfile && !edit.Explicit && edit.Op != OpDelete
		switch edit.Op {
		case OpReplace:
			cur.Text = edit.Text
			cur.Hash = fragmentHash(edit.Text)
			cur.Version++
			cur.Provenance = edit.Provenance
			cur.Proposal = proposal
			if _, err := ex.Exec(ctx, `UPDATE doc_fragments SET text=?, version=?, hash=?, provenance=?, proposal=? WHERE id=?`,
				cur.Text, cur.Version, cur.Hash, cur.Provenance, boolInt(cur.Proposal), cur.ID); err != nil {
				return err
			}
		case OpInsert:
			pos := edit.Position
			if pos <= 0 {
				pos = cur.Position + 1
			}
			if _, err := ex.Exec(ctx, `UPDATE doc_fragments SET position=position+1 WHERE doc_id=? AND position>=?`, cur.Doc, pos); err != nil {
				return err
			}
			cur = Fragment{
				ID: newFragmentID(), Doc: cur.Doc,
				Heading:  orDefault(edit.Heading, cur.Heading),
				Position: pos, Text: edit.Text, Version: 1,
				Hash:       fragmentHash(edit.Text),
				Provenance: edit.Provenance, Proposal: proposal,
			}
			if _, err := ex.Exec(ctx, `INSERT INTO doc_fragments(id, doc_id, heading_path, position, text, version, hash, provenance, proposal) VALUES(?,?,?,?,?,?,?,?,?)`,
				cur.ID, cur.Doc, cur.Heading, cur.Position, cur.Text, cur.Version, cur.Hash, cur.Provenance, boolInt(cur.Proposal)); err != nil {
				return err
			}
		case OpDelete:
			if _, err := ex.Exec(ctx, `DELETE FROM doc_fragments WHERE id=?`, cur.ID); err != nil {
				return err
			}
			result = cur
			return nil
		}
		if err := appendHistoryTx(ctx, ex, cur); err != nil {
			return err
		}
		result = cur
		return nil
	})
	if err != nil {
		return Fragment{}, err
	}
	return result, guarded
}

// AppendFragment adds a new fragment at the end of a document,
// recording its first version for history and revert.
func (s *Service) AppendFragment(ctx context.Context, scopes []string, docID, heading, text, provenance string) (Fragment, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return Fragment{}, err
	}
	var out Fragment
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		var scopeID string
		if err := ex.QueryRow(ctx, `SELECT scope_id FROM identity_docs WHERE id=?`, docID).Scan(&scopeID); err != nil {
			return fmt.Errorf("%w: document %q", ErrNotFound, docID)
		}
		allowed := false
		for _, sc := range scopes {
			if sc == scopeID {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q", ErrScopeDenied, scopeID)
		}
		var maxPos int
		_ = ex.QueryRow(ctx, `SELECT COALESCE(MAX(position),-1) FROM doc_fragments WHERE doc_id=?`, docID).Scan(&maxPos)
		out = Fragment{
			ID: newFragmentID(), Doc: docID, Heading: heading,
			Position: maxPos + 1, Text: text, Version: 1,
			Hash: fragmentHash(text), Provenance: provenance,
		}
		if _, err := ex.Exec(ctx, `INSERT INTO doc_fragments(id, doc_id, heading_path, position, text, version, hash, provenance) VALUES(?,?,?,?,?,?,?,?)`,
			out.ID, out.Doc, out.Heading, out.Position, out.Text, out.Version, out.Hash, out.Provenance); err != nil {
			return err
		}
		return appendHistoryTx(ctx, ex, out)
	})
	return out, err
}

func (s *Service) docIsProfile(ctx context.Context, scopes []string, docID string) (bool, error) {
	profile := false
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var scopeID, kind string
		if err := q.QueryRow(ctx, `SELECT scope_id, kind FROM identity_docs WHERE id=?`, docID).Scan(&scopeID, &kind); err != nil {
			return fmt.Errorf("%w: document %q", ErrNotFound, docID)
		}
		allowed := false
		for _, sc := range scopes {
			if sc == scopeID {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q", ErrScopeDenied, scopeID)
		}
		profile = kind == DocAgent || kind == DocUser || strings.HasPrefix(kind, "persona:")
		return nil
	})
	return profile, err
}

func getFragmentTx(ctx context.Context, ex store.Execer, id string, out *Fragment) error {
	var proposal int
	err := ex.QueryRow(ctx, `SELECT id, doc_id, heading_path, position, text, version, hash, provenance, proposal FROM doc_fragments WHERE id=?`, id).
		Scan(&out.ID, &out.Doc, &out.Heading, &out.Position, &out.Text, &out.Version, &out.Hash, &out.Provenance, &proposal)
	if err != nil {
		return fmt.Errorf("%w: fragment %q", ErrNotFound, id)
	}
	out.Proposal = proposal != 0
	return nil
}

func appendHistoryTx(ctx context.Context, ex store.Execer, f Fragment) error {
	_, err := ex.Exec(ctx, `INSERT INTO doc_fragment_history(fragment_id, version, text, hash, provenance) VALUES(?,?,?,?,?)
		ON CONFLICT(fragment_id, version) DO UPDATE SET text=excluded.text, hash=excluded.hash, provenance=excluded.provenance`,
		f.ID, f.Version, f.Text, f.Hash, f.Provenance)
	return err
}

func bumpConflictTx(ctx context.Context, ex store.Execer, fragmentID string) (int, error) {
	_, err := ex.Exec(ctx, `INSERT INTO doc_edit_conflicts(fragment_id, consecutive, updated_at) VALUES(?,1,strftime('%s','now')*1000)
		ON CONFLICT(fragment_id) DO UPDATE SET consecutive=consecutive+1, updated_at=strftime('%s','now')*1000`, fragmentID)
	if err != nil {
		return 0, err
	}
	var n int
	if err := ex.QueryRow(ctx, `SELECT consecutive FROM doc_edit_conflicts WHERE fragment_id=?`, fragmentID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func resetConflictTx(ctx context.Context, ex store.Execer, fragmentID string) error {
	_, err := ex.Exec(ctx, `DELETE FROM doc_edit_conflicts WHERE fragment_id=?`, fragmentID)
	return err
}

// FragmentHistory returns every preserved version for history and
// revert (Section 10.2).
func (s *Service) FragmentHistory(ctx context.Context, scopes []string, docID, fragmentID string) ([]Fragment, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Fragment
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var scopeID string
		if err := q.QueryRow(ctx, `SELECT scope_id FROM identity_docs WHERE id=?`, docID).Scan(&scopeID); err != nil {
			return fmt.Errorf("%w: document %q", ErrNotFound, docID)
		}
		allowed := false
		for _, sc := range scopes {
			if sc == scopeID {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q", ErrScopeDenied, scopeID)
		}
		rows, err := q.Query(ctx, `SELECT fragment_id, version, text, hash, provenance FROM doc_fragment_history WHERE fragment_id=? ORDER BY version`, fragmentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f Fragment
			var fid string
			if err := rows.Scan(&fid, &f.Version, &f.Text, &f.Hash, &f.Provenance); err != nil {
				return err
			}
			f.ID, f.Doc = fid, docID
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}

// RevertFragment restores an older version as a new guarded version.
func (s *Service) RevertFragment(ctx context.Context, scopes []string, docID, fragmentID string, version int, expectedHash, provenance string) (Fragment, error) {
	hist, err := s.FragmentHistory(ctx, scopes, docID, fragmentID)
	if err != nil {
		return Fragment{}, err
	}
	for _, h := range hist {
		if h.Version == version {
			return s.EditFragment(ctx, scopes, DocEdit{
				Doc: docID, FragmentID: fragmentID, ExpectedHash: expectedHash,
				Op: OpReplace, Text: h.Text, Provenance: provenance, Explicit: true,
			})
		}
	}
	return Fragment{}, fmt.Errorf("%w: version %d of fragment %q", ErrNotFound, version, fragmentID)
}

// ConfirmProposal clears the proposal flag once consolidation finds
// repeated evidence (Section 10.2).
func (s *Service) ConfirmProposal(ctx context.Context, fragmentID string) error {
	return s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE doc_fragments SET proposal=0 WHERE id=?`, fragmentID)
		return err
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
