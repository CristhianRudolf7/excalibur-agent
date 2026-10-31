package memory

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// StartReindex moves embeddings to a new model: the new space opens
// as active, the previous spaces mark reindexing, and both answer
// until FinishReindex retires the old one (Section 10.8). The
// background pass (ReindexBatch) then backfills row by row.
func (s *Service) StartReindex(ctx context.Context, next Embedder) error {
	if next == nil {
		return &inputError{"memory: reindex needs an embedder"}
	}
	cur, _, _ := s.embedSnapshot()
	var prevModel string
	var prevSpace string
	if cur != nil {
		prevModel = cur.Model()
		prevSpace = spaceID(prevModel, s.settings.EmbeddingDimensions)
	}
	nextSpace := spaceID(next.Model(), s.settings.EmbeddingDimensions)
	if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		if prevModel != "" {
			if _, err := ensureSpaceTx(ctx, ex, prevModel, s.settings.EmbeddingDimensions); err != nil {
				return err
			}
		}
		if _, err := ensureSpaceTx(ctx, ex, next.Model(), s.settings.EmbeddingDimensions); err != nil {
			return err
		}
		if prevSpace != "" && prevSpace != nextSpace {
			_, err := ex.Exec(ctx, `UPDATE embedding_spaces SET status='reindexing' WHERE id=?`, prevSpace)
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	if prevSpace == "" || prevSpace == nextSpace {
		// Same-model call: idempotent, no retiring state.
		s.embeds.Store(&embedSet{cur: next})
		return nil
	}
	s.embeds.Store(&embedSet{cur: next, prev: cur, prevSpace: prevSpace})
	return nil
}

// ReindexBatch backfills up to limit memories and chunks missing
// vectors in the active space, then deletes their pending jobs. It
// runs on the background lane via Background twins; a canceled
// context stops after the current row.
func (s *Service) ReindexBatch(ctx context.Context, limit int) (memories, chunks int, err error) {
	cur := s.curEmbed()
	if cur == nil || limit <= 0 {
		return 0, 0, nil
	}
	space := spaceID(cur.Model(), s.settings.EmbeddingDimensions)
	type job struct {
		kind  string
		id    string
		scope string
		text  string
	}
	var jobs []job
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT m.id, m.statement, m.scope_id FROM memories m
			LEFT JOIN embedding_vectors v ON v.entity_kind='memory' AND v.entity_id=m.id AND v.space_id=?
			WHERE v.entity_id IS NULL LIMIT ?`, space, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var j job
			j.kind = "memory"
			if err := rows.Scan(&j.id, &j.text, &j.scope); err != nil {
				rows.Close()
				return err
			}
			jobs = append(jobs, j)
		}
		rows.Close()
		crows, err := q.Query(ctx, `SELECT c.id, c.text, c.scope_id FROM doc_chunks c
			LEFT JOIN embedding_vectors v ON v.entity_kind='doc_chunk' AND v.entity_id=c.id AND v.space_id=?
			WHERE v.entity_id IS NULL LIMIT ?`, space, limit)
		if err != nil {
			return err
		}
		defer crows.Close()
		for crows.Next() {
			var j job
			var stored string
			j.kind = "doc_chunk"
			if err := crows.Scan(&j.id, &stored, &j.scope); err != nil {
				return err
			}
			t, cx := splitChunkText(stored)
			j.text = t
			if cx != "" {
				j.text = cx + "\n" + t
			}
			jobs = append(jobs, j)
		}
		return crows.Err()
	})
	if err != nil {
		return 0, 0, err
	}
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			break
		}
		vecs, err := cur.Embed(ctx, []string{j.text})
		if err != nil || len(vecs) == 0 {
			continue
		}
		vec := fitDims(vecs[0], s.settings.EmbeddingDimensions)
		werr := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
			if err := store.PutEmbedding(ctx, ex, j.kind, j.id, space, j.scope, s.settings.EmbeddingDimensions, vec); err != nil {
				return err
			}
			_, err := ex.Exec(ctx, `DELETE FROM pending_vectors WHERE kind=? AND id=?`, j.kind, j.id)
			return err
		})
		if werr != nil {
			continue
		}
		if j.kind == "memory" {
			memories++
		} else {
			chunks++
		}
	}
	return memories, chunks, nil
}

// FinishReindex retires the previous space once the new one covers
// every row; callers verify coverage by running ReindexBatch to zero
// first.
func (s *Service) FinishReindex(ctx context.Context) error {
	cur, prev, prevSpace := s.embedSnapshot()
	if prev == nil || prevSpace == "" {
		return nil
	}
	if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE embedding_spaces SET status='retired' WHERE id=?`, prevSpace)
		return err
	}); err != nil {
		return err
	}
	s.embeds.Store(&embedSet{cur: cur})
	return nil
}

// ReindexCoverage reports rows still missing vectors in the active
// space.
func (s *Service) ReindexCoverage(ctx context.Context) (memories, chunks int, err error) {
	cur := s.curEmbed()
	if cur == nil {
		return 0, 0, fmt.Errorf("%w: no embedder", ErrNoEmbedder)
	}
	space := spaceID(cur.Model(), s.settings.EmbeddingDimensions)
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM memories m
			LEFT JOIN embedding_vectors v ON v.entity_kind='memory' AND v.entity_id=m.id AND v.space_id=?
			WHERE v.entity_id IS NULL`, space).Scan(&memories); err != nil {
			return err
		}
		return q.QueryRow(ctx, `SELECT COUNT(*) FROM doc_chunks c
			LEFT JOIN embedding_vectors v ON v.entity_kind='doc_chunk' AND v.entity_id=c.id AND v.space_id=?
			WHERE v.entity_id IS NULL`, space).Scan(&chunks)
	})
	return memories, chunks, err
}
