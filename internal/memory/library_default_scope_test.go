package memory

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// A daemon memory service has no bound scopes; library tools must still
// reach documents uploaded to the general scope.
func TestLibraryToolsDefaultToGeneralScope(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	s := newTestService(t, db, time.Now())
	d, _, err := s.CreatePendingDocument(ctx, "general", DocMeta{BlobDigest: "digest-default", Title: "Robins"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestExtracted(ctx, "general", d.ID, extractionFixture()); err != nil {
		t.Fatal(err)
	}
	list := &libraryBrowseTool{name: ToolLibraryList, svc: s}
	res, err := list.Invoke(ctx, tools.Invocation{Tool: ToolLibraryList, Arguments: json.RawMessage(`{}`)})
	if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, d.ID) {
		t.Fatalf("library_list without scopes: %v %+v", err, res)
	}
	search := &librarySearchTool{svc: s}
	res, err = search.Invoke(ctx, tools.Invocation{Tool: ToolLibrarySearch, Arguments: json.RawMessage(`{"query":"robins hedges"}`)})
	if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, "[[doc:"+d.ID) {
		t.Fatalf("library_search without scopes: %v %+v", err, res)
	}
	grep := &libraryBrowseTool{name: ToolLibraryGrep, svc: s}
	res, err = grep.Invoke(ctx, tools.Invocation{Tool: ToolLibraryGrep, Arguments: json.RawMessage(`{"pattern":"hedges","run_id":"run_grep"}`)})
	if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, "[[doc:"+d.ID) || !strings.Contains(res.Output, "p. xii") {
		t.Fatalf("library_grep must return citable refs: %v %+v", err, res)
	}
	var out struct {
		Matches []struct {
			Ref string `json:"ref"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(res.Output), &out); err != nil || len(out.Matches) == 0 {
		t.Fatalf("grep output: %v %s", err, res.Output)
	}
	valid, dropped := s.ValidateCitations(ctx, []string{out.Matches[0].Ref}, []string{"run_grep"})
	if len(valid) != 1 || len(dropped) != 0 {
		t.Fatalf("grep reads must validate: %+v %v", valid, dropped)
	}
	view := &libraryBrowseTool{name: ToolLibraryView, svc: s}
	res, err = view.Invoke(ctx, tools.Invocation{Tool: ToolLibraryView, Arguments: json.RawMessage(`{"document_id":"` + d.ID + `","pages":"2","run_id":"run_view"}`)})
	if err != nil || res.Status != tools.StatusOK || !strings.Contains(res.Output, `"citable"`) || !strings.Contains(res.Output, "[[doc:"+d.ID) {
		t.Fatalf("library_view must return citable refs: %v %+v", err, res)
	}
}
