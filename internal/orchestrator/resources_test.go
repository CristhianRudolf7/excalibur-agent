package orchestrator_test

import (
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Section 5.2: over the threshold the watchdog stops admitting and
// pauses maintenance first; recovery resumes one by one with
// hysteresis and never wakes user-paused runs; the last resort kills
// the largest tool process of the lowest-priority run.

func TestWatchdogShedAndHysteresis(t *testing.T) {
	f := newFixture(t)
	kid := func(name string, lane orchestrator.Lane) orchestrator.RunRecord {
		card := childCard(name, agents.ProfileImplementer)
		card.Lane = lane
		_, rec := f.spawnOk(f.parent.ID, card)
		f.attach(rec)
		return rec
	}
	// The fixture parent sits out user-paused: the shed chooses
	// among the children by lane, oldest heartbeat first.
	if err := f.o.Pause(f.prun.Run.ID, types.PauseUser, "test"); err != nil {
		t.Fatal(err)
	}
	p3 := kid("bg", orchestrator.LaneP3)
	p1 := kid("fg", orchestrator.LaneP1)
	rss := uint64(0)
	f.o.SetHooks(orchestrator.Hooks{Sampler: func() (uint64, error) { return rss, nil }})
	f.o.SetRAMThreshold(1000)

	// Over the threshold: maintenance (P3) pauses first.
	rss = 1200
	paused, killed := f.o.WatchdogTick(rss, f.now.Unix())
	if len(paused) != 1 || paused[0] != p3.Run.ID {
		t.Fatalf("watchdog must pause P3 first: %v %q", paused, killed)
	}
	if got := runState(t, f.store, p3.Run.ID); got != types.RunPaused {
		t.Fatalf("P3 must be paused, got %q", got)
	}
	rec, _ := f.store.GetRun(p3.Run.ID)
	if rec.Run.PauseReason == nil || *rec.Run.PauseReason != types.PauseResource {
		t.Fatal("pause reason must be resource")
	}
	// Still over: now P1 pauses too.
	paused, _ = f.o.WatchdogTick(rss, f.now.Unix())
	if len(paused) != 1 || paused[0] != p1.Run.ID {
		t.Fatalf("watchdog must pause P1 next: %v", paused)
	}
	// New runs stay queued while halted.
	_, q := f.spawnOk(f.parent.ID, childCard("later", agents.ProfileImplementer))
	if runState(t, f.store, q.Run.ID) != types.RunQueued {
		t.Fatal("admission must halt under memory pressure")
	}
	// A dip below the threshold does not resume on the first sample:
	// the low watermark must hold 60 s (hysteresis). P1 resumes
	// first, one by one.
	rss = 700
	if _, _ = f.o.WatchdogTick(rss, f.now.Unix()); true {
	}
	if got := runState(t, f.store, p1.Run.ID); got != types.RunPaused {
		t.Fatalf("hysteresis must hold the pause, got %q", got)
	}
	f.advance(61 * time.Second)
	paused, _ = f.o.WatchdogTick(rss, f.now.Unix())
	if len(paused) != 1 || paused[0] != p1.Run.ID {
		t.Fatalf("hysteresis must resume P1 first: %v", paused)
	}
	if got := runState(t, f.store, p1.Run.ID); got != types.RunRunning {
		t.Fatalf("P1 must be running again, got %q", got)
	}
}

func TestWatchdogNeverResumesUserPaused(t *testing.T) {
	f := newFixture(t)
	_, rec := f.spawnOk(f.parent.ID, childCard("u", agents.ProfileImplementer))
	_ = f.attach(rec)
	if err := f.o.Pause(rec.Run.ID, types.PauseUser, "user-1"); err != nil {
		t.Fatal(err)
	}
	rss := uint64(10)
	f.o.SetHooks(orchestrator.Hooks{Sampler: func() (uint64, error) { return rss, nil }})
	f.o.SetRAMThreshold(1000)
	f.advance(120 * time.Second)
	if _, _ = f.o.WatchdogTick(rss, f.now.Unix()); true {
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunPaused {
		t.Fatalf("user-paused run must stay paused, got %q", got)
	}
}

func TestWatchdogKillLastResort(t *testing.T) {
	f := newFixture(t)
	if err := f.o.Pause(f.prun.Run.ID, types.PauseUser, "test"); err != nil {
		t.Fatal(err)
	}
	_, rec := f.spawnOk(f.parent.ID, childCard("v", agents.ProfileImplementer))
	vr := f.attach(rec)
	// The tool outlives the pause: the last resort destroys it.
	vr.SetTool("exec", "train", types.CancelGraceful, []string{"session:gpu"}, time.Hour, f.now)
	killed := []types.RunID{}
	f.o.SetHooks(orchestrator.Hooks{
		Sampler: func() (uint64, error) { return 5000, nil },
		KillTool: func(run types.RunID) (string, error) {
			killed = append(killed, run)
			return "exec pid 4242", nil
		},
	})
	f.o.SetRAMThreshold(1000)
	// First tick pauses everything pausable (parent cascade); the
	// tool stays alive inside the paused run.
	if _, _ = f.o.WatchdogTick(5000, f.now.Unix()); true {
	}
	if got := runState(t, f.store, rec.Run.ID); got != types.RunPaused {
		t.Fatalf("want paused before the kill, got %q", got)
	}
	// Still over the threshold with nothing left to pause: kill the
	// live tool of the lowest-priority resource-paused run.
	_, kill := f.o.WatchdogTick(5000, f.now.Unix()+1)
	if kill == "" || len(killed) != 1 || killed[0] != rec.Run.ID {
		t.Fatalf("last resort must kill the live tool, got %q", kill)
	}
}
