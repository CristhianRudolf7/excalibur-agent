package memory

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// TestHeuristicExtractorKeepsExplicitSpeech pins the deterministic
// fallback extractor (Section 10.5 step 3).
func TestHeuristicExtractorKeepsExplicitSpeech(t *testing.T) {
	x := HeuristicExtractor{}
	cands, err := x.Extract(context.Background(), "general", []Turn{
		{MsgID: "msg_1", Conversation: "cnv_1", Text: "The weather is nice today. Remember that my office is on Baker Street. I prefer concise answers."},
		{MsgID: "msg_2", Conversation: "cnv_1", Text: "Small talk without memory content here."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d: %+v", len(cands), cands)
	}
	if cands[0].Sources[0].Ref != "msg_1" || cands[0].Trust != TrustUser {
		t.Fatalf("provenance wrong: %+v", cands[0])
	}
	if cands[1].Kind != KindPreference {
		t.Fatalf("preference kind = %q", cands[1].Kind)
	}
}

// scriptRewriter returns a canned rephrasing for rewrite tests.
type scriptRewriter struct {
	to    string
	calls int
}

func (r *scriptRewriter) Rewrite(_ context.Context, q string) (string, error) {
	r.calls++
	_ = q
	return r.to, nil
}

// TestRewriteOnceOnZeroResults asserts the single rephrase on empty
// results and that it never fires when the first pass hits.
func TestRewriteOnceOnZeroResults(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")
	rw := &scriptRewriter{to: "harbor tariffs"}
	svc.rewriter = rw

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Harbor tariffs rise in spring"})
	ret, err := svc.Retrieve(context.Background(), Query{Text: "xqzvblm zzzqqq", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if rw.calls != 1 {
		t.Fatalf("rewrite calls = %d, want exactly one", rw.calls)
	}
	if len(ret.Items) == 0 {
		t.Fatal("rephrased query found nothing")
	}
	found := false
	for _, n := range ret.Notices {
		if n.Code == "rewritten" {
			found = true
		}
	}
	if !found {
		t.Fatal("rewritten notice missing")
	}
	ret, err = svc.Retrieve(context.Background(), Query{Text: "harbor tariffs", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if rw.calls != 1 {
		t.Fatalf("rewrite fired on a hit: calls = %d", rw.calls)
	}
}

// scriptReranker reverses the order to prove the harness honors the
// reranked order directly (Section 10.7: no RRF re-fusion).
type scriptReranker struct{ calls int }

func (r *scriptReranker) Rerank(_ context.Context, _ string, items []ResultItem) ([]ResultItem, error) {
	r.calls++
	out := append([]ResultItem(nil), items...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// TestRerankCloseTopOnly runs the reranker only when the top sits
// close together with budget remaining.
func TestRerankCloseTopOnly(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")
	rr := &scriptReranker{}
	svc.rerank = rr

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Rerank close trio alpha beta gamma"})
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Rerank close trio alpha beta delta"})
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Statement: "Rerank close trio alpha beta epsilon"})
	ret, err := svc.Retrieve(context.Background(), Query{Text: "rerank close trio alpha beta", Scopes: []string{"general"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ret.Items) < 3 {
		t.Fatalf("items = %d", len(ret.Items))
	}
	if rr.calls != 1 {
		t.Fatalf("rerank calls = %d, want 1 for a close top", rr.calls)
	}
}

// TestConcurrentFragmentEditsSerializes proves the porter serializes
// two runs on the same fragment: one applies, the other gets the
// current text back (Section 10.2). Test files may launch
// goroutines; the bare-go guard covers non-test code only.
func TestConcurrentFragmentEditsSerializes(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	docID, err := svc.ResolveDoc(context.Background(), "general", DocNotebook)
	if err != nil {
		t.Fatal(err)
	}
	frag, err := svc.AppendFragment(context.Background(), []string{"general"}, docID, "notes", "Shared note", "")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() {
		_, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
			Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "Writer one"})
		results <- err
	}()
	go func() {
		_, err := svc.EditFragment(context.Background(), []string{"general"}, DocEdit{
			Doc: docID, FragmentID: frag.ID, ExpectedHash: frag.Hash, Op: OpReplace, Text: "Writer two"})
		results <- err
	}()
	var ok, conflict int
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			conflict++
		} else {
			ok++
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("applied=%d conflicts=%d, want 1 and 1", ok, conflict)
	}
}

// stubVisionTool is a scripted vision_extract for ingest tests.
type stubVisionTool struct{}

func (stubVisionTool) Definition() tools.Definition {
	return tools.Definition{Name: "vision_extract"}
}

func (stubVisionTool) Invoke(_ context.Context, _ tools.Invocation) (tools.Result, error) {
	raw, _ := json.Marshal(map[string]any{"pages": []any{
		map[string]any{"page": 1, "text": "Scanned invoice total forty two."},
		map[string]any{"page": 2, "text": "Scanned signature block here."},
	}})
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
}

func (stubVisionTool) ResourceKeys(_ json.RawMessage) ([]string, error) { return nil, nil }

// TestVisionIngestScannedPDF covers the vision path for scanned
// documents with page anchors (Section 10.9 step 2).
func TestVisionIngestScannedPDF(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := NewService(db,
		WithSettings(testSettings()),
		WithClock(func() time.Time { return now }),
		WithEmbedder(NewFakeEmbedder("fake-test", 512)),
		WithScopes("general"),
		WithVision(&VisionToolAdapter{Tool: stubVisionTool{}}),
	)

	doc, err := svc.Ingest(context.Background(), []string{"general"}, "scan.pdf", "application/pdf", []byte("binary"),
		IngestOptions{Title: "Scan"})
	if err != nil {
		t.Fatal(err)
	}
	hits, _, err := svc.LibrarySearch(context.Background(), []string{"general"}, "", "invoice total", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("vision chunks unsearchable: %v", err)
	}
	if hits[0].Citation.Page != 1 || hits[0].Doc.ID != doc.ID {
		t.Fatalf("vision citation = %+v", hits[0].Citation)
	}
}
