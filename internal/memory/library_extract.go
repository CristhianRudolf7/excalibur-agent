package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

type extractedChunk struct {
	Chunk
	Kind, Anchor, PageLabel, SHA string
	Boxes                        []docextract.Box
}

func textSHA(text string) string { h := sha256.Sum256([]byte(text)); return hex.EncodeToString(h[:]) }
func jsonText(v any) string      { raw, _ := json.Marshal(v); return string(raw) }

// IngestExtracted stores an extraction atomically, with provider work outside
// the write transaction. Identical content keeps existing citation identities.
func (s *Service) IngestExtracted(ctx context.Context, scope, docID string, doc docextract.Document) (DocumentInfo, error) {
	if !libraryEnabled() {
		return DocumentInfo{}, &inputError{"memory: library disabled"}
	}
	old, err := s.GetDocument(ctx, []string{scope}, docID)
	if err != nil {
		return DocumentInfo{}, err
	}
	if len(doc.Outline) > 100000 {
		return DocumentInfo{}, &inputError{"memory: too many outline entries"}
	}
	if len(doc.Pages) > 2000 {
		return DocumentInfo{}, &inputError{"memory: too many document pages"}
	}
	var previousSHA string
	var previousChunks int
	err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT content_sha,(SELECT count(*) FROM doc_chunks WHERE doc_id=documents.id) FROM documents WHERE id=?`, docID).Scan(&previousSHA, &previousChunks)
	})
	if err != nil {
		return DocumentInfo{}, err
	}
	var content strings.Builder
	textBytes := 0
	blockCount := 0
	warnings := append([]string{}, doc.Warnings...)
	status := "ready"
	pageNumbers := map[int]bool{}
	for i := range doc.Pages {
		if err := ctx.Err(); err != nil {
			return DocumentInfo{}, err
		}
		p := &doc.Pages[i]
		if p.Number <= 0 {
			p.Number = i + 1
		}
		if pageNumbers[p.Number] {
			return DocumentInfo{}, &inputError{"memory: duplicate page number"}
		}
		pageNumbers[p.Number] = true
		if p.Method == docextract.MethodNone || len(p.Warnings) > 0 {
			status = "partial"
		}
		warnings = append(warnings, p.Warnings...)
		for _, b := range p.Blocks {
			textBytes += len(b.Text)
			blockCount++
			if textBytes > 64<<20 || blockCount > 100000 {
				return DocumentInfo{}, &inputError{"memory: extracted document exceeds text budget"}
			}
			content.WriteString(b.Text)
			content.WriteString("\n\n")
		}
	}
	if len(warnings) > 0 {
		status = "partial"
	}
	sha := textSHA(content.String())
	same := previousSHA != "" && previousSHA == sha
	chunks := extractChunks(old.Title, doc)
	if len(chunks) == 0 {
		status = "partial"
		warnings = append(warnings, "No extractable text")
	}
	version := old.Version
	if previousSHA != "" || previousChunks > 0 {
		version++
	}
	vectors := make([][]float32, len(chunks))
	reuse := map[string][]float32{}
	embed := s.curEmbed()
	space := shadowSpace(s)
	if !same && embed != nil {
		err = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
			rows, e := q.Query(ctx, `SELECT c.chunk_sha,v.vector FROM doc_chunks c JOIN embedding_vectors v ON v.entity_id=c.id AND v.entity_kind='doc_chunk' AND v.space_id=? WHERE c.doc_id=? AND c.version=?`, space, docID, old.Version)
			if e != nil {
				return e
			}
			defer rows.Close()
			for rows.Next() {
				var hash string
				var raw []byte
				if e := rows.Scan(&hash, &raw); e != nil {
					return e
				}
				v, e := decodeVector(raw)
				if e != nil {
					return e
				}
				reuse[hash] = v
			}
			return rows.Err()
		})
		if err != nil {
			return DocumentInfo{}, err
		}
		var missing []int
		for i, c := range chunks {
			if v, ok := reuse[c.SHA]; ok {
				vectors[i] = v
			} else {
				missing = append(missing, i)
			}
		}
		for start := 0; start < len(missing); start += 64 {
			end := start + 64
			if end > len(missing) {
				end = len(missing)
			}
			batch := missing[start:end]
			texts := make([]string, len(batch))
			for j, i := range batch {
				texts[j] = embedText(chunks[i].Chunk)
			}
			vecs, e := embed.Embed(ctx, texts)
			if ctx.Err() != nil {
				return DocumentInfo{}, ctx.Err()
			}
			if e != nil || len(vecs) != len(batch) {
				warnings = append(warnings, "Embeddings pending; lexical search remains available")
				continue
			}
			for j, i := range batch {
				vectors[i] = fitDims(vecs[j], s.settings.EmbeddingDimensions)
			}
		}
	}
	err = s.ctxWrite(ctx, func(ctx context.Context, ex store.Execer) error {
		var currentVersion int
		var currentSHA, currentStatus, currentDigest string
		if e := ex.QueryRow(ctx, `SELECT version,content_sha,status,blob_digest FROM documents WHERE id=?`, docID).Scan(&currentVersion, &currentSHA, &currentStatus, &currentDigest); e != nil {
			return e
		}
		if currentVersion != old.Version || currentSHA != previousSHA || currentDigest != old.BlobDigest {
			return fmt.Errorf("memory: document changed during extraction")
		}
		if currentStatus == "canceled" {
			return fmt.Errorf("memory: document canceled")
		}
		if e := ensureSpaceTxSilent(ctx, ex, s); e != nil {
			return e
		}
		for _, p := range doc.Pages {
			if _, e := ex.Exec(ctx, `INSERT OR REPLACE INTO doc_pages(doc_id,version,number,label,title,width,height,method,quality,blocks_json,warnings_json) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, docID, version, p.Number, p.Label, p.Title, p.Width, p.Height, p.Method, p.Quality, jsonText(p.Blocks), jsonText(p.Warnings)); e != nil {
				return e
			}
		}
		if same {
			if _, e := ex.Exec(ctx, `UPDATE doc_chunks SET version=? WHERE doc_id=? AND version=?`, version, docID, old.Version); e != nil {
				return e
			}
		} else {
			for i, c := range chunks {
				id := newChunkID()
				stored := c.Context + chunkContextMarker + c.Text
				if _, e := ex.Exec(ctx, `INSERT INTO doc_chunks(id,doc_id,scope_id,version,page,paragraph,char_start,char_end,section_path,text,kind,anchor,page_label,positions_json,chunk_sha,token_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, docID, old.ScopeID, version, c.Page, c.Paragraph, c.CharStart, c.CharEnd, c.Section, stored, c.Kind, c.Anchor, c.PageLabel, jsonText(c.Boxes), c.SHA, (utf8.RuneCountInString(c.Text)+3)/4); e != nil {
					return e
				}
				if vectors[i] != nil {
					if e := store.PutEmbedding(ctx, ex, "doc_chunk", id, space, old.ScopeID, s.settings.EmbeddingDimensions, vectors[i]); e != nil {
						return e
					}
				} else {
					if e := enqueuePendingTx(ctx, ex, "doc_chunk", id, old.ScopeID, s.nowMS()); e != nil {
						return e
					}
				}
			}
		}
		count := doc.PageCount
		if count == 0 {
			count = len(doc.Pages)
		}
		_, e := ex.Exec(ctx, `UPDATE documents SET version=?,format=?,status=?,progress_pages=?,total_pages=?,page_count=?,paginated=?,warnings_json=?,content_sha=?,failure_stage='',error='',updated_at=? WHERE id=?`, version, doc.Format, status, len(doc.Pages), count, count, doc.Paginated, jsonText(warnings), sha, s.nowMS(), docID)
		return e
	})
	if err != nil {
		return DocumentInfo{}, err
	}
	return s.GetDocument(ctx, []string{scope}, docID)
}

// extractChunks keeps headings as hard boundaries. Paragraphs may span pages;
// their rectangles retain physical page numbers when combined into a chunk.
func extractChunks(title string, doc docextract.Document) []extractedChunk {
	var out []extractedChunk
	var active extractedChunk
	headings := []string{}
	outlineSections := []string{}
	outlineByPage := map[int][]docextract.OutlineEntry{}
	for _, entry := range doc.Outline {
		outlineByPage[entry.Page] = append(outlineByPage[entry.Page], entry)
	}
	section := ""
	offset := 0
	flush := func() {
		if active.Text != "" {
			active.Context = contextSentence(title, active.Section, active.Text)
			active.SHA = textSHA(active.Text)
			out = append(out, active)
			active = extractedChunk{}
		}
	}
	for _, p := range doc.Pages {
		// Paginated chunks never cross a page boundary, so every citation
		// points at the exact page that holds the passage.
		if doc.Paginated {
			flush()
		}
		if doc.Format == "pptx" || doc.Format == "odp" || doc.Format == "xlsx" || doc.Format == "ods" {
			flush()
			headings = nil
			section = p.Title
		}
		if len(headings) == 0 {
			for _, entry := range outlineByPage[p.Number] {
				level := entry.Level
				if level < 1 {
					level = 1
				}
				if level > 6 {
					level = 6
				}
				if level > len(outlineSections)+1 {
					level = len(outlineSections) + 1
				}
				outlineSections = append(outlineSections[:level-1], entry.Title)
				path := strings.Join(outlineSections, " / ")
				if section != path {
					flush()
					section = path
				}
			}
		}
		paragraph := 0
		for _, b := range p.Blocks {
			text := strings.TrimSpace(b.Text)
			if text == "" {
				continue
			}
			if b.Kind == docextract.KindHeading {
				flush()
				level := b.Level
				if level < 1 {
					level = 1
				}
				if level > 6 {
					level = 6
				}
				if level > len(headings)+1 {
					level = len(headings) + 1
				}
				headings = headings[:level-1]
				headings = append(headings, text)
				section = strings.Join(headings, " / ")
				offset += len(text) + 2
				continue
			}
			paragraph++
			page := p.Number
			seed := extractedChunk{Chunk: Chunk{Page: page, Paragraph: paragraph, Section: section, CharStart: offset}, Kind: "child", Anchor: b.Anchor, PageLabel: p.Label, Boxes: mergeChunkBoxes(b.Boxes)}
			if b.Kind == docextract.KindTable || b.Kind == docextract.KindCell {
				flush()
				seed.Kind = "table"
				for _, part := range splitTable(text) {
					c := seed
					c.Text = part
					c.CharEnd = offset + len(part)
					c.Context = contextSentence(title, section, part)
					c.SHA = textSHA(part)
					out = append(out, c)
				}
				offset += len(text) + 2
				continue
			}
			pieceOffset := offset
			for _, part := range splitChunkTextBounded(text) {
				seed.CharStart = pieceOffset
				if active.Text != "" && utf8.RuneCountInString(active.Text)+utf8.RuneCountInString(part) > 1600 {
					previous := active
					flush()
					overlap := chunkOverlap(previous.Text)
					active = previous
					active.Text = overlap
					active.CharStart = previous.CharEnd - len(overlap)
				}
				if active.Text == "" {
					active = seed
				} else {
					active.Text += "\n\n"
					active.Boxes = mergeChunkBoxes(append(active.Boxes, seed.Boxes...))
				}
				active.Text += part
				active.CharEnd = pieceOffset + len(part)
				pieceOffset += len(part)
			}
			offset += len(text) + 2
		}
	}
	flush()
	return out
}

func mergeChunkBoxes(boxes []docextract.Box) []docextract.Box {
	out := []docextract.Box{}
	indexes := map[int]int{}
	for _, b := range boxes {
		if i, ok := indexes[b.Page]; ok {
			p := &out[i]
			p.X0 = min(p.X0, b.X0)
			p.Y0 = min(p.Y0, b.Y0)
			p.X1 = max(p.X1, b.X1)
			p.Y1 = max(p.Y1, b.Y1)
		} else {
			indexes[b.Page] = len(out)
			out = append(out, b)
		}
	}
	return out
}

func splitChunkTextBounded(text string) []string {
	var out []string
	runes := []rune(text)
	for len(runes) > 1400 {
		cut := 1400
		for i := 1400; i >= 1000; i-- {
			if runes[i-1] == '\n' || ((runes[i-1] == '.' || runes[i-1] == '!' || runes[i-1] == '?') && runes[i] == ' ') {
				cut = i
				break
			}
		}
		if cut == 1400 {
			for i := 1400; i >= 1000; i-- {
				if runes[i] == ' ' {
					cut = i
					break
				}
			}
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if tail := strings.TrimSpace(string(runes)); tail != "" {
		out = append(out, tail)
	}
	return out
}
func chunkOverlap(text string) string {
	r := []rune(text)
	if len(r) <= 192 {
		return text
	}
	start := len(r) - 192
	for i := start; i < len(r); i++ {
		if r[i] == '\n' || ((r[i] == '.' || r[i] == '!' || r[i] == '?') && i+1 < len(r) && r[i+1] == ' ') {
			return strings.TrimSpace(string(r[i+1:]))
		}
	}
	for i := start; i < len(r); i++ {
		if r[i] == ' ' {
			return strings.TrimSpace(string(r[i+1:]))
		}
	}
	return ""
}
func splitTable(text string) []string {
	rows := strings.Split(text, "\n")
	if len(rows) < 3 {
		return []string{text}
	}
	header := strings.Join(rows[:2], "\n")
	var out []string
	current := header
	for _, row := range rows[2:] {
		if len(current)+len(row) > 6400 && current != header {
			out = append(out, current)
			current = header
		}
		current += "\n" + row
	}
	if current != header {
		out = append(out, current)
	}
	return out
}
