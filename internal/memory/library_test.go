package memory

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"strings"
	"testing"
	"time"
)

// TestLibraryIngestTextChunksUnpaginated covers structure-aware
// chunking of plain text: sections split, paragraphs numbered, no
// invented pages (Section 10.9 step 3).
func TestLibraryIngestTextChunksUnpaginated(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	text := "# Field guide\n\nThe robin nests in spring hedges.\n\nIt sings before dawn.\n\n## Migration\n\nRobins leave in late autumn."
	doc, err := svc.Ingest(context.Background(), []string{"general"}, "guide.txt", "text/plain", []byte(text),
		IngestOptions{Title: "Field guide", Origin: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Title != "Field guide" {
		t.Fatalf("doc = %+v", doc)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_1", "robin nests", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("library search found nothing")
	}
	seenSection := false
	for _, h := range hits {
		if h.Chunk.Page != 0 {
			t.Fatalf("unpaginated chunk carries page %d", h.Chunk.Page)
		}
		if h.Chunk.Section == "Field guide" {
			seenSection = true
		}
		if !strings.Contains(h.Citation.Verbatim, "¶") {
			t.Fatalf("citation lacks paragraph: %q", h.Citation.Verbatim)
		}
		if strings.Contains(h.Citation.Verbatim, "p. ") {
			t.Fatalf("unpaginated citation invents a page: %q", h.Citation.Verbatim)
		}
	}
	if !seenSection {
		t.Fatal("section anchor missing from chunks")
	}
}

// TestLibraryPaginatedCitesPage covers paginated extraction: exact
// page and paragraph (Section 10.9 step 5).
func TestLibraryPaginatedCitesPage(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	pages := [][]string{
		{"First page intro paragraph.", "First page second paragraph."},
		{"Second page decisive paragraph about/Billing."},
	}
	doc, err := svc.Ingest(context.Background(), []string{"general"}, "report.pdf", "application/pdf", []byte("x"),
		IngestOptions{Title: "Report", Pages: pages, Paginated: true})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_1", "decisive billing", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	cit := hits[0].Citation
	if cit.Page != 2 || cit.Paragraph != 1 {
		t.Fatalf("citation = %+v, want p.2 ¶1", cit)
	}
	if cit.Verbatim != "Report, p. 2, ¶ 1" {
		t.Fatalf("verbatim = %q", cit.Verbatim)
	}
	if hits[0].Doc.ID != doc.ID {
		t.Fatal("hit points at the wrong document")
	}
}

// TestLibraryValidationTree enforces the retrieval-in-tree rule:
// references to unread fragments drop and the claim loses its
// source (Section 10.9 step 6).
func TestLibraryValidationTree(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	pages := [][]string{{"Alpha paragraph content here."}, {"Beta paragraph content there."}}
	doc, err := svc.Ingest(context.Background(), []string{"general"}, "r.pdf", "application/pdf", []byte("x"),
		IngestOptions{Title: "R", Pages: pages, Paginated: true})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_parent", "alpha paragraph", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v %d", err, len(hits))
	}
	goodRef := "[[doc:" + doc.ID + "#" + hits[0].Chunk.ID + "]]"
	// Unknown chunk id drops.
	badRef := "[[doc:" + doc.ID + "#chn_missing]]"
	valid, dropped := svc.ValidateCitations(context.Background(), []string{goodRef, badRef, "not-a-ref"}, []string{"run_parent", "run_child"})
	if len(valid) != 1 || len(dropped) != 2 {
		t.Fatalf("valid=%d dropped=%d, want 1 and 2", len(valid), len(dropped))
	}
	// Retrieved in another tree does not count.
	valid, dropped = svc.ValidateCitations(context.Background(), []string{goodRef}, []string{"run_other"})
	if len(valid) != 0 || len(dropped) != 1 {
		t.Fatalf("cross-tree ref passed: valid=%d dropped=%d", len(valid), len(dropped))
	}
}

// TestLibraryContextSentence stores the disambiguating sentence only
// for marked library documents (Section 10.9 step 3).
func TestLibraryContextSentence(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	_, err := svc.Ingest(context.Background(), []string{"general"}, "a.txt", "text/plain", []byte("Plain note without marks."),
		IngestOptions{Title: "Plain"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Ingest(context.Background(), []string{"general"}, "b.txt", "text/plain", []byte("# Sec\n\nMarked library paragraph."),
		IngestOptions{Title: "Marked", Library: true})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "", "marked library paragraph", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.Doc.Title == "Marked" {
			found = true
			if h.Chunk.Context == "" {
				t.Fatal("marked library chunk lacks its context sentence")
			}
		}
	}
	if !found {
		t.Fatal("marked document not found")
	}
}

// TestSourceEventsOnlyRead builds answer source lists from pages
// actually read in the tree, never unconsulted ones (Section 10.9).
func TestSourceEventsOnlyRead(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	if _, err := svc.RecordSourceEvent(context.Background(), "run_a", "q", "https://a.example/x", "X", "sha1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordSourceEvent(context.Background(), "run_b", "q", "https://b.example/y", "Y", "sha2"); err != nil {
		t.Fatal(err)
	}
	srcs, err := svc.AnswerSources(context.Background(), []string{"run_a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 1 || srcs[0].URL != "https://a.example/x" {
		t.Fatalf("sources = %+v", srcs)
	}
}

// TestPurgeDocumentMatchesAppendixA proves user-requested deletion
// purges content, FTS, vectors and citation reads, leaving only the
// tombstone (Sections 4.7, 10.3).
func TestPurgeDocumentMatchesAppendixA(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	doc, err := svc.Ingest(context.Background(), []string{"general"}, "s.txt", "text/plain", []byte("Secret project details here."),
		IngestOptions{Title: "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "run_1", "secret project", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v", err)
	}
	chunkID := hits[0].Chunk.ID
	if err := svc.PurgeDocument(context.Background(), doc.ID, "user request"); err != nil {
		t.Fatal(err)
	}
	hits, _, err = svc.LibrarySearch(context.Background(), []string{"general"}, "run_2", "secret project", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("purged content still searchable: %d hits", len(hits))
	}
	var vecs, pend, reads int
	err = db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		_ = q.QueryRow(context.Background(), `SELECT COUNT(*) FROM embedding_vectors WHERE entity_id=?`, chunkID).Scan(&vecs)
		_ = q.QueryRow(context.Background(), `SELECT COUNT(*) FROM pending_vectors WHERE id=?`, chunkID).Scan(&pend)
		_ = q.QueryRow(context.Background(), `SELECT COUNT(*) FROM citation_runs WHERE chunk_id=?`, chunkID).Scan(&reads)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if vecs+pend+reads != 0 {
		t.Fatalf("residue after purge: vectors=%d pending=%d reads=%d", vecs, pend, reads)
	}
	var tombs int
	err = db.Read(context.Background(), func(_ context.Context, q store.Queryer) error {
		return q.QueryRow(context.Background(), `SELECT COUNT(*) FROM tombstones WHERE kind=?`, "document:"+doc.ID).Scan(&tombs)
	})
	if err != nil {
		t.Fatal(err)
	}
	if tombs != 1 {
		t.Fatal("tombstone missing after purge")
	}
}
