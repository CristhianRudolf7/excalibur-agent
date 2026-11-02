package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestEditScopePairs(t *testing.T) {
	for _, tc := range []struct {
		name, request, before, after string
		warn                         bool
	}{
		{"literal", "Replace `amber` with `<violet>`.", "color: amber\n", "color: <violet>\n", false},
		{"generic", "Replace selected values with placeholders.", "color: amber\n", "color: <violet>\n", false},
		{"preserve quotes", "Replace selected values with placeholders.", "color: 'amber'\n", "color: '<violet>'\n", false},
		{"empty value", "Replace selected values with placeholders.", "color: amber\n", "color: \n", false},
		{"empty quoted value", "Replace selected values with placeholders.", "color: 'amber'\n", "color: ''\n", false},
		{"unicode and CRLF", "Replace `amber` with `紫`.", "  color: \"amber\"\r\n", "  color: \"紫\"\r\n", false},
		{"explicit spaced value", "Replace `light amber` with `dark violet`.", "color: light amber\n", "color: dark violet\n", false},
		{"explicit quoted value", "Replace `amber` with `\"violet\"`.", "color: amber\n", "color: \"violet\"\n", false},
		{"rename", "Rename amber to violet.", "amber = 1\n", "violet = 1\n", false},
		{"dotted value", "Replace amber with palette.violet.", "color: amber\n", "color: palette.violet\n", false},
		{"added quotes", "Replace selected values with placeholders.", "color: amber\n", "color: '<violet>'\n", true},
		{"wrapped URL", "Replace selected values with placeholders.", "url: https://amber@example.invalid/path\n", "url: 'https://<violet>@example.invalid/path'\n", true},
		{"escape", "Replace selected values with placeholders.", "color: amber\n", "color: \\<violet\\>\n", true},
		{"moved quotes", "Replace selected values with placeholders.", "print 'amber' label\n", "print <violet> 'label'\n", true},
		{"whitespace reflow", "Replace selected values with placeholders.", "color:  amber\n", "color: <violet>\n", true},
		{"line reflow", "Replace selected values with placeholders.", "colors: [amber, green]\n", "colors:\n  - <violet>\n  - green\n", true},
		{"key order", "Replace selected values with placeholders.", "first: amber\nsecond: green\n", "second: green\nfirst: <violet>\n", true},
		{"extra prose", "Replace `amber` with `<violet>`.", "color: amber\n", "new color: <violet>\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeEditScope("settings.txt", tc.before, tc.after, tc.request)
			if (got.note != "") != tc.warn {
				t.Fatalf("warn=%v evidence=%+v", tc.warn, got)
			}
			if !strings.Contains(got.diff, "@@") || strings.Contains(got.diff+got.note, "amber") || strings.Contains(got.diff+got.note, "violet") {
				t.Fatal("expected hunk locations without raw values", got)
			}
			if tc.warn && !strings.HasPrefix(got.note, "edit changed text beyond the replaced values in settings.txt: ") {
				t.Fatal(got.note)
			}
		})
	}
}

func editScopeRun(t *testing.T, e *Engine) (*Run, string) {
	t.Helper()
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Replace selected values with placeholders in settings.txt."
	path := filepath.Join(r.Workdir, "settings.txt")
	writeFile := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("color: amber\n")
	r.SetInputCopyDir(filepath.Join(t.TempDir(), "originals"))
	e.startInputSnapshot(r)
	writeFile("color: '<violet>'\n")
	return r, path
}

func TestEditScopeEvidenceFlows(t *testing.T) {
	for _, checklist := range []string{"", "off"} {
		t.Run("checklist="+checklist, func(t *testing.T) {
			t.Setenv(editScopeSwitch, "")
			t.Setenv("EXCALIBUR_FINISH_CHECKLIST", checklist)
			model := &fakeEndpoint{responses: []providers.ChatResponse{finalResp(""), verifierPassResp()}}
			e, _ := testEngine(model, scriptedRunner(), passingGate())
			r, path := editScopeRun(t, e)
			note := "edit changed text beyond the replaced values in " + path + ":"
			if got := e.confirmationText(r); !strings.Contains(got, note) {
				t.Fatal("finish evidence missing scope advisory", got)
			}
			cfg := *verifyConfig()
			cfg.EnableGeneralizeChecks = false
			drafts := []tasks.DraftCriterion{{ID: "D1", Key: path, Method: types.CriterionInspection}}
			if ok, why := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, "done"); !ok {
				t.Fatal("audit must never refuse completion", why)
			}
			if len(model.requests) != 2 {
				t.Fatal("expected full and recovery review", len(model.requests))
			}
			for i, req := range model.requests {
				prompt := req.Messages[0].Content
				if !strings.Contains(prompt, note) || !strings.Contains(prompt, "@@ -1,1 +1,1 @@") {
					t.Fatalf("review %d missing scope evidence: %s", i, prompt)
				}
				if strings.Contains(prompt, "amber") || strings.Contains(prompt, "violet") {
					t.Fatal("audit exposed a replaced value")
				}
			}
		})
	}
}

func TestEditScopeEligibility(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r, path := editScopeRun(t, e)
	if got := e.editScopeAudit(r, r.Request); len(got) != 1 || got[0].note == "" {
		t.Fatal("default audit absent", got)
	}
	for _, off := range []string{"off", "0", "false", "no"} {
		t.Setenv(editScopeSwitch, off)
		if got := e.editScopeAudit(r, r.Request); len(got) != 0 {
			t.Fatal("disabled audit", got)
		}
	}
	t.Setenv(editScopeSwitch, "")
	r.Profile.ExplicitFinish = false
	if got := e.editScopeAudit(r, r.Request); len(got) != 0 {
		t.Fatal("assistant audit", got)
	}
	r.Profile.ExplicitFinish = true
	if got := e.editScopeAudit(r, "Implement a new color picker."); len(got) != 0 {
		t.Fatal("unrelated request", got)
	}
	// New files have no original and are excluded even when their content is
	// similar to a changed input. Unchanged originals produce no summaries.
	if err := os.WriteFile(filepath.Join(r.Workdir, "new.txt"), []byte("color: '<violet>'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("color: amber\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := e.editScopeAudit(r, r.Request); len(got) != 0 {
		t.Fatal("new/unchanged files included", got)
	}
	r.fin.snapshot.Entries[path] = tasks.InputEntry{Path: path}
	if got := e.editScopeAudit(r, r.Request); len(got) != 0 {
		t.Fatal("metadata-only original included", got)
	}
	r.fin.snapshot = nil
	if got := e.editScopeAudit(r, r.Request); len(got) != 0 {
		t.Fatal("missing snapshot", got)
	}
}

func TestEditScopeHunkBounds(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&before, "key%d: amber\nunchanged%d\n", i, i)
		value := "<violet>"
		if i == 39 {
			value = "'<violet>'"
		}
		fmt.Fprintf(&after, "key%d: %s\nunchanged%d\n", i, value, i)
	}
	got := summarizeEditScope("settings.txt", before.String(), after.String(), "Replace selected values with placeholders.")
	if lines := strings.Count(got.diff, "\n"); lines > editScopeMaxLines {
		t.Fatal("diff line cap", lines)
	}
	if strings.Contains(got.diff, "unchanged") || strings.Contains(got.diff, "key") || !strings.Contains(got.note, "line 79") {
		t.Fatal("must omit unchanged context and still audit hunks after summary limit", got)
	}
}

func TestEditScopeReadBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.txt")
	for _, body := range []string{strings.Repeat("x", editScopeFileBytes+1), "binary\x00data", strings.Repeat("\n", editScopeFileLines)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		remaining := int64(editScopeReadBytes)
		if _, ok := readEditScopeFile(path, &remaining); ok {
			t.Fatal("out-of-bounds input accepted")
		}
	}
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	remaining := int64(2)
	if _, ok := readEditScopeFile(path, &remaining); ok {
		t.Fatal("total read budget exceeded")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	remaining = editScopeReadBytes
	if _, ok := readEditScopeFile(link, &remaining); ok {
		t.Fatal("symlink accepted")
	}
}

func TestEditScopeFileCountBound(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	r.Workdir = t.TempDir()
	r.Request = "Replace selected values with placeholders."
	for i := 0; i < editScopeMaxFiles+2; i++ {
		path := filepath.Join(r.Workdir, fmt.Sprintf("settings-%02d.txt", i))
		if err := os.WriteFile(path, []byte("color: amber\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r.SetInputCopyDir(filepath.Join(t.TempDir(), "originals"))
	e.startInputSnapshot(r)
	for path := range r.fin.snapshot.Entries {
		if err := os.WriteFile(path, []byte("color: '<violet>'\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got := e.editScopeAudit(r, r.Request); len(got) != editScopeMaxFiles {
		t.Fatal("changed-file bound", len(got))
	}
}

func TestEditScopeChangesInvalidateReviewReuse(t *testing.T) {
	t.Setenv("EXCALIBUR_VERDICT_REUSE", "on")
	model := &fakeEndpoint{responses: []providers.ChatResponse{verifierPassResp(), verifierPassResp()}}
	e, _ := testEngine(model, scriptedRunner(), passingGate())
	r, path := editScopeRun(t, e)
	// No path in the request/history: the audit must supply its own cache
	// identity rather than rely on the deliverable/dependency discovery rules.
	r.Request = "Replace selected values with placeholders."
	cfg := *verifyConfig()
	cfg.EnableGeneralizeChecks = false
	drafts := []tasks.DraftCriterion{{ID: "D1", Statement: "Replace selected values", Method: types.CriterionInspection}}
	check := func() {
		t.Helper()
		if ok, why := e.runStrongVerifier(context.Background(), r, cfg, drafts, nil, "done"); !ok {
			t.Fatal(why)
		}
	}
	check()
	check()
	if len(model.requests) != 1 {
		t.Fatal("unchanged audit should reuse review", len(model.requests))
	}
	if err := os.WriteFile(path, []byte("color: <violet>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check()
	if len(model.requests) != 2 {
		t.Fatal("changed audit reused stale review", len(model.requests))
	}
}
