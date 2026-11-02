package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestScriptedTwentyTurnPrefixStability(t *testing.T) {
	run := testRun(Profile{})
	b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
	// The offline driver models successive provider responses without network I/O.
	var previous []byte
	var identical, total int
	for turn := 0; turn < 20; turn++ {
		run.addFact(fmt.Sprintf("observed version %d", turn), 24)
		req, _, err := b.BuildRequest(context.Background(), run, false)
		if err != nil {
			t.Fatal(err)
		}
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
		run.History = append(run.History, ctxcompiler.ActivityEvent{Seq: run.nextHistorySeqLocked(), Role: providers.RoleAssistant, Content: fmt.Sprintf("step %d", turn)})
		run.mu.Unlock()
	}
	t.Logf("20-turn offline run: identical bytes / current request bytes = %.4f (%d/%d); prior stable prefix retained = 1.0000", float64(identical)/float64(total), identical, total)
}
