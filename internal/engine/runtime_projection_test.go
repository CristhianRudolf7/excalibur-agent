package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestRuntimeSizingGuidanceCompactAndScoped(t *testing.T) {
	compile := func(autonomous bool, projection, early string) string {
		t.Helper()
		t.Setenv(tools.RuntimeProjectionSwitch, projection)
		t.Setenv(earlyDeliverablesSwitch, early)
		run := testRun(Profile{ExplicitFinish: autonomous})
		b := &DefaultBuilder{Config: ctxcompiler.DefaultConfig(), Snapshot: testSnapshotFunc()}
		_, compiled, err := b.BuildRequest(context.Background(), run, false)
		if err != nil {
			t.Fatal(err)
		}
		return compiled.LayerText(ctxcompiler.LayerTask)
	}
	legacy := compile(true, "off", "")
	enabled := compile(true, "", "")
	for _, want := range []string{"time a small sample", "extrapolate the full run against remaining time and CPUs", "finishes with margin and keeps a valid output"} {
		if !strings.Contains(enabled, want) {
			t.Fatalf("missing %q in task guidance: %s", want, enabled)
		}
		if strings.Contains(legacy, want) {
			t.Fatalf("projection switch did not remove %q", want)
		}
	}
	if !strings.Contains(legacy, earlyDeliverablesGuidance) || !strings.Contains(enabled, earlyDeliverablesGuidance) {
		t.Fatal("existing early-output guidance lost")
	}
	// Measure actual compiled task bytes as well as the instruction, ensuring
	// the new advice extends one instruction instead of duplicating it.
	if delta := len(enabled) - len(legacy); delta != 179 {
		t.Fatalf("compiled guidance byte delta = %d, want 179", delta)
	}
	if size := len(earlyDeliverablesGuidance) + 1 + len(runtimeSizingGuidance); size != 414 {
		t.Fatalf("combined instruction size = %d, want 414", size)
	}
	for _, got := range []string{compile(false, "", ""), compile(true, "", "off")} {
		if strings.Contains(got, runtimeSizingGuidance) {
			t.Fatal("sizing guidance escaped the autonomous early-output scope")
		}
	}
}

type runtimeContextRunner struct {
	*fakeRunner
	ctx context.Context
}

func (r *runtimeContextRunner) Execute(ctx context.Context, name string, args json.RawMessage, opts tools.ExecuteOptions) (tools.Result, error) {
	r.ctx = ctx
	return r.fakeRunner.Execute(ctx, name, args, opts)
}

func TestRuntimeProjectionRunBudgetContext(t *testing.T) {
	t.Setenv(DeadlineSwitch, "off")
	// Classification must not depend on whether assumed time reserves are on.
	t.Setenv(TimeReserveExplicitOnlySwitch, "off")
	for _, tc := range []struct {
		name, source string
		autonomous   bool
		wantTotal    time.Duration
	}{
		{"explicit", "explicit", true, time.Hour},
		{"estimated", "default", true, 0},
		{"assistant", "explicit", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := scriptedRunner()
			e, _ := testEngine(&fakeEndpoint{}, fake, passingGate())
			runner := &runtimeContextRunner{fakeRunner: fake}
			e.deps.Tools = runner
			run := testRun(Profile{ExplicitFinish: tc.autonomous, WallSource: tc.source})
			run.Tracker.Limit.MaxSeconds = 3600
			e.runOne(context.Background(), run, toolCall("probe", "probe", `{}`))
			want := tools.WithGitHistoryRunID(tools.WithRunInstruction(context.Background(), run.Request), string(run.ID))
			if tc.autonomous {
				want = tools.WithRuntimeBudget(want, run.StartedAt, tc.wantTotal)
			}
			if !reflect.DeepEqual(runner.ctx, want) {
				t.Fatalf("runtime budget context differs for %s", tc.name)
			}
		})
	}
}
