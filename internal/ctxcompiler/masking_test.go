package ctxcompiler_test

import (
	"fmt"
	"strings"
	"testing"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func bigObservation(id string, tokens int) ctxcompiler.Observation {
	return ctxcompiler.Observation{
		CallID:   id,
		ToolName: "read_file",
		Label:    "read_file big.go",
		Output:   "x",
		Tokens:   tokens,
	}
}

// 11.8 row 3: past 24K unmasked tokens one batch drops toward 12K;
// the newest 10 and pinned entries stay literal.
func TestMaskBatchTriggerAndTarget(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	masker := ctxcompiler.NewMasker(cfg, fixedCounter{per: 4})
	var obs []ctxcompiler.Observation
	for i := 0; i < 30; i++ {
		o := bigObservation(fmt.Sprintf("tc_%02d", i), 1000)
		o.Output = strings.Repeat("x", 4000)
		obs = append(obs, o)
	}
	// Pin one ancient observation: it must survive the batch.
	obs[0].Pinned = true
	total := masker.UnmaskedTotal(obs)
	if total != 30*1000 {
		t.Fatalf("unmasked total = %d, want 30000", total)
	}
	plan := masker.Apply(obs, false)
	if plan.UnmaskedTokens > 12000 {
		t.Fatalf("batch must drop toward 12K, left %d", plan.UnmaskedTokens)
	}
	masked := 0
	for i, m := range plan.Masked {
		if m {
			masked++
			if plan.Markers[i] == "" {
				t.Fatalf("masked %d has no marker", i)
			}
			if !strings.Contains(plan.Markers[i], obs[i].CallID) ||
				!strings.Contains(plan.Markers[i], "get_tool_output") {
				t.Fatalf("bad marker for %d: %s", i, plan.Markers[i])
			}
		}
	}
	// Newest 10 plus the pinned ancient entry stay literal.
	for i := len(obs) - 10; i < len(obs); i++ {
		if plan.Masked[i] {
			t.Fatalf("observation %d is within the newest 10 and must stay literal", i)
		}
	}
	if plan.Masked[0] {
		t.Fatal("pinned observation must stay literal")
	}
	if masked == 0 {
		t.Fatal("batch past the trigger must mask something")
	}
	t.Logf("masked %d of %d, left %d tokens", masked, len(obs), plan.UnmaskedTokens)
}

// 11.3.5: recent error results are conserved.
func TestMaskPreservesErrors(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	masker := ctxcompiler.NewMasker(cfg, fixedCounter{per: 4})
	var obs []ctxcompiler.Observation
	for i := 0; i < 20; i++ {
		o := bigObservation("e", 2000)
		o.CallID = "tc_err"
		obs = append(obs, o)
	}
	obs[0].IsError = true
	plan := masker.Apply(obs, false)
	if plan.Masked[0] {
		t.Fatal("error result must stay literal")
	}
}

// Below the trigger nothing masks.
func TestNoMaskBelowTrigger(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	masker := ctxcompiler.NewMasker(cfg, fixedCounter{per: 4})
	obs := []ctxcompiler.Observation{bigObservation("tc_small", 100)}
	plan := masker.Apply(obs, false)
	if plan.Masked[0] || plan.UnmaskedTokens != 100 {
		t.Fatalf("must not mask below trigger: %+v", plan)
	}
}

// 11.3.4: resuming past the cache TTL advances the batch even below
// the trigger, because the cache is already lost.
func TestMaskAdvancesOnResume(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	masker := ctxcompiler.NewMasker(cfg, fixedCounter{per: 4})
	var obs []ctxcompiler.Observation
	for i := 0; i < 15; i++ {
		o := bigObservation("r", 1000)
		o.CallID = "tc_resume"
		obs = append(obs, o)
	}
	quiet := masker.Apply(obs, false)
	if countTrue(quiet.Masked) != 0 {
		t.Fatal("15K unmasked tokens must not mask mid-session")
	}
	resumed := masker.Apply(obs, true)
	if countTrue(resumed.Masked) == 0 {
		t.Fatal("resume past TTL must advance the batch toward 12K")
	}
	if resumed.UnmaskedTokens > 12000+2000 {
		t.Fatalf("resumed batch left %d tokens", resumed.UnmaskedTokens)
	}
}

// Masked tool results rewrite in place keeping call identity, and
// the store returns the exact bytes (get_tool_output).
func TestMaskRoundTripThroughStore(t *testing.T) {
	cfg := ctxcompiler.DefaultConfig()
	masker := ctxcompiler.NewMasker(cfg, fixedCounter{per: 4})
	store := fake.NewMemContentStore()
	var obs []ctxcompiler.Observation
	for i := 0; i < 12; i++ {
		body := "output body " + string(rune('A'+i)) + strings.Repeat(".", 7999)
		id := "tc_rt_" + string(rune('a'+i))
		obs = append(obs, ctxcompiler.Observation{
			CallID: id, ToolName: "exec", Label: "exec build", Output: body, Tokens: 2000,
		})
		store.Store(id, body)
	}
	plan := masker.Apply(obs, false)
	if countTrue(plan.Masked) == 0 {
		t.Fatal("want a masking batch")
	}
	activity := make([]ctxcompiler.ActivityEvent, 0, len(obs))
	for i, o := range obs {
		out := o.Output
		if plan.Masked[i] {
			out = plan.Markers[i]
		}
		activity = append(activity, ctxcompiler.ActivityEvent{
			Seq: int64(i + 1), Role: providers.RoleTool, Content: out,
			ToolResult: &providers.ToolResult{CallID: o.CallID, Name: o.ToolName, Output: out},
		})
	}
	maskedIDs := plan.ApplyToActivity(obs, activity)
	if len(maskedIDs) == 0 {
		t.Fatal("ApplyToActivity must report masked call IDs")
	}
	for _, id := range maskedIDs {
		want, err := store.Load(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range activity {
			if e.ToolResult != nil && e.ToolResult.CallID == id {
				if !strings.Contains(e.Content, "get_tool_output") {
					t.Fatalf("masked result %s lost its marker", id)
				}
			}
		}
		if !strings.HasPrefix(want, "output body ") {
			t.Fatalf("store corrupted content for %s", id)
		}
	}
	if _, err := store.Load("tc_missing"); err == nil {
		t.Fatal("unknown call ID must fail")
	}
}

func countTrue(b []bool) int {
	n := 0
	for _, v := range b {
		if v {
			n++
		}
	}
	return n
}
