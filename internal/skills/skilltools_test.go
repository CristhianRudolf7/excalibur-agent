package skills

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// The skill tools carry the Section 13.2 contract: fast, read-only,
// idempotent, and registered through the same Registry as every
// other tool (no privileged path).
func TestSkillToolsContract(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	for _, tool := range SkillTools(ix, nil) {
		def := tool.Definition()
		if err := def.Validate(); err != nil {
			t.Errorf("%s: bad definition: %v", def.Name, err)
		}
		if !def.Fast {
			t.Errorf("%s: skill tools must be fast (master-usable)", def.Name)
		}
		var schema map[string]any
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Errorf("%s: input schema must be JSON: %v", def.Name, err)
		}
	}
	reg := tools.NewMapRegistry()
	for _, tool := range SkillTools(ix, nil) {
		if err := reg.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	if len(reg.Names()) != 2 {
		t.Errorf("want search_skills and load_skill, got %v", reg.Names())
	}
}

// search_skills returns name, group and path; load_skill returns
// the body plus declared tools without touching the stable prefix.
func TestSkillToolsInvoke(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	search, load := SkillTools(ix, nil)[0], SkillTools(ix, nil)[1]
	res, err := search.Invoke(ctx, tools.Invocation{Tool: "search_skills", Arguments: json.RawMessage(`{"query":"regression test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "fault-isolation") {
		t.Errorf("search_skills must find fault-isolation: %+v", res)
	}
	res, err = search.Invoke(ctx, tools.Invocation{Tool: "search_skills", Arguments: json.RawMessage(`{"query":"","group":"testing"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "test-driven-development") {
		t.Errorf("empty query with group must list the group: %+v", res)
	}
	res, err = load.Invoke(ctx, tools.Invocation{Tool: "load_skill", Arguments: json.RawMessage(`{"name":"no-such-skill"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusInputError {
		t.Errorf("unknown skill must be an input error: %+v", res)
	}
	res, err = load.Invoke(ctx, tools.Invocation{Tool: "load_skill", Arguments: json.RawMessage(`{"name":"test-driven-development"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != tools.StatusOK || !strings.Contains(res.Output, "ailing-first tests define done") {
		t.Errorf("load_skill must return the body: %+v", res)
	}
	if !strings.Contains(res.Output, "Deferred tools") {
		t.Errorf("load_skill must list declared deferred tools: %q", res.Output)
	}
}

// The watcher picks up SKILL.md changes without a manual rescan.
func TestWatcherPicksUpChanges(t *testing.T) {
	r, _ := fixtureRoots(t)
	writeSkill(t, filepath.Join(r.User, "watched"), "watched", "Original text.", "planning")
	ix := NewIndex(r, nil)
	scanned := make(chan error, 8)
	w, err := ix.Watch(30*time.Minute, func(err error) { scanned <- err })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	<-scanned // initial scan
	snap := ix.Freeze(nil)
	writeSkill(t, filepath.Join(r.User, "watched"), "watched", "Changed text for the watcher.", "planning")
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-scanned:
			if err != nil {
				t.Fatalf("rescan: %v", err)
			}
			if len(snap.Delta(ix)) > 0 {
				got, _ := ix.Get("watched")
				if strings.Contains(got.Description, "Changed") {
					return
				}
			}
		case <-deadline:
			t.Fatal("watcher did not pick up the SKILL.md change within 10s")
		}
	}
}

// Archived skills are excluded from search, suggest and load.
func TestArchivedExcluded(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if err := ix.SetState("root-cause-first", StateArchived); err != nil {
		t.Fatal(err)
	}
	for _, h := range ix.Search("root cause error evidence", "") {
		if h.Name == "root-cause-first" {
			t.Error("archived skills must not appear in search")
		}
	}
	for _, s := range ix.Suggest("root cause error evidence debugging", "m", 3) {
		if s.Name == "root-cause-first" {
			t.Error("archived skills must not be suggested")
		}
	}
	if _, err := ix.Load("root-cause-first"); err == nil {
		t.Error("archived skills must not load")
	}
	if err := ix.SetState("root-cause-first", "bogus"); err == nil {
		t.Error("unknown states must be rejected")
	}
}

// Suggestion reasons reference the query; group filtering works.
func TestSearchGroupFilter(t *testing.T) {
	ix := NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	hits := ix.Search("", "planning")
	if len(hits) != 7 {
		t.Errorf("want 7 planning skills, got %d", len(hits))
	}
	for _, h := range hits {
		if h.Group != "planning" {
			t.Errorf("group filter leaked %q", h.Name)
		}
	}
}
