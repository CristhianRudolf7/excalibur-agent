package fake_test

import (
	"context"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler/fake"
)

func TestFakes(t *testing.T) {
	ctx := context.Background()
	mem := &fake.FakeMemorySource{Memories: []ctxcompiler.MemoryHit{
		{Text: "a", Tokens: 100},
		{Text: "b", Tokens: 100},
	}}
	got, err := mem.Recall(ctx, "crash", []string{"general"}, 150)
	if err != nil || len(got) != 1 {
		t.Fatalf("recall must respect the budget: %v %v", got, err)
	}
	sk := &fake.FakeSkillSource{
		Catalog:     "planning -- 1 skill\n",
		Suggestions: []ctxcompiler.SkillSuggestion{{Name: "s1"}, {Name: "s2"}, {Name: "s3"}, {Name: "s4"}},
	}
	sug, err := sk.Suggest(ctx, "fix", 3)
	if err != nil || len(sug) != 3 {
		t.Fatalf("suggest must cap at max: %v %v", sug, err)
	}
	if cat, err := sk.CatalogSnippet(ctx, "child", 100); err != nil || cat == "" {
		t.Fatalf("catalog snippet: %q %v", cat, err)
	}
	tools := &fake.FakeToolSource{Tools: []ctxcompiler.CoreTool{{Name: "exec"}}}
	if defs, err := tools.CoreTools(ctx, "child"); err != nil || len(defs) != 1 {
		t.Fatalf("core tools: %v %v", defs, err)
	}

	sum := &fake.FakeSummarizer{Summaries: []string{"seg"}}
	merged, err := sum.Summarize(ctx, "discarded", "prev")
	if err != nil || merged != "prev\nseg" {
		t.Fatalf("fake summary must fuse: %q %v", merged, err)
	}

	store := fake.NewMemContentStore()
	store.Store("tc_1", "exact bytes")
	if out, err := store.Load("tc_1"); err != nil || out != "exact bytes" {
		t.Fatalf("store must return exact bytes: %q %v", out, err)
	}
	if _, err := store.Load("tc_nope"); err == nil {
		t.Fatal("unknown call ID must fail")
	}

	hist := &fake.MemHistoryStore{}
	hist.Append([]ctxcompiler.ActivityEvent{{Seq: 1, Content: "needle in haystack"}})
	if got := hist.Search("needle"); len(got) != 1 {
		t.Fatalf("search must find the archived turn: %v", got)
	}
	if got := hist.Search("missing"); len(got) != 0 {
		t.Fatalf("search must miss: %v", got)
	}
}
