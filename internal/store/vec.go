package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Vector storage helpers for Section 4.5/10.8: the portable shadow
// table embedding_vectors is the source of truth; vec0 tables index
// it when the module exists. Retrieval ranking belongs to milestone
// H6; H2 guarantees the schema, the round trip and the rebuild.

// vecTables maps shadow entity kinds to their vec0 tables.
var vecTables = map[string]struct {
	table  string
	idCol  string
	vecCol string
}{
	"memory":    {table: "memories_vec", idCol: "memory_id", vecCol: "embedding"},
	"doc_chunk": {table: "doc_chunks_vec", idCol: "chunk_id", vecCol: "embedding"},
}

// VecAvailable reports whether this database has its vec0 tables.
func (db *DB) VecAvailable(ctx context.Context) (bool, error) {
	var n int
	err := db.Read(ctx, func(_ context.Context, q Queryer) error {
		return q.QueryRow(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('memories_vec','doc_chunks_vec')`).Scan(&n)
	})
	return n == 2, err
}

// VecAvailable reports whether the vec0 module created its tables in
// this database.
func VecAvailable(ctx context.Context, q Queryer) (bool, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('memories_vec','doc_chunks_vec')`).Scan(&n)
	return n == 2, err
}

// KNN runs one scoped vec0 nearest-neighbor query through the
// interactive lane and drains it before returning.
func (db *DB) KNN(ctx context.Context, kind string, query []float32, k int, scopes []string) ([]string, []float64, error) {
	var ids []string
	var dists []float64
	err := db.Read(ctx, func(ctx context.Context, q Queryer) error {
		var err error
		ids, dists, err = KNNSearch(ctx, q, kind, query, k, scopes)
		return err
	})
	return ids, dists, err
}

// SerializeFloat32 encodes a vector the way sqlite-vec accepts it
// (little-endian float32).
func SerializeFloat32(v []float32) ([]byte, error) {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}
	return buf, nil
}

// PutEmbedding upserts one shadow vector and mirrors it into vec0
// when available. Both writes happen in the caller's transaction.
func PutEmbedding(ctx context.Context, ex Execer, kind, id, spaceID, scopeID string, dims int, vector []float32) error {
	if _, ok := vecTables[kind]; !ok {
		return fmt.Errorf("store: unknown embedding kind %q", kind)
	}
	if len(vector) != dims {
		return fmt.Errorf("store: vector dims %d != %d", len(vector), dims)
	}
	raw, err := SerializeFloat32(vector)
	if err != nil {
		return err
	}
	if _, err := ex.Exec(ctx, `INSERT INTO embedding_vectors(entity_kind,entity_id,space_id,dims,vector,scope_id)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(entity_kind,entity_id) DO UPDATE SET space_id=excluded.space_id, dims=excluded.dims, vector=excluded.vector, scope_id=excluded.scope_id`,
		kind, id, spaceID, dims, raw, scopeID); err != nil {
		return err
	}
	vt := vecTables[kind]
	// Virtual tables do not support UPSERT: delete then insert.
	del := fmt.Sprintf("DELETE FROM %s WHERE %s=?", vt.table, vt.idCol)
	if _, err := ex.Exec(ctx, del, id); err != nil {
		if isVecMissing(err) {
			return nil
		}
		return err
	}
	stmt := fmt.Sprintf("INSERT INTO %s(%s, %s, scope_id) VALUES(?,?,?)",
		vt.table, vt.idCol, vt.vecCol)
	if _, err := ex.Exec(ctx, stmt, id, raw, scopeID); err != nil {
		if isVecMissing(err) {
			return nil
		}
		return err
	}
	return nil
}

// DeleteEmbedding removes one vector from the shadow and vec0.
func DeleteEmbedding(ctx context.Context, ex Execer, kind, id string) error {
	if _, ok := vecTables[kind]; !ok {
		return fmt.Errorf("store: unknown embedding kind %q", kind)
	}
	if _, err := ex.Exec(ctx, "DELETE FROM embedding_vectors WHERE entity_kind=? AND entity_id=?", kind, id); err != nil {
		return err
	}
	vt := vecTables[kind]
	stmt := fmt.Sprintf("DELETE FROM %s WHERE %s=?", vt.table, vt.idCol)
	if _, err := ex.Exec(ctx, stmt, id); err != nil {
		if isVecMissing(err) {
			return nil
		}
		return err
	}
	return nil
}

// RebuildVec repopulates every present vec0 table from the shadow
// (model change reindexing starts here, Section 10.8).
func RebuildVec(ctx context.Context, ex Execer) error {
	for kind, vt := range vecTables {
		var name string
		row := ex.QueryRow(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name=?", vt.table)
		if err := row.Scan(&name); err != nil {
			continue // table absent: module missing, nothing to index
		}
		del := fmt.Sprintf("DELETE FROM %s", vt.table)
		if _, err := ex.Exec(ctx, del); err != nil {
			if isVecMissing(err) {
				continue
			}
			return err
		}
		ins := fmt.Sprintf("INSERT INTO %s(%s, %s, scope_id) SELECT entity_id, vector, scope_id FROM embedding_vectors WHERE entity_kind=?",
			vt.table, vt.idCol, vt.vecCol)
		if _, err := ex.Exec(ctx, ins, kind); err != nil {
			if isVecMissing(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// KNNSearch runs one scoped vec0 KNN query and returns up to k ids
// with distances, nearest first. Scopes are mandatory and enforced
// in SQL through the vec0 partition key (Section 9.5): there is no
// scopeless vector query. The query vector travels as the serialized
// float32 blob sqlite-vec accepts.
func KNNSearch(ctx context.Context, q Queryer, kind string, query []float32, k int, scopes []string) ([]string, []float64, error) {
	vt, ok := vecTables[kind]
	if !ok {
		return nil, nil, fmt.Errorf("store: unknown embedding kind %q", kind)
	}
	if len(scopes) == 0 {
		return nil, nil, fmt.Errorf("store: vector search requires at least one scope")
	}
	if k < 1 {
		k = 1
	}
	raw, err := SerializeFloat32(query)
	if err != nil {
		return nil, nil, err
	}
	placeholders := ""
	args := []any{raw}
	for i, s := range scopes {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, s)
	}
	stmt := fmt.Sprintf(`SELECT %s, distance FROM %s
		WHERE %s MATCH ? AND scope_id IN (%s)
		ORDER BY distance LIMIT %d`,
		vt.idCol, vt.table, vt.vecCol, placeholders, k)
	rows, err := q.Query(ctx, stmt, args...)
	if err != nil {
		if isVecMissing(err) {
			return nil, nil, fmt.Errorf("store: vector search unavailable: %w", err)
		}
		return nil, nil, err
	}
	defer rows.Close()
	var ids []string
	var dists []float64
	for rows.Next() {
		var id string
		var d float64
		if err := rows.Scan(&id, &d); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		dists = append(dists, d)
	}
	return ids, dists, rows.Err()
}

// isVecMissing reports the absent-module error from any backend.
func isVecMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, sub := range []string{
		"no such module",
		"no such table: vec_neighbors",
		"no such table: memories_vec",
		"no such table: doc_chunks_vec",
	} {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
