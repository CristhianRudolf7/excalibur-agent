package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
)

func TestDelegationDocumentToolGrants(t *testing.T) {
	for _, tc := range []struct {
		name      string
		attached  bool
		requested []string
		remove    []string
		wantView  bool
	}{
		{name: "attachments", attached: true, wantView: true},
		{name: "no attachments"},
		{name: "explicit writes", attached: true, requested: []string{"write_file"}},
		{name: "explicit search", attached: true, requested: []string{"library_search"}},
		{name: "profile removes library", attached: true, remove: []string{"library_search"}},
		{name: "profile removes view", attached: true, remove: []string{"library_view"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := agents.Builtins[agents.ProfileImplementer]
			profile.Remove = append([]string(nil), tc.remove...)
			names, err := agents.ClampTools(profile, 2, tc.requested)
			if err != nil {
				t.Fatal(err)
			}
			allowed := map[string]bool{}
			for _, name := range names {
				allowed[name] = true
			}
			allowDelegationDocumentTools(allowed, profile, 2, tc.requested, tc.attached)
			if allowed["library_view"] != tc.wantView {
				t.Fatalf("library_view grant = %v", allowed["library_view"])
			}
			if len(tc.requested) > 0 && len(allowed) != len(tc.requested) {
				t.Fatalf("explicit tools expanded: %v", allowed)
			}
			if !profile.Has("library_search", 2) && allowed["library_search"] {
				t.Fatal("profile library restriction bypassed")
			}
		})
	}
}

type delegationDocumentFixture struct {
	docs  map[string]memory.DocumentInfo
	pages map[string][]docextract.Page
}

func TestDelegationAttachmentFiles(t *testing.T) {
	ctx := context.Background()
	blobs := gateway.NewMemoryBlobs()
	original := "%PDF-fixture-original"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
	if err := blobs.Put(digest, strings.NewReader(original)); err != nil {
		t.Fatal(err)
	}
	f := delegationDocumentFixture{
		docs:  map[string]memory.DocumentInfo{"doc_tdr": {ID: "doc_tdr", SourceName: "TDR.pdf", Title: "Client TDR", BlobDigest: digest, PageCount: 1, Version: 1, Status: "ready"}},
		pages: map[string][]docextract.Page{"doc_tdr": {{Number: 1, Blocks: []docextract.Block{{Text: strings.Repeat("Inventory requirements. ", 3000) + "FINAL REQUIREMENT"}}}}},
	}
	for _, mode := range []string{"new", "existing", "symlink", "docs-symlink", "no-attachments", "pending", "no-blobs"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			fixture := f
			if mode == "pending" {
				fixture.docs = map[string]memory.DocumentInfo{"doc_tdr": {ID: "doc_tdr", SourceName: "TDR.pdf", BlobDigest: digest, Status: "pending"}}
			}
			outside := t.TempDir()
			if mode == "existing" || mode == "symlink" {
				if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"TDR.pdf", "TDR.md"} {
					path := filepath.Join(root, "docs", name)
					if mode == "symlink" {
						path = filepath.Join(outside, name)
						if err := os.Symlink(path, filepath.Join(root, "docs", name)); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
					}
					if err := os.WriteFile(path, []byte("user content"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "docs-symlink" {
				if err := os.Symlink(outside, filepath.Join(root, "docs")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			ids := []string{"doc_tdr"}
			if mode == "no-attachments" {
				ids = nil
			}
			var source gateway.BlobStore = blobs
			if mode == "no-blobs" {
				source = nil
			}
			got := delegationAttachmentSection(ctx, fixture, ids, source, root)
			if mode == "no-attachments" {
				if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) || got != "" {
					t.Fatal("no attachments created docs or changed prompt")
				}
				return
			}
			if mode == "docs-symlink" {
				files, err := os.ReadDir(outside)
				if err != nil || len(files) != 0 || !strings.Contains(got, "Could not open") || !strings.Contains(got, "Inventory requirements") {
					t.Fatal("docs symlink escaped project or lost inline text")
				}
				return
			}
			if mode == "pending" {
				if _, err := os.Stat(filepath.Join(root, "docs", "TDR.md")); !os.IsNotExist(err) || !strings.Contains(got, "No extracted text") {
					t.Fatal("pending extraction saved an empty Markdown file")
				}
				return
			}
			for _, name := range []string{"TDR.pdf", "TDR.md"} {
				data, err := os.ReadFile(filepath.Join(root, "docs", name))
				if mode == "no-blobs" && name == "TDR.pdf" {
					if !os.IsNotExist(err) {
						t.Fatal("created empty original without blob")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				if mode == "existing" || mode == "symlink" {
					if string(data) != "user content" || !strings.Contains(got, "Existing path retained") {
						t.Fatalf("existing %s changed", name)
					}
				} else if name == "TDR.pdf" && string(data) != original {
					t.Fatal("original changed")
				} else if name == "TDR.md" && !strings.HasSuffix(string(data), "FINAL REQUIREMENT\n\n") {
					t.Fatal("Markdown file did not receive full extraction")
				}
			}
			if !strings.Contains(got, "Inventory requirements") || !strings.Contains(got, "docs/TDR.md") {
				t.Fatal("missing inline text or extraction path")
			}
		})
	}
}

func (f delegationDocumentFixture) GetDocument(_ context.Context, scopes []string, id string) (memory.DocumentInfo, error) {
	// The real memory service has no default scope and rejects nil scopes.
	if len(scopes) == 0 {
		return memory.DocumentInfo{}, errors.New("no scope")
	}
	if doc, ok := f.docs[id]; ok {
		return doc, nil
	}
	return memory.DocumentInfo{}, errors.New("not found")
}

func (f delegationDocumentFixture) DocumentPages(_ context.Context, scopes []string, id string, _, from, to int) ([]docextract.Page, error) {
	if len(scopes) == 0 {
		return nil, errors.New("no scope")
	}
	var pages []docextract.Page
	for _, p := range f.pages[id] {
		if p.Number >= from && p.Number <= to {
			pages = append(pages, p)
		}
	}
	return pages, nil
}

func TestDelegationDocumentIDs(t *testing.T) {
	history := []master.Normalized{{Role: "user", Text: "Old [document:doc_old]"}}
	for i := 0; i < 5; i++ {
		history = append(history, master.Normalized{Role: "user", Text: fmt.Sprintf("Attached: TDR.pdf [document:doc_%d]", i)}, master.Normalized{Role: "assistant", Text: "[document:doc_ignore]"})
	}
	history[len(history)-2].Attachments = []string{"doc_4"}
	got := delegationDocumentIDs(history, "Use doc_old and [document:doc_4]")
	want := []string{"doc_old", "doc_4", "doc_3", "doc_2", "doc_1", "doc_0"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v, want %v", got, want)
	}
	if got := delegationDocumentIDs(history, ""); strings.Contains(strings.Join(got, ","), "doc_old") {
		t.Fatalf("included attachment outside five user turns: %v", got)
	}
	if got := delegationDocumentIDs([]master.Normalized{{Text: "Attached: Old.pdf (doc_legacy)"}}, ""); !reflect.DeepEqual(got, []string{"doc_legacy"}) {
		t.Fatalf("legacy attachment: %v", got)
	}
	if got := delegationDocumentIDs(nil, strings.Repeat("doc_repeat ", 30)); len(got) != 1 {
		t.Fatalf("duplicates: %v", got)
	}
	var many []string
	for i := 0; i < 30; i++ {
		many = append(many, fmt.Sprintf("doc_%d", i))
	}
	if got := delegationDocumentIDs(nil, strings.Join(many, " ")); len(got) != delegationAttachmentLimit {
		t.Fatalf("document count limit: %d", len(got))
	}
}

func TestDelegationAttachmentNames(t *testing.T) {
	for source, want := range map[string]string{
		"../../TDR.pdf":                   "TDR.pdf",
		`C:\uploads\TDR.pdf`:              "TDR.pdf",
		"CON.pdf":                         "attachment-CON.pdf",
		"COM1.txt":                        "attachment-COM1.txt",
		"odd:name?\n.pdf":                 "odd_name__.pdf",
		"":                                "doc_fixture.bin",
		"..":                              "doc_fixture.bin",
		strings.Repeat("界", 100) + ".pdf": strings.Repeat("界", 40) + ".pdf",
	} {
		if got := delegationAttachmentName(memory.DocumentInfo{ID: "doc_fixture", SourceName: source}); got != want {
			t.Errorf("name(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestDelegationAttachmentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := delegationDocumentFixture{
		docs: map[string]memory.DocumentInfo{"doc_cancelled": {ID: "doc_cancelled", SourceName: "TDR.pdf", PageCount: 1, Version: 1}},
	}
	root := t.TempDir()
	got := delegationAttachmentSection(ctx, f, []string{"doc_cancelled"}, nil, root)
	if !strings.Contains(got, "Could not load or save") {
		t.Fatalf("cancellation omitted: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "TDR.md")); !os.IsNotExist(err) {
		t.Fatal("failed extraction left a partial file")
	}
}

type failedDelegationWriter struct{}

func (failedDelegationWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestDelegationDocumentTextSurvivesWriteFailure(t *testing.T) {
	f := delegationDocumentFixture{pages: map[string][]docextract.Page{
		"doc_tdr": {{Number: 1, Blocks: []docextract.Block{{Text: "First requirement"}, {Text: "Second requirement"}}}},
	}}
	text, truncated, err := delegationDocumentText(context.Background(), f, memory.DocumentInfo{ID: "doc_tdr", PageCount: 1, Version: 1}, 40000, failedDelegationWriter{})
	if !errors.Is(err, io.ErrClosedPipe) || truncated || text != "First requirement\n\nSecond requirement\n\n" {
		t.Fatalf("write failure discarded inline text: %q, %v, %v", text, truncated, err)
	}
}

func TestDelegationAttachmentSection(t *testing.T) {
	ctx := context.Background()
	if got := delegationAttachmentSection(ctx, nil, nil, nil, ""); got != "" {
		t.Fatalf("no attachments changed prompt: %q", got)
	}
	f := delegationDocumentFixture{docs: map[string]memory.DocumentInfo{}, pages: map[string][]docextract.Page{}}
	var ids []string
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("doc_%d", i)
		ids = append(ids, id)
		f.docs[id] = memory.DocumentInfo{ID: id, Title: "Client TDR", SourceName: "TDR.pdf", PageCount: 21, Version: 1, Status: "ready"}
		f.pages[id] = []docextract.Page{{Number: 21, Blocks: []docextract.Block{{Text: strings.Repeat(string(rune('界'+i)), 45000)}}}}
	}
	got := delegationAttachmentSection(ctx, f, ids, nil, "")
	for _, want := range []string{"Attached documents", "Client TDR", "TDR.pdf", "doc_0", "doc_1", "doc_2", "truncated"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if !utf8.ValidString(got) || strings.Count(got, "界") != 40000 || strings.Count(got, string('界'+1)) != 40000 || strings.Count(got, string('界'+2)) != 0 {
		t.Fatal("per-document or total Unicode character limit failed")
	}
	if len([]rune(got)) > delegationAttachmentChars+2000 {
		t.Fatal("attachment section exceeded text budget plus metadata")
	}
	got = delegationAttachmentSection(ctx, f, []string{"doc_missing"}, nil, "")
	if !strings.Contains(got, "doc_missing: unavailable") {
		t.Fatalf("missing document omitted: %q", got)
	}
	if got := delegationAttachmentSection(ctx, nil, []string{"doc_missing"}, nil, ""); !strings.Contains(got, "library unavailable") {
		t.Fatalf("missing library: %q", got)
	}
}
