package memory

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// IngestOptions tunes one library ingest (Section 10.9).
type IngestOptions struct {
	// Title names the document for citations.
	Title string
	// Origin records where the file came from.
	Origin string
	// Pages carries pre-extracted pages (each page is a paragraph
	// list) for paginated formats; when nil the service extracts.
	Pages [][]string
	// Paginated marks formats with real page numbers. Unpaginated
	// text cites section and paragraph instead of inventing pages.
	Paginated bool
	// Library marks documents selected for the library treatment:
	// one generated context sentence per ambiguous chunk
	// (Section 10.9 step 3).
	Library bool
	// VisionPages selects pages for vision extraction of scanned
	// PDFs and images; nil means all pages.
	VisionPages []int
}

// Ingest stores the original as a versioned blob and writes
// structure-aware chunks with location anchors, FTS5 entries and
// vectors (Section 10.9 steps 1-4). The blob lands before any row
// may reference it (Section 4.2).
func (s *Service) Ingest(ctx context.Context, scopes []string, name string, mime string, data []byte, opts IngestOptions) (LibraryDoc, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return LibraryDoc{}, err
	}
	if len(data) == 0 && len(opts.Pages) == 0 {
		return LibraryDoc{}, &inputError{"memory: ingest needs file bytes or pre-extracted pages"}
	}
	title := opts.Title
	if title == "" {
		title = name
	}
	pages, paginated, err := s.extractPages(ctx, mime, data, opts)
	if err != nil {
		return LibraryDoc{}, err
	}
	chunks := chunkPages(title, pages, paginated, opts.Library)
	if len(chunks) == 0 {
		return LibraryDoc{}, &inputError{"memory: no extractable text"}
	}
	// Batch-embed all chunk texts before the write so no provider
	// I/O runs inside BEGIN IMMEDIATE.
	vectors := make([][]float32, len(chunks))
	if s.curEmbed() != nil {
		texts := make([]string, 0, len(chunks))
		for _, c := range chunks {
			texts = append(texts, embedText(c))
		}
		if vecs, err := s.curEmbed().Embed(ctx, texts); err == nil && len(vecs) == len(chunks) {
			for i, v := range vecs {
				vectors[i] = fitDims(v, s.settings.EmbeddingDimensions)
			}
		}
	}
	digest, err := s.db.Blob().Put(ctx, bytes.NewReader(data))
	if err != nil {
		return LibraryDoc{}, err
	}
	doc := LibraryDoc{ID: newDocID(), Scope: scopes[0], Version: 1, Digest: digest, MIME: mime, Origin: opts.Origin, Title: title}
	space := shadowSpace(s)
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO documents(id, scope_id, version, blob_digest, mime, origin, title) VALUES(?,?,?,?,?,?,?)`,
			doc.ID, doc.Scope, doc.Version, doc.Digest, doc.MIME, doc.Origin, doc.Title); err != nil {
			return err
		}
		if err := ensureSpaceTxSilent(ctx, ex, s); err != nil {
			return err
		}
		for i, c := range chunks {
			c.ID = newChunkID()
			c.Doc = doc.ID
			c.Version = doc.Version
			stored := c.Text
			if c.Context != "" {
				// The context sentence prefixes the row behind a
				// marker; splitChunkText reverses it on load. One
				// row per chunk keeps the FTS triggers untouched.
				stored = c.Context + chunkContextMarker + c.Text
			}
			if _, err := ex.Exec(ctx, `INSERT INTO doc_chunks(id, doc_id, version, page, paragraph, char_start, char_end, section_path, text) VALUES(?,?,?,?,?,?,?,?,?)`,
				c.ID, c.Doc, c.Version, c.Page, c.Paragraph, c.CharStart, c.CharEnd, c.Section, stored); err != nil {
				return err
			}
			chunks[i] = c
			if vectors[i] != nil {
				if err := store.PutEmbedding(ctx, ex, "doc_chunk", c.ID, space, doc.Scope, s.settings.EmbeddingDimensions, vectors[i]); err != nil {
					return err
				}
			} else {
				if err := enqueuePendingTx(ctx, ex, "doc_chunk", c.ID, doc.Scope, s.nowMS()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return LibraryDoc{}, err
	}
	_ = chunks
	return doc, nil
}

func ensureSpaceTxSilent(ctx context.Context, ex store.Execer, s *Service) error {
	if s.curEmbed() == nil {
		return nil
	}
	_, err := ensureSpaceTx(ctx, ex, s.curEmbed().Model(), s.settings.EmbeddingDimensions)
	return err
}

// extractPages resolves pre-extracted pages, native text formats or
// the vision path for scanned PDFs and images (Section 10.9 step 2).
func (s *Service) extractPages(ctx context.Context, mime string, data []byte, opts IngestOptions) ([][]string, bool, error) {
	if len(opts.Pages) > 0 {
		return opts.Pages, opts.Paginated, nil
	}
	switch {
	case isTextMIME(mime):
		return paginateText(string(data)), false, nil
	case isVisionMIME(mime):
		if s.vision == nil {
			return nil, false, &inputError{"memory: scanned PDFs and images need the vision extractor (vision_extract)"}
		}
		pages := opts.VisionPages
		blocks, err := s.vision.ExtractText(ctx, opts.Origin, pages)
		if err != nil {
			return nil, false, err
		}
		return blocks, true, nil
	default:
		return nil, false, &inputError{fmt.Sprintf("memory: unsupported library format %q", mime)}
	}
}

func isTextMIME(mime string) bool {
	m := strings.ToLower(mime)
	return m == "" || strings.HasPrefix(m, "text/") || strings.Contains(m, "markdown") ||
		m == "application/json" || m == "application/csv" || strings.HasSuffix(m, "+xml") || m == "text/html"
}

func isVisionMIME(mime string) bool {
	m := strings.ToLower(mime)
	return m == "application/pdf" || strings.HasPrefix(m, "image/")
}

// paginateText splits plain text into one pseudo-page of paragraphs;
// page stays 0 so citations never invent page numbers (Section 10.9).
func paginateText(text string) [][]string {
	paras := splitParagraphs(text)
	if len(paras) == 0 {
		return nil
	}
	return [][]string{paras}
}

// splitParagraphs cuts on blank lines.
func splitParagraphs(text string) []string {
	var out []string
	for _, p := range strings.Split(text, "\n\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// chunkPages assigns anchors: headings open sections, paragraphs
// number inside their page, offsets count document characters.
func chunkPages(title string, pages [][]string, paginated, library bool) []Chunk {
	var out []Chunk
	offset := 0
	section := ""
	para := 0
	pageNo := 0
	for _, page := range pages {
		if paginated {
			pageNo++
		}
		para = 0
		for _, p := range page {
			if h, ok := headingOf(p); ok {
				section = h
				offset += len(p) + 2
				continue
			}
			para++
			c := Chunk{Page: pageNo, Paragraph: para, CharStart: offset, CharEnd: offset + len(p), Section: section, Text: p}
			if library {
				c.Context = contextSentence(title, section, p)
			}
			out = append(out, c)
			offset += len(p) + 2
		}
	}
	return out
}

func headingOf(para string) (string, bool) {
	t := strings.TrimSpace(para)
	if strings.HasPrefix(t, "#") {
		h := strings.TrimSpace(strings.TrimLeft(t, "#"))
		if h != "" {
			return h, true
		}
	}
	if t != "" && len(t) < 120 && strings.ToUpper(t) == t && len(strings.Fields(t)) <= 8 {
		return t, true
	}
	return "", false
}

// contextSentence is the one deterministic disambiguating sentence
// stored for marked library documents (Section 10.9 step 3).
func contextSentence(title, section, para string) string {
	words := strings.Fields(para)
	head := strings.Join(words, " ")
	if len(words) > 12 {
		head = strings.Join(words[:12], " ") + "…"
	}
	if section != "" {
		return fmt.Sprintf("Passage from %q, section %q, about: %s", title, section, head)
	}
	return fmt.Sprintf("Passage from %q, about: %s", title, head)
}

// embedText joins the context sentence and the chunk text so the
// vector carries the disambiguation (Section 10.9, Contextual
// Retrieval).
func embedText(c Chunk) string {
	if c.Context != "" {
		return c.Context + "\n" + c.Text
	}
	return c.Text
}

// ---- search ----

// LibraryHit is one citable search hit.
type LibraryHit struct {
	Doc      LibraryDoc `json:"doc"`
	Chunk    Chunk      `json:"chunk"`
	Citation Citation   `json:"citation"`
	Score    float64    `json:"score"`
}

// LibrarySearch answers queries with citable fragments (Section
// 10.9 step 4). runID records which run retrieved each chunk so
// citation validation can prove the tree actually read it.
func (s *Service) LibrarySearch(ctx context.Context, scopes []string, runID, query string, k int, docIDs ...string) ([]LibraryHit, bool, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, false, err
	}
	if len(docIDs) > 100 {
		return nil, false, &inputError{"memory: at most 100 document filters"}
	}
	if k <= 0 {
		k = 10
	}
	if k > 100 {
		k = 100
	}
	perList := s.settings.CandidatesPerList
	if perList <= 0 {
		perList = 50
	}
	lexical := s.chunkLexicalList(ctx, scopes, query, perList, docIDs...)
	semantic, semanticOK := s.chunkSemanticList(ctx, scopes, query, perList, docIDs...)
	fused := fuseRRF(s.settings.RRFFusedK, lexical, semantic)
	// Apply document filters before taking the requested result count.
	hits := s.loadHits(ctx, fused, len(fused))
	filtered := hits[:0]
	allowed := map[string]bool{}
	for _, id := range docIDs {
		allowed[id] = true
	}
	for _, h := range hits {
		if len(docIDs) == 0 || allowed[h.Doc.ID] {
			filtered = append(filtered, h)
			if len(filtered) >= k {
				break
			}
		}
	}
	hits = filtered
	if runID != "" && len(hits) > 0 {
		_ = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
			for _, h := range hits {
				_, _ = ex.Exec(ctx, `INSERT INTO citation_runs(chunk_id, run_id, at_ms) VALUES(?,?,?)
					ON CONFLICT(chunk_id, run_id) DO UPDATE SET at_ms=excluded.at_ms`, h.Chunk.ID, runID, s.nowMS())
			}
			return nil
		})
	}
	return hits, semanticOK, nil
}

func (s *Service) chunkLexicalList(ctx context.Context, scopes []string, query string, limit int, docIDs ...string) []string {
	match := ftsOr(query, quotedPhrases(query))
	if match == "" {
		return nil
	}
	var ids []string
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := []any{match}
		args = append(args, scopeArgs(scopes)...)
		filter := ""
		if len(docIDs) > 0 {
			filter = " AND c.doc_id IN (" + placeholders(len(docIDs)) + ")"
			for _, id := range docIDs {
				args = append(args, id)
			}
		}
		rows, err := q.Query(ctx, `SELECT c.id FROM doc_chunks_fts x JOIN doc_chunks c ON c.id = x.id JOIN documents d ON d.id=c.doc_id AND d.version=c.version
			WHERE doc_chunks_fts MATCH ? AND d.scope_id IN (`+placeholders(len(scopes))+`)
			`+filter+` ORDER BY bm25(doc_chunks_fts) LIMIT ?`, append(args, limit)...)
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

func (s *Service) chunkSemanticList(ctx context.Context, scopes []string, query string, limit int, docIDs ...string) ([]string, bool) {
	cur, prev, prevSpace := s.embedSnapshot()
	if cur == nil {
		return nil, false
	}
	seen := map[string]bool{}
	var out []string
	ok := false
	if vecs, err := cur.Embed(ctx, []string{query}); err == nil && len(vecs) > 0 {
		qv := fitDims(vecs[0], s.settings.EmbeddingDimensions)
		space := spaceID(cur.Model(), s.settings.EmbeddingDimensions)
		if ids, err := s.libraryVectorNeighbors(ctx, qv, scopes, space, limit, docIDs); err == nil && len(ids) > 0 {
			for _, id := range aboveFloor(ctx, s, "doc_chunk", qv, space, ids) {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			ok = true
		}
	}
	if prev != nil && prevSpace != "" {
		if vecs, err := prev.Embed(ctx, []string{query}); err == nil && len(vecs) > 0 {
			qv := fitDims(vecs[0], s.settings.EmbeddingDimensions)
			if ids, err := s.libraryVectorNeighbors(ctx, qv, scopes, prevSpace, limit, docIDs); err == nil {
				for _, id := range aboveFloor(ctx, s, "doc_chunk", qv, prevSpace, ids) {
					if !seen[id] {
						seen[id] = true
						out = append(out, id)
					}
				}
				if len(ids) > 0 {
					ok = true
				}
			}
		}
	}
	if !ok {
		return nil, false
	}
	return out, true
}

func (s *Service) loadHits(ctx context.Context, fused []fusedHit, k int) []LibraryHit {
	docs := map[string]LibraryDoc{}
	chunks := map[string]Chunk{}
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for _, fc := range chunkStrings(fusedIDs(fused), 200) {
			args := make([]any, 0, len(fc))
			for _, id := range fc {
				args = append(args, id)
			}
			rows, err := q.Query(ctx, `SELECT id, doc_id, version, page, paragraph, char_start, char_end, section_path, text, page_label, anchor FROM doc_chunks WHERE id IN (`+placeholders(len(fc))+`)`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				c, err := scanChunk(rows)
				if err != nil {
					rows.Close()
					return err
				}
				c.Text, c.Context = splitChunkText(c.Text)
				chunks[c.ID] = c
			}
			rows.Close()
		}
		// Load each owning document once.
		seen := map[string]bool{}
		for _, c := range chunks {
			if seen[c.Doc] {
				continue
			}
			seen[c.Doc] = true
			d, err := getLibraryDocTx(ctx, q, c.Doc)
			if err == nil {
				docs[c.Doc] = d
			}
		}
		return nil
	})
	var hits []LibraryHit
	for _, h := range fused {
		c, ok := chunks[h.id]
		if !ok {
			continue
		}
		d, ok := docs[c.Doc]
		if !ok || c.Version != d.Version {
			continue
		}
		hits = append(hits, LibraryHit{Doc: d, Chunk: c, Citation: RenderCitation(d, c), Score: h.score})
		if len(hits) >= k {
			break
		}
	}
	return hits
}

// chunkContextMarker separates the stored context sentence from the
// chunk text; splitChunkText reverses the marker on load.
const chunkContextMarker = "\n⟦ctx⟧\n"

func splitChunkText(stored string) (text, contextText string) {
	if i := strings.Index(stored, chunkContextMarker); i >= 0 {
		return stored[i+len(chunkContextMarker):], stored[:i]
	}
	return stored, ""
}

// ListChunks returns the versioned chunks of one document inside
// the scope set, for inspection and citation debugging.
func (s *Service) ListChunks(ctx context.Context, scopes []string, docID string) ([]Chunk, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	var out []Chunk
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		doc, err := getLibraryDocTx(ctx, q, docID)
		if err != nil {
			return err
		}
		allowed := false
		for _, sc := range scopes {
			if sc == doc.Scope {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q", ErrScopeDenied, doc.Scope)
		}
		chunks, err := listChunksTx(ctx, q, docID, doc.Version)
		if err != nil {
			return err
		}
		for _, c := range chunks {
			c.Text, c.Context = splitChunkText(c.Text)
			out = append(out, c)
		}
		return nil
	})
	return out, err
}

// getChunkTx loads one chunk of a document.
func getChunkTx(ctx context.Context, q store.Queryer, docID, chunkID string) (Chunk, error) {
	rows, err := q.Query(ctx, `SELECT id, doc_id, version, page, paragraph, char_start, char_end, section_path, text, page_label, anchor FROM doc_chunks WHERE id=? AND doc_id=?`, chunkID, docID)
	if err != nil {
		return Chunk{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return Chunk{}, fmt.Errorf("%w: chunk %q", ErrNotFound, chunkID)
	}
	c, err := scanChunk(rows)
	if err != nil {
		return Chunk{}, err
	}
	c.Text, c.Context = splitChunkText(c.Text)
	return c, rows.Err()
}

var citeRe = regexp.MustCompile(`\[\[doc:([^\]#]+)#([^\]]+)\]\]`)

// ---- citations ----

// ParseCitationRef parses [[doc:<docID>#<chunkID>].
func ParseCitationRef(ref string) (docID, chunkID string, ok bool) {
	m := citeRe.FindStringSubmatch(ref)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// RenderCitation converts a chunk id into the human citation from
// metadata. The model never writes page numbers (Section 10.9 step
// 5): unpaginated formats cite section and paragraph, never an
// invented page.
func RenderCitation(doc LibraryDoc, c Chunk) Citation {
	cit := Citation{ChunkID: c.ID, DocID: doc.ID, DocTitle: doc.Title, Page: c.Page, Paragraph: c.Paragraph, Section: c.Section, PageLabel: c.PageLabel}
	switch {
	case strings.Contains(c.Anchor, "!"):
		cit.Verbatim = fmt.Sprintf("%s, %s", doc.Title, c.Anchor)
	case doc.Format == "pptx" || doc.Format == "odp":
		cit.Verbatim = fmt.Sprintf("%s, slide %d", doc.Title, c.Page)
	case c.Page > 0 && c.PageLabel != "" && (doc.Paginated || doc.Format == ""):
		cit.Verbatim = fmt.Sprintf("%s, p. %s, ¶ %d", doc.Title, c.PageLabel, c.Paragraph)
	case c.Page > 0 && (doc.Paginated || doc.Format == ""):
		cit.Verbatim = fmt.Sprintf("%s, p. %d, ¶ %d", doc.Title, c.Page, c.Paragraph)
	case c.Section != "":
		cit.Verbatim = fmt.Sprintf("%s, § %s, ¶ %d", doc.Title, c.Section, c.Paragraph)
	default:
		cit.Verbatim = fmt.Sprintf("%s, ¶ %d (chars %d–%d)", doc.Title, c.Paragraph, c.CharStart, c.CharEnd)
	}
	return cit
}

// ValidateCitations drops every reference that does not point at a
// fragment retrieved in the task tree and marks the claim unsourced
// (Section 10.9 step 6). tree holds the answering run and all its
// descendants; unknown or malformed refs never pass.
func (s *Service) ValidateCitations(ctx context.Context, refs []string, tree []string) (valid []Citation, dropped []string) {
	inTree := map[string]bool{}
	for _, r := range tree {
		inTree[r] = true
	}
	for _, ref := range refs {
		docID, chunkID, ok := ParseCitationRef(ref)
		if !ok {
			dropped = append(dropped, ref)
			continue
		}
		var doc LibraryDoc
		var chunk Chunk
		found := false
		read := false
		_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
			d, err := getLibraryDocTx(ctx, q, docID)
			if err != nil {
				return nil
			}
			c, err := getChunkTx(ctx, q, docID, chunkID)
			if err != nil {
				return nil
			}
			doc, chunk, found = d, c, true
			rrows, err := q.Query(ctx, `SELECT run_id FROM citation_runs WHERE chunk_id=?`, chunkID)
			if err != nil {
				return nil
			}
			defer rrows.Close()
			for rrows.Next() {
				var run string
				if err := rrows.Scan(&run); err != nil {
					return err
				}
				if inTree[run] {
					read = true
				}
			}
			return rrows.Err()
		})
		if !found || !read {
			dropped = append(dropped, ref)
			continue
		}
		valid = append(valid, RenderCitation(doc, chunk))
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].ChunkID < valid[j].ChunkID })
	return valid, dropped
}

// ---- source events ----

// RecordSourceEvent logs one web page read backing answer sources:
// the source list of a response holds only pages actually read in
// its task tree, never unconsulted ones (Section 10.9).
func (s *Service) RecordSourceEvent(ctx context.Context, runID, query, url, title, contentSHA string) (SourceEvent, error) {
	ev := SourceEvent{ID: newSourceID(), RunID: runID, Query: query, URL: url, Title: title, FetchedAt: s.now().UTC(), ContentSHA: contentSHA}
	err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO source_events(id, run_id, query, url, title, fetched_ms, content_hash) VALUES(?,?,?,?,?,?,?)`,
			ev.ID, ev.RunID, ev.Query, ev.URL, ev.Title, ev.FetchedAt.UnixMilli(), ev.ContentSHA)
		return err
	})
	if err != nil {
		return SourceEvent{}, err
	}
	// Telemetry mirroring lands with the H10 telemetry schema, which
	// owns the source_events table in telemetry.db; the primary
	// record above is complete without it.
	return ev, nil
}

// AnswerSources lists the source events read by the task tree.
func (s *Service) AnswerSources(ctx context.Context, tree []string) ([]SourceEvent, error) {
	var out []SourceEvent
	err := s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for _, fc := range chunkStrings(tree, 200) {
			args := make([]any, 0, len(fc))
			for _, id := range fc {
				args = append(args, id)
			}
			rows, err := q.Query(ctx, `SELECT id, run_id, query, url, title, fetched_ms, content_hash FROM source_events WHERE run_id IN (`+placeholders(len(fc))+`) ORDER BY fetched_ms`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var ev SourceEvent
				var at int64
				if err := rows.Scan(&ev.ID, &ev.RunID, &ev.Query, &ev.URL, &ev.Title, &at, &ev.ContentSHA); err != nil {
					rows.Close()
					return err
				}
				ev.FetchedAt = fromMS(at)
				out = append(out, ev)
			}
			rows.Close()
		}
		return nil
	})
	return out, err
}

// PurgeMemory forgets one memory with tombstone and backup notice
// (Sections 4.7, 10.3): content rows, FTS, vectors and pending jobs
// go; only the tombstone stays.
func (s *Service) PurgeMemory(ctx context.Context, memoryID, reason string) error {
	if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `DELETE FROM pending_vectors WHERE kind='memory' AND id=?`, memoryID)
		return err
	}); err != nil {
		return err
	}
	return store.PurgeMemory(ctx, s.db, s.tele, memoryID, reason)
}

// PurgeDocument forgets one library document with the same
// guarantees, including citation reads.
func (s *Service) PurgeDocument(ctx context.Context, documentID, reason string) error {
	if err := s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `DELETE FROM pending_vectors WHERE kind='doc_chunk' AND id IN (SELECT id FROM doc_chunks WHERE doc_id=?)`, documentID); err != nil {
			return err
		}
		_, err := ex.Exec(ctx, `DELETE FROM citation_runs WHERE chunk_id IN (SELECT id FROM doc_chunks WHERE doc_id=?)`, documentID)
		return err
	}); err != nil {
		return err
	}
	return store.PurgeDocument(ctx, s.db, s.tele, s.db.Blob(), documentID, reason)
}

// BackupNotice is the confirmation text user-facing deletions must
// show (Section 4.7).
const BackupNotice = store.BackupPurgeNotice
