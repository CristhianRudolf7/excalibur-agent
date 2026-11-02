package memory

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func pendingFixture(t *testing.T, s *Service, digest string) DocumentInfo {
	t.Helper()
	d, exists, err := s.CreatePendingDocument(context.Background(), "general", DocMeta{Title: "Bird guide", SourceName: "robins.pdf", MIME: "application/pdf", Format: "pdf", BlobDigest: digest})
	if err != nil || exists {
		t.Fatalf("pending: %+v %v %v", d, exists, err)
	}
	return d
}
func extractionFixture() docextract.Document {
	return docextract.Document{Format: "pdf", Paginated: true, PageCount: 2, Pages: []docextract.Page{
		{Number: 1, Label: "xii", Method: docextract.MethodNative, Quality: 1, Blocks: []docextract.Block{{Kind: docextract.KindHeading, Level: 1, Text: "Habitat"}, {Kind: docextract.KindParagraph, Text: "Robins nest in hedges.", Boxes: []docextract.Box{{Page: 1, X0: 10, Y0: 20, X1: 50, Y1: 30}}}}},
		{Number: 2, Label: "xiii", Method: docextract.MethodNative, Quality: 1, Blocks: []docextract.Block{{Kind: docextract.KindParagraph, Text: "They sing in spring.", Boxes: []docextract.Box{{Page: 2, X0: 10, Y0: 20, X1: 60, Y1: 30}}}, {Kind: docextract.KindTable, Text: "| Bird | Season |\n| --- | --- |\n| Robin | Spring |"}}},
	}}
}
func TestExtractedLibraryLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestService(t, db, time.Now(), "general")
	d := pendingFixture(t, s, "digest-a")
	same, existing, err := s.CreatePendingDocument(ctx, "general", DocMeta{BlobDigest: "digest-a"})
	if err != nil || !existing || same.ID != d.ID {
		t.Fatalf("dedup: %+v %v %v", same, existing, err)
	}
	if err := s.SetDocumentProgress(ctx, d.ID, "processing", 1, 2, "extract", ""); err != nil {
		t.Fatal(err)
	}
	d, err = s.IngestExtracted(ctx, "general", d.ID, extractionFixture())
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "ready" || d.Version != 1 || !d.Paginated || d.ProgressPages != 2 {
		t.Fatalf("document: %+v", d)
	}
	chunks, err := s.ListChunks(ctx, nil, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Paginated chunks stop at page boundaries: page 1 text, page 2 text
	// and the page 2 table.
	if len(chunks) != 3 || chunks[0].Page != 1 || chunks[1].Page != 2 {
		t.Fatalf("chunks: %+v", chunks)
	}
	detail, err := s.ChunkDetail(ctx, nil, d.ID, chunks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Boxes) != 1 || detail.Boxes[0].Page != 1 || detail.PageLabel != "xii" || detail.Paragraph != 1 {
		t.Fatalf("detail: %+v", detail)
	}
	hits, _, err := s.LibrarySearch(ctx, nil, "run-library", "Bird guide", 10, d.ID)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search title: %+v %v", hits, err)
	}
	var hit LibraryHit
	for _, h := range hits {
		if h.Chunk.ID == detail.ChunkID {
			hit = h
		}
	}
	if !strings.Contains(hit.Citation.Verbatim, "p. xii, ¶ 1") {
		t.Fatalf("citation: %+v", hit.Citation)
	}
	expanded, err := s.ExpandHit(ctx, nil, hit)
	if err != nil {
		t.Fatal(err)
	}
	if expanded.Next == "" {
		t.Fatalf("expansion: %+v", expanded)
	}
	valid, dropped := s.ValidateCitations(ctx, []string{"[[doc:" + d.ID + "#" + detail.ChunkID + "]]"}, []string{"run-library"})
	if len(valid) != 1 || len(dropped) != 0 || valid[0].PageLabel != "xii" {
		t.Fatalf("validation: %+v %v", valid, dropped)
	}
	if hits, _, err := s.LibrarySearch(ctx, nil, "", "Robins", 10, "other"); err != nil || len(hits) != 0 {
		t.Fatalf("filter: %+v %v", hits, err)
	}
	pages, err := s.DocumentPages(ctx, nil, d.ID, 0, 1, 2)
	if err != nil || len(pages) != 2 || pages[0].Label != "xii" {
		t.Fatalf("pages: %+v %v", pages, err)
	}
	if _, err := s.DocumentPages(ctx, nil, d.ID, 0, 1, 21); err == nil {
		t.Fatal("accepted 21 pages")
	}
	if _, err := s.GetDocument(ctx, []string{"project:denied"}, d.ID); err == nil {
		t.Fatal("scope allowed")
	}
	matches, err := s.GrepDocument(ctx, nil, d.ID, "(?i)robin", 1)
	if err != nil || len(matches) != 1 {
		t.Fatalf("grep: %+v %v", matches, err)
	}
	if _, err := s.GrepDocument(ctx, nil, d.ID, "[", 1); err == nil {
		t.Fatal("accepted malformed regexp")
	}
	calls := s.curEmbed().(*FakeEmbedder).Calls()
	d, err = s.IngestExtracted(ctx, "general", d.ID, extractionFixture())
	if err != nil || d.Version != 2 {
		t.Fatalf("same version: %+v %v", d, err)
	}
	if s.curEmbed().(*FakeEmbedder).Calls() != calls {
		t.Fatal("same content reembedded")
	}
	chunks2, err := s.ListChunks(ctx, nil, d.ID)
	if err != nil || chunks2[0].ID != chunks[0].ID {
		t.Fatalf("chunk identity lost: %+v %v", chunks2, err)
	}
	changed := extractionFixture()
	changed.Pages[1].Blocks = append(changed.Pages[1].Blocks, docextract.Block{Kind: docextract.KindHeading, Level: 1, Text: "Migration"}, docextract.Block{Kind: docextract.KindParagraph, Text: "Birds migrate south."})
	d, err = s.IngestExtracted(ctx, "general", d.ID, changed)
	if err != nil || d.Version != 3 {
		t.Fatalf("changed version: %+v %v", d, err)
	}
	if s.curEmbed().(*FakeEmbedder).Calls() != calls+1 {
		t.Fatal("expected one batch for new chunk")
	}
	hits, _, err = s.LibrarySearch(ctx, nil, "", "Robins", 10, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Chunk.Version != 3 {
			t.Fatal("search returned old version")
		}
	}
	list, next, err := s.ListDocuments(ctx, nil, ListQuery{Query: "robins.pdf", Limit: 1})
	if err != nil || len(list) != 1 || next != "" {
		t.Fatalf("list: %+v %q %v", list, next, err)
	}
	if err := s.PurgeDocument(ctx, d.ID, "test"); err != nil {
		t.Fatal(err)
	}
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var n int
		if e := q.QueryRow(ctx, `SELECT count(*) FROM doc_pages WHERE doc_id=?`, d.ID).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			t.Fatal("pages remain after purge")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLibraryCursorsProgressAndSwitch(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, openTestDB(t), time.Now(), "general")
	a := pendingFixture(t, s, "a")
	pendingFixture(t, s, "b")
	list, next, err := s.ListDocuments(ctx, nil, ListQuery{Limit: 1})
	if err != nil || len(list) != 1 || next == "" {
		t.Fatalf("first page: %+v %q %v", list, next, err)
	}
	second, end, err := s.ListDocuments(ctx, nil, ListQuery{Limit: 1, Cursor: next})
	if err != nil || len(second) != 1 || second[0].ID == list[0].ID || end != "" {
		t.Fatalf("second page: %+v %q %v", second, end, err)
	}
	if _, _, err := s.ListDocuments(ctx, nil, ListQuery{Cursor: "bad"}); err == nil {
		t.Fatal("bad cursor allowed")
	}
	for _, status := range []string{"pending", "processing", "partial", "failed", "canceled"} {
		if err := s.SetDocumentProgress(ctx, a.ID, status, 0, 2, "extract", "failed page"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetDocumentProgress(ctx, a.ID, "ready", 3, 2, "", ""); err == nil {
		t.Fatal("invalid progress allowed")
	}
	if _, err := s.IngestExtracted(ctx, "general", a.ID, extractionFixture()); err == nil {
		t.Fatal("canceled document ingested")
	}
	t.Setenv("EXCALIBUR_LIBRARY", "off")
	if _, _, err := s.CreatePendingDocument(ctx, "general", DocMeta{BlobDigest: "c"}); err == nil {
		t.Fatal("switch ignored")
	}
}

func TestStructureAwareChunksAndCitationFormats(t *testing.T) {
	doc := extractionFixture()
	doc.Pages[0].Blocks = append(doc.Pages[0].Blocks, docextract.Block{Kind: docextract.KindParagraph, Text: strings.Repeat("A sentence with useful text. ", 140)}, docextract.Block{Kind: docextract.KindHeading, Level: 2, Text: "Details"}, docextract.Block{Kind: docextract.KindParagraph, Text: "Unique second section."})
	chunks := extractChunks("Guide", doc)
	for _, c := range chunks {
		if len([]rune(c.Text)) > 1800 && c.Kind != "table" {
			t.Fatalf("oversized chunk %d", len(c.Text))
		}
		if c.Section == "Habitat" && strings.Contains(c.Text, "Unique") {
			t.Fatal("crossed heading")
		}
	}
	table := "| A | B |\n| --- | --- |\n" + strings.Repeat("| data | value |\n", 1000)
	parts := splitTable(table)
	if len(parts) < 2 {
		t.Fatal("table not split")
	}
	for _, part := range parts {
		if !strings.HasPrefix(part, "| A | B |\n| --- | --- |") {
			t.Fatal("missing table header")
		}
	}
	for _, test := range []struct {
		format, anchor, want string
		page                 int
	}{{"pptx", "", "slide 4", 4}, {"xlsx", "Sheet1!B2", "Sheet1!B2", 0}, {"txt", "", "§ Intro", 0}} {
		c := RenderCitation(LibraryDoc{Title: "Title", Format: test.format}, Chunk{Page: test.page, Anchor: test.anchor, Section: "Intro", Paragraph: 3})
		if !strings.Contains(c.Verbatim, test.want) {
			t.Fatalf("citation: %+v", c)
		}
	}
}

type recordingLibraryEmbedder struct {
	batches []int
	texts   []string
	fail    bool
	before  func(context.Context) error
}

func (e *recordingLibraryEmbedder) Model() string { return "library-recording" }
func (e *recordingLibraryEmbedder) Close() error  { return nil }
func (e *recordingLibraryEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e.before != nil {
		if err := e.before(ctx); err != nil {
			return nil, err
		}
	}
	e.batches = append(e.batches, len(texts))
	e.texts = append(e.texts, texts...)
	if e.fail {
		return nil, context.DeadlineExceeded
	}
	return NewFakeEmbedder(e.Model(), 512).Embed(ctx, texts)
}

func TestExtractedEmbeddingBatchesReuseAndPartial(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	embed := &recordingLibraryEmbedder{}
	s := NewService(db, WithScopes("general"), WithSettings(testSettings()), WithEmbedder(embed))
	d := pendingFixture(t, s, "batch")
	doc := docextract.Document{Format: "txt", Pages: []docextract.Page{{Number: 1, Method: docextract.MethodNative, Quality: 1}}}
	for i := 0; i < 130; i++ {
		doc.Pages[0].Blocks = append(doc.Pages[0].Blocks, docextract.Block{Kind: docextract.KindHeading, Level: 1, Text: fmt.Sprintf("Section %d", i)}, docextract.Block{Kind: docextract.KindParagraph, Text: fmt.Sprintf("Passage number %d contains material.", i)})
	}
	if _, err := s.IngestExtracted(ctx, "general", d.ID, doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embed.batches, []int{64, 64, 2}) {
		t.Fatalf("batches: %v", embed.batches)
	}
	matches, err := s.GrepDocument(ctx, nil, d.ID, "Passage", 1000)
	if err != nil || len(matches) != 100 {
		t.Fatalf("grep cap: %d %v", len(matches), err)
	}
	if _, err := s.GrepDocument(ctx, nil, d.ID, strings.Repeat("a", 4097), 1); err == nil {
		t.Fatal("oversized regexp accepted")
	}
	for _, text := range embed.texts {
		if strings.Contains(text, "robins.pdf") {
			t.Fatal("lexical metadata leaked into embeddings")
		}
	}
	doc.Pages[0].Blocks[len(doc.Pages[0].Blocks)-1].Text = "A changed final passage."
	if _, err := s.IngestExtracted(ctx, "general", d.ID, doc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(embed.batches, []int{64, 64, 2, 1}) {
		t.Fatalf("reuse failed: %v", embed.batches)
	}
	embed.fail = true
	doc.Pages[0].Blocks[len(doc.Pages[0].Blocks)-1].Text = "A passage while embeddings unavailable."
	doc.Pages[0].Warnings = []string{"Low quality extraction"}
	doc.Pages[0].Method = docextract.MethodNone
	info, err := s.IngestExtracted(ctx, "general", d.ID, doc)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != "partial" || len(info.Warnings) < 2 {
		t.Fatalf("partial: %+v", info)
	}
	err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var n int
		if e := q.QueryRow(ctx, `SELECT count(*) FROM pending_vectors WHERE scope_id='general'`).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			t.Fatalf("pending vectors: %d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	hits, semantic, err := s.LibrarySearch(ctx, nil, "", "unavailable", 10, d.ID)
	if err != nil || len(hits) != 1 || semantic {
		t.Fatalf("lexical degradation: %+v %v %v", hits, semantic, err)
	}
}

func TestLogicalSlidesStaySeparate(t *testing.T) {
	doc := docextract.Document{Format: "pptx", Pages: []docextract.Page{
		{Number: 4, Title: "First slide", Blocks: []docextract.Block{{Kind: docextract.KindSlideText, Text: "The first slide content."}}},
		{Number: 5, Title: "Second slide", Blocks: []docextract.Block{{Kind: docextract.KindSlideText, Text: "The second slide content."}}},
	}}
	chunks := extractChunks("Slides", doc)
	if len(chunks) != 2 || chunks[0].Page != 4 || chunks[1].Page != 5 {
		t.Fatalf("merged slides: %+v", chunks)
	}
}

func TestExtractionDoesNotPublishOverReplacementUpload(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	embed := &recordingLibraryEmbedder{}
	s := NewService(db, WithScopes("general"), WithSettings(testSettings()), WithEmbedder(embed))
	d := pendingFixture(t, s, "original")
	embed.before = func(ctx context.Context) error {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		// A write during the provider call also proves ingestion holds no write transaction.
		_, _, err := s.CreatePendingDocument(bounded, "general", DocMeta{ID: d.ID, Title: "Replacement", BlobDigest: "replacement"})
		return err
	}
	if _, err := s.IngestExtracted(ctx, "general", d.ID, extractionFixture()); err == nil {
		t.Fatal("stale extraction published")
	}
	current, err := s.GetDocument(ctx, nil, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.BlobDigest != "replacement" || current.Status != "pending" {
		t.Fatalf("replacement overwritten: %+v", current)
	}
	chunks, err := s.ListChunks(ctx, nil, d.ID)
	if err != nil || len(chunks) != 0 {
		t.Fatalf("stale chunks: %+v %v", chunks, err)
	}
}

func TestLogicalPagesAndTableExpansion(t *testing.T) {
	ctx := context.Background()
	s := newTestService(t, openTestDB(t), time.Now(), "general")
	d := pendingFixture(t, s, "sheets")
	doc := docextract.Document{Format: "xlsx", Pages: []docextract.Page{
		{Number: 1, Title: "Sheet1", Method: "native", Blocks: []docextract.Block{{Kind: "paragraph", Text: "Sheet one introduction."}, {Kind: "table", Text: "Table one"}, {Kind: "table", Text: "Table two"}, {Kind: "table", Text: "Table two"}}},
		{Number: 2, Title: "Sheet2", Method: "native", Blocks: []docextract.Block{{Kind: "table", Text: "Other sheet table"}}},
	}}
	if _, err := s.IngestExtracted(ctx, "general", d.ID, doc); err != nil {
		t.Fatal(err)
	}
	hits, _, err := s.LibrarySearch(ctx, nil, "", "introduction", 1, d.ID)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits: %+v %v", hits, err)
	}
	if hits[0].Chunk.Page != 1 || strings.Contains(hits[0].Citation.Verbatim, "p. ") {
		t.Fatalf("logical page citation: %+v", hits[0])
	}
	expanded, err := s.ExpandHit(ctx, nil, hits[0])
	if err != nil {
		t.Fatal(err)
	}
	text := expanded.Previous + expanded.Next + strings.Join(expanded.Tables, " ")
	if strings.Contains(text, "Other sheet") || strings.Count(text, "Table two") != 1 {
		t.Fatalf("table expansion: %+v", expanded)
	}
}

func TestOutlineKeepsSectionHierarchy(t *testing.T) {
	doc := docextract.Document{Format: "pdf", Paginated: true, Outline: []docextract.OutlineEntry{{Title: "Chapter", Level: 1, Page: 1}, {Title: "Detail", Level: 2, Page: 2}}, Pages: []docextract.Page{
		{Number: 1, Blocks: []docextract.Block{{Kind: "paragraph", Text: "Opening text."}}},
		{Number: 2, Blocks: []docextract.Block{{Kind: "paragraph", Text: "Detailed text."}}},
	}}
	chunks := extractChunks("Outline", doc)
	if len(chunks) != 2 || chunks[0].Section != "Chapter" || chunks[1].Section != "Chapter / Detail" {
		t.Fatalf("outline hierarchy: %+v", chunks)
	}
}
