package memory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func libraryEnabled() bool {
	switch strings.ToLower(os.Getenv("EXCALIBUR_LIBRARY")) {
	case "0", "false", "off":
		return false
	}
	return true
}

const documentColumns = `id,title,source_name,mime,format,blob_digest,scope_id,size,version,status,progress_pages,total_pages,page_count,paginated,uploaded_at,updated_at,error,failure_stage,warnings_json,summary`

type documentScanner interface{ Scan(...any) error }

func scanDocument(row documentScanner) (DocumentInfo, error) {
	var d DocumentInfo
	var uploaded, updated int64
	var warnings string
	err := row.Scan(&d.ID, &d.Title, &d.SourceName, &d.MIME, &d.Format, &d.BlobDigest, &d.ScopeID, &d.Size, &d.Version, &d.Status, &d.ProgressPages, &d.TotalPages, &d.PageCount, &d.Paginated, &uploaded, &updated, &d.Error, &d.FailureStage, &warnings, &d.Summary)
	d.UploadedAt = fromMS(uploaded)
	d.UpdatedAt = fromMS(updated)
	d.Warnings = []string{}
	if err == nil {
		err = json.Unmarshal([]byte(warnings), &d.Warnings)
	}
	return d, err
}

func (s *Service) CreatePendingDocument(ctx context.Context, scope string, meta DocMeta) (DocumentInfo, bool, error) {
	if !libraryEnabled() {
		return DocumentInfo{}, false, &inputError{"memory: library disabled"}
	}
	scopes, err := s.resolveScopes([]string{scope})
	if err != nil {
		return DocumentInfo{}, false, err
	}
	scope = scopes[0]
	if meta.Size < 0 || strings.TrimSpace(meta.BlobDigest) == "" {
		return DocumentInfo{}, false, &inputError{"memory: blob digest and nonnegative size required"}
	}
	var d DocumentInfo
	existing := false
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		rows, err := ex.Query(ctx, `SELECT `+documentColumns+` FROM documents WHERE scope_id=? AND blob_digest=? ORDER BY uploaded_at DESC,id LIMIT 1`, scope, meta.BlobDigest)
		if err != nil {
			return err
		}
		if rows.Next() {
			d, err = scanDocument(rows)
			rows.Close()
			existing = true
			return err
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		id := meta.ID
		if id == "" {
			id = newDocID()
		} else {
			var storedScope string
			if err := ex.QueryRow(ctx, `SELECT scope_id FROM documents WHERE id=?`, id).Scan(&storedScope); err != nil {
				return fmt.Errorf("%w: document %q", ErrNotFound, id)
			}
			if storedScope != scope {
				return ErrScopeDenied
			}
		}
		title := meta.Title
		if title == "" {
			title = meta.SourceName
		}
		if meta.ID == "" {
			_, err = ex.Exec(ctx, `INSERT INTO documents(id,scope_id,title,source_name,mime,format,size,blob_digest,origin,status,uploaded_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'pending',?,?)`, id, scope, title, meta.SourceName, meta.MIME, meta.Format, meta.Size, meta.BlobDigest, meta.Origin, s.nowMS(), s.nowMS())
		} else {
			_, err = ex.Exec(ctx, `UPDATE documents SET title=?,source_name=?,mime=?,format=?,size=?,blob_digest=?,origin=?,status='pending',progress_pages=0,total_pages=0,failure_stage='',error='',uploaded_at=?,updated_at=? WHERE id=?`, title, meta.SourceName, meta.MIME, meta.Format, meta.Size, meta.BlobDigest, meta.Origin, s.nowMS(), s.nowMS(), id)
		}
		if err != nil {
			return err
		}
		d, err = scanDocument(ex.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=?`, id))
		return err
	})
	return d, existing, err
}

func (s *Service) SetDocumentProgress(ctx context.Context, id, status string, progress, total int, stage, errText string) error {
	switch status {
	case "pending", "processing", "ready", "partial", "failed", "canceled":
	default:
		return &inputError{"memory: invalid document status"}
	}
	if progress < 0 || total < 0 || progress > total {
		return &inputError{"memory: invalid page progress"}
	}
	switch stage {
	case "", "read", "extract", "ocr", "chunk", "embed", "store":
	default:
		return &inputError{"memory: invalid failure stage"}
	}
	return s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		var scope string
		if err := ex.QueryRow(ctx, `SELECT scope_id FROM documents WHERE id=?`, id).Scan(&scope); err != nil {
			return ErrNotFound
		}
		if _, err := s.resolveScopes([]string{scope}); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `UPDATE documents SET status=?,progress_pages=?,total_pages=?,failure_stage=?,error=?,updated_at=? WHERE id=?`, status, progress, total, stage, errText, s.nowMS(), id)
		return err
	})
}

func (s *Service) GetDocument(ctx context.Context, scopes []string, id string) (DocumentInfo, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return DocumentInfo{}, err
	}
	var d DocumentInfo
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		d, err = scanDocument(q.QueryRow(ctx, `SELECT `+documentColumns+` FROM documents WHERE id=?`, id))
		if err != nil {
			return fmt.Errorf("%w: document %q", ErrNotFound, id)
		}
		for _, scope := range scopes {
			if scope == d.ScopeID {
				return nil
			}
		}
		return ErrScopeDenied
	})
	return d, err
}

func (s *Service) ListDocuments(ctx context.Context, scopes []string, in ListQuery) ([]DocumentInfo, string, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	where := `scope_id IN (` + placeholders(len(scopes)) + `)`
	args := scopeArgs(scopes)
	if in.Status != "" {
		where += ` AND status=?`
		args = append(args, in.Status)
	}
	if in.Query != "" {
		like := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(in.Query) + "%"
		where += ` AND (title LIKE ? ESCAPE '\' OR source_name LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\'`
		args = append(args, like, like, like)
		if match := ftsOr(in.Query, quotedPhrases(in.Query)); match != "" {
			where += ` OR id IN (SELECT c.doc_id FROM doc_chunks_fts f JOIN doc_chunks c ON c.id=f.id WHERE doc_chunks_fts MATCH ? AND c.version=documents.version)`
			args = append(args, match)
		}
		where += `)`
	}
	if in.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(in.Cursor)
		if e != nil {
			return nil, "", &inputError{"memory: invalid cursor"}
		}
		parts := strings.SplitN(string(raw), "/", 2)
		if len(parts) != 2 {
			return nil, "", &inputError{"memory: invalid cursor"}
		}
		ms, e := strconv.ParseInt(parts[0], 10, 64)
		if e != nil {
			return nil, "", &inputError{"memory: invalid cursor"}
		}
		where += ` AND (uploaded_at<? OR (uploaded_at=? AND id<?))`
		args = append(args, ms, ms, parts[1])
	}
	out := []DocumentInfo{}
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, e := q.Query(ctx, `SELECT `+documentColumns+` FROM documents WHERE `+where+` ORDER BY uploaded_at DESC,id DESC LIMIT ?`, append(args, limit+1)...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			d, e := scanDocument(rows)
			if e != nil {
				return e
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d/%s", last.UploadedAt.UnixMilli(), last.ID)))
	}
	return out, next, err
}

func (s *Service) DocumentPages(ctx context.Context, scopes []string, id string, version, from, to int) ([]docextract.Page, error) {
	d, err := s.GetDocument(ctx, scopes, id)
	if err != nil {
		return nil, err
	}
	if version == 0 {
		version = d.Version
	}
	if version < 1 || from < 1 || to < from || to-from >= 20 {
		return nil, &inputError{"memory: pages must be a range of at most 20"}
	}
	out := []docextract.Page{}
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, e := q.Query(ctx, `SELECT number,label,title,width,height,method,quality,blocks_json,warnings_json FROM doc_pages WHERE doc_id=? AND version=? AND number BETWEEN ? AND ? ORDER BY number`, id, version, from, to)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var p docextract.Page
			var blocks, warnings string
			if e := rows.Scan(&p.Number, &p.Label, &p.Title, &p.Width, &p.Height, &p.Method, &p.Quality, &blocks, &warnings); e != nil {
				return e
			}
			if e := json.Unmarshal([]byte(blocks), &p.Blocks); e != nil {
				return e
			}
			if e := json.Unmarshal([]byte(warnings), &p.Warnings); e != nil {
				return e
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ChunkDetail(ctx context.Context, scopes []string, docID, chunkID string) (ChunkDetail, error) {
	if _, err := s.GetDocument(ctx, scopes, docID); err != nil {
		return ChunkDetail{}, err
	}
	var c ChunkDetail
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var boxes string
		err := q.QueryRow(ctx, `SELECT id,doc_id,section_path,anchor,text,version,page,paragraph,page_label,positions_json FROM doc_chunks WHERE doc_id=? AND id=?`, docID, chunkID).Scan(&c.ChunkID, &c.DocumentID, &c.Section, &c.Anchor, &c.Text, &c.Version, &c.Page, &c.Paragraph, &c.PageLabel, &boxes)
		if err != nil {
			return ErrNotFound
		}
		c.Text, _ = splitChunkText(c.Text)
		return json.Unmarshal([]byte(boxes), &c.Boxes)
	})
	return c, err
}

func (s *Service) GrepDocument(ctx context.Context, scopes []string, docID, pattern string, limit int) ([]ChunkDetail, error) {
	if len(pattern) > 4096 {
		return nil, &inputError{"memory: pattern too long"}
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, &inputError{err.Error()}
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	d, err := s.GetDocument(ctx, scopes, docID)
	if err != nil {
		return nil, err
	}
	out := []ChunkDetail{}
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, e := q.Query(ctx, `SELECT id,doc_id,section_path,anchor,text,version,page,paragraph,page_label,positions_json FROM doc_chunks WHERE doc_id=? AND version=? ORDER BY char_start,id LIMIT 10000`, docID, d.Version)
		if e != nil {
			return e
		}
		defer rows.Close()
		budget := 8 << 20
		for rows.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			var c ChunkDetail
			var boxes string
			if e := rows.Scan(&c.ChunkID, &c.DocumentID, &c.Section, &c.Anchor, &c.Text, &c.Version, &c.Page, &c.Paragraph, &c.PageLabel, &boxes); e != nil {
				return e
			}
			c.Text, _ = splitChunkText(c.Text)
			budget -= len(c.Text)
			if budget < 0 {
				break
			}
			if re.MatchString(c.Text) {
				if e := json.Unmarshal([]byte(boxes), &c.Boxes); e != nil {
					return e
				}
				out = append(out, c)
				if len(out) >= limit {
					break
				}
			}
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) ExpandHit(ctx context.Context, scopes []string, hit LibraryHit) (ExpandedHit, error) {
	if _, err := s.GetDocument(ctx, scopes, hit.Doc.ID); err != nil {
		return ExpandedHit{}, err
	}
	out := ExpandedHit{Hit: hit}
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		var offset, rowID int64
		if err := q.QueryRow(ctx, `SELECT char_start,rowid FROM doc_chunks WHERE id=? AND doc_id=? AND version=?`, hit.Chunk.ID, hit.Doc.ID, hit.Chunk.Version).Scan(&offset, &rowID); err != nil {
			return ErrNotFound
		}
		neighbour := func(comparison, order string) (string, error) {
			rows, err := q.Query(ctx, `SELECT text,section_path FROM doc_chunks WHERE doc_id=? AND version=? AND (char_start `+comparison+` ? OR (char_start=? AND rowid `+comparison+` ?)) ORDER BY char_start `+order+`,rowid `+order+` LIMIT 1`, hit.Doc.ID, hit.Chunk.Version, offset, offset, rowID)
			if err != nil {
				return "", err
			}
			defer rows.Close()
			if !rows.Next() {
				return "", rows.Err()
			}
			var text, section string
			if err := rows.Scan(&text, &section); err != nil {
				return "", err
			}
			if section != hit.Chunk.Section {
				return "", nil
			}
			text, _ = splitChunkText(text)
			return text, rows.Err()
		}
		var err error
		out.Previous, err = neighbour("<", "DESC")
		if err != nil {
			return err
		}
		out.Next, err = neighbour(">", "ASC")
		if err != nil {
			return err
		}
		rows, err := q.Query(ctx, `SELECT text FROM doc_chunks WHERE doc_id=? AND version=? AND page=? AND kind='table' AND id<>? ORDER BY char_start,rowid`, hit.Doc.ID, hit.Chunk.Version, hit.Chunk.Page, hit.Chunk.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{hit.Chunk.Text: true, out.Previous: true, out.Next: true}
		for rows.Next() {
			var text string
			if err := rows.Scan(&text); err != nil {
				return err
			}
			text, _ = splitChunkText(text)
			if !seen[text] {
				out.Tables = append(out.Tables, text)
				seen[text] = true
			}
		}
		return rows.Err()
	})
	return out, err
}

// libraryVectorNeighbors filters current document versions before ranking.
func (s *Service) libraryVectorNeighbors(ctx context.Context, vector []float32, scopes []string, space string, limit int, docIDs []string) ([]string, error) {
	var scores []scoredHit
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := []any{space}
		args = append(args, scopeArgs(scopes)...)
		filter := ""
		if len(docIDs) > 0 {
			filter = ` AND c.doc_id IN (` + placeholders(len(docIDs)) + `)`
			for _, id := range docIDs {
				args = append(args, id)
			}
		}
		rows, e := q.Query(ctx, `SELECT c.id,v.vector FROM embedding_vectors v JOIN doc_chunks c ON c.id=v.entity_id JOIN documents d ON d.id=c.doc_id AND d.version=c.version WHERE v.entity_kind='doc_chunk' AND v.space_id=? AND d.scope_id IN (`+placeholders(len(scopes))+`)`+filter, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			var id string
			var raw []byte
			if e := rows.Scan(&id, &raw); e != nil {
				return e
			}
			v, e := decodeVector(raw)
			if e != nil {
				return e
			}
			candidate := scoredHit{id: id, s: Cosine(vector, v)}
			if len(scores) < limit || lessScored(candidate, scores[len(scores)-1]) {
				scores = append(scores, candidate)
				sort.Slice(scores, func(i, j int) bool { return lessScored(scores[i], scores[j]) })
				if len(scores) > limit {
					scores = scores[:limit]
				}
			}
		}
		return rows.Err()
	})
	var out []string
	for _, h := range scores {
		out = append(out, h.id)
	}
	return out, err
}
