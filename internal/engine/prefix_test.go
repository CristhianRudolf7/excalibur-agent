package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestScriptedTwentyTurnPrefixStability(t *testing.T) {
	run := testRun(Profile{})
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
	// The offline driver models successive provider responses without network I/O.
	model := &fakeEndpoint{}
	var previous, previousFull []byte
	var fullIdentical, fullTotal int
	var identical, total int
	for turn := 0; turn < 20; turn++ {
		run.addFact(fmt.Sprintf("observed version %d", turn), 24)
		req, _, err := b.BuildRequest(context.Background(), run, false)
		if err != nil {
			t.Fatal(err)
		}
		response, _, err := model.Chat(context.Background(), types.RoleExecutor, req)
		if err != nil {
			t.Fatal(err)
		}
		full, _ := json.Marshal(req)
		if turn > 0 {
			common := 0
			for common < len(full) && common < len(previousFull) && full[common] == previousFull[common] {
				common++
			}
			fullIdentical += common
			fullTotal += len(full)
		}
		previousFull = full
		// L5 is the explicitly volatile control tail (Section 11.1).
		req.Messages = req.Messages[:len(req.Messages)-1]
		raw, _ := json.Marshal(req.Messages)
		if turn > 0 {
			// JSON's closing array delimiter is replaced by the appended suffix.
			prefix := previous[:len(previous)-1]
			if !strings.HasPrefix(string(raw), string(prefix)) {
				t.Fatalf("turn %d rewrote prior prefix", turn)
			}
			identical += len(prefix)
			total += len(raw)
		}
		previous = raw
		run.mu.Lock()
		run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: run.nextHistorySeqLocked(), Role: providers.RoleAssistant, Content: fmt.Sprintf("step %d: %s", turn, response.Message.Content)})
		run.mu.Unlock()
	}
	if model.calls() != 20 {
		t.Fatal("scripted provider did not see all turns")
	}
	t.Logf("full serialized request common-prefix fraction = %.4f (%d/%d)", float64(fullIdentical)/float64(fullTotal), fullIdentical, fullTotal)
	t.Logf("20-turn offline run: identical bytes / current request bytes = %.4f (%d/%d); prior stable prefix retained = 1.0000", float64(identical)/float64(total), identical, total)
}

func TestStableScopeAndToolDefinitions(t *testing.T) {
	r := testRun(Profile{})
	state := "old rule"
	defs := []providers.ToolDefinition{{Name: "exec", Description: "original", Schema: json.RawMessage(`{"type":"object"}`)}}
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: func(context.Context, *Run) (ctxcompiler.Snapshot, error) {
		return ctxcompiler.Snapshot{ProjectContext: state, Task: ctxcompiler.TaskView{OriginalRequest: "original instruction"}}, nil
	}, ToolDefs: func(*Run) []providers.ToolDefinition { return defs }}
	first, _, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	state = "updated rule"
	defs[0].Description = "changed"
	next, _, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if first.Messages[i].Content != next.Messages[i].Content {
			t.Fatal("prefix changed")
		}
	}
	if next.Tools[0].Description != "original" {
		t.Fatal("tool definition changed")
	}
	if !strings.Contains(next.Messages[len(next.Messages)-2].Content, "updated rule") {
		t.Fatal("scope update not appended")
	}
	t.Setenv("EXCALIBUR_STABLE_PREFIX", "0")
	legacy, _, err := b.BuildRequest(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(legacy.Messages[2].Content, "updated rule") || legacy.Tools[0].Description != "changed" {
		t.Fatal("legacy switch ignored")
	}
}
