package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestReviewReuseAuditRecordsVersionedHitsAndMisses(t *testing.T) {
	model := &fakeEndpoint{responses: []providers.ChatResponse{verifierPassResp(), verifierPassResp(), verifierPassResp()}}
	e, store := testEngine(model, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	e.AttachJournal(r.ID, store)
	r.Workdir = t.TempDir()
	path := filepath.Join(r.Workdir, "result.py")
	const contents = "private-file-body-do-not-export"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	r.Request = "Create " + path + "; private-instruction-do-not-export"
	history := []tasks.HistoryTool{{Name: "exec", Args: `{"command":"python3 result.py"}`, Output: "private-tool-output-do-not-export", Dependencies: map[string]string{path: fileVersion(path)}, SessionStates: map[string]tasks.SessionState{"sess-1": {Alive: true}}}}
	drafts := []tasks.DraftCriterion{{ID: "D1", Key: path, Method: types.CriterionCommand}}
	check := func() {
		t.Helper()
		if ok, why := e.runStrongVerifier(context.Background(), r, *verifyConfig(), drafts, history, "done"); !ok {
			t.Fatal(why)
		}
	}
	check()
	check()
	history[0].SessionStates["sess-1"] = tasks.SessionState{Alive: false}
	check()
	r.mu.Lock()
	r.appendDirectiveLocked("additional instruction", nil)
	r.mu.Unlock()
	check()
	rows, err := store.List(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var audit []reviewReuseAudit
	for _, row := range rows {
		if row.Type != journal.EntryType("review_reuse") {
			continue
		}
		for _, private := range []string{contents, "private-instruction-do-not-export", "private-tool-output-do-not-export"} {
			if strings.Contains(string(row.Payload), private) {
				t.Fatalf("audit exposed private content: %s", private)
			}
		}
		var a reviewReuseAudit
		if err := json.Unmarshal(row.Payload, &a); err != nil {
			t.Fatal(err)
		}
		audit = append(audit, a)
	}
	if len(audit) != 4 || audit[0].Hit || !audit[1].Hit || audit[2].Hit || audit[3].Hit {
		t.Fatalf("hit/miss audit: %+v", audit)
	}
	if audit[0].Key == "" || audit[0].Key != audit[1].Key || audit[0].Dependencies[path] != fileVersion(path) || audit[0].DependencyReceipts != 1 {
		t.Fatal("missing dependency identity/version")
	}
	if audit[0].SessionAuthorityVersion == audit[2].SessionAuthorityVersion || audit[2].RequestRevision == audit[3].RequestRevision {
		t.Fatal("session or request revision not audited")
	}
	t.Setenv("EXCALIBUR_REVIEW_REUSE_AUDIT", "0")
	check()
	after, _ := store.List(context.Background(), r.ID)
	count := 0
	for _, row := range after {
		if row.Type == journal.EntryType("review_reuse") {
			count++
		}
	}
	if count != 4 {
		t.Fatal("audit switch ignored")
	}
}
