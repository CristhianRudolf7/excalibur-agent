package memory

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// UpsertEntity records or merges an entity by canonical name inside
// the scope, folding aliases (Section 10.4). Explicit mentions link
// deterministically; the model only proposes implicit relations
// during consolidation.
func (s *Service) UpsertEntity(ctx context.Context, scope, typ, canonical string, aliases []string) (Entity, error) {
	scopes, err := s.resolveScopes([]string{scope})
	if err != nil {
		return Entity{}, err
	}
	if canonical == "" {
		return Entity{}, &inputError{"memory: entity canonical name is required"}
	}
	var out Entity
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		e, err := upsertEntityTx(ctx, ex, scopes[0], typ, canonical, aliases)
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	return out, err
}

// ListEntities returns every entity in the scope set.
func (s *Service) ListEntities(ctx context.Context, scopes []string) ([]Entity, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Entity
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		out, err = listEntitiesTx(ctx, q, scopes)
		return err
	})
	return out, err
}

// AddEdge records one memory-layer edge; the relation must be a
// registered type (seed or learned, active) or the structural
// mentions link (Section 10.4).
func (s *Service) AddEdge(ctx context.Context, e Edge) error {
	scopes, err := s.resolveScopes([]string{e.Scope})
	if err != nil {
		return err
	}
	if e.FromID == "" || e.ToID == "" || e.Rel == "" {
		return &inputError{"memory: edge needs from_id, rel and to_id"}
	}
	return s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		if err := ensureSeedRelationTypesTx(ctx, ex); err != nil {
			return err
		}
		if e.Rel != "mentions" {
			var st string
			err := ex.QueryRow(ctx, `SELECT status FROM relation_types WHERE name=? AND scope_id='general'`, e.Rel).Scan(&st)
			if err != nil {
				return fmt.Errorf("%w: relation %q", ErrNotFound, e.Rel)
			}
			if st != "active" {
				return &inputError{fmt.Sprintf("memory: relation %q is not active", e.Rel)}
			}
			if _, err := ex.Exec(ctx, `UPDATE relation_types SET use_count=use_count+1 WHERE name=? AND scope_id='general'`, e.Rel); err != nil {
				return err
			}
		}
		e.ID = newEdgeID()
		e.Scope = scopes[0]
		return insertEdgeTx(ctx, ex, e)
	})
}

// ListMemories returns scoped rows for inspection APIs (memory.query
// provenance, review tooling). Retrieval, not this listing, serves
// the model.
func (s *Service) ListMemories(ctx context.Context, scopes []string, f MemoryFilter, limit int) ([]Memory, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Memory
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		out, err = listMemoriesTx(ctx, q, scopes, f, limit)
		return err
	})
	return out, err
}

// ListEdges returns the memory-layer edges touching entityID, so the
// user can inspect and correct the graph (Sections 4.5, 10.4).
func (s *Service) ListEdges(ctx context.Context, scopes []string, entityID string) ([]Edge, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Edge
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := scopeArgs(scopes)
		rows, err := q.Query(ctx, `SELECT id, from_type, from_id, rel, to_type, to_id, scope_id, provenance_json, valid_from_ms, valid_to_ms FROM edges
			WHERE layer='memory' AND scope_id IN (`+placeholders(len(scopes))+`) AND (from_id=? OR to_id=?) ORDER BY id`,
			append(args, entityID, entityID)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEdge(rows)
			if err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
