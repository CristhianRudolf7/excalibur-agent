// Retention scheduler tests: 4.7 daily pass, idempotent and ordered.
package daemon

import (
	"context"
	"errors"
	"testing"
	"time"
)

type phaseLog struct {
	order []string
	fail  map[string]bool
	count map[string]int
}

func (p *phaseLog) hook(name string) func(context.Context) error {
	return func(ctx context.Context) error {
		p.order = append(p.order, name)
		p.count[name]++
		if p.fail[name] {
			return errors.New(name + " failed")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
}

func TestRunRetentionPhaseOrder(t *testing.T) {
	log := &phaseLog{count: map[string]int{}}
	ph := RetentionPhases{
		CompactJournals:  log.hook("journals"),
		ReleaseToolBlobs: log.hook("blobs"),
		TrimTelemetry:    log.hook("telemetry"),
		CheckpointWAL:    log.hook("wal"),
		BackupAll:        log.hook("backup"),
	}
	rep, err := RunRetention(context.Background(), ph)
	if err != nil {
		t.Fatalf("RunRetention: %v", err)
	}
	want := []string{"journals", "blobs", "telemetry", "wal", "backup"}
	if len(log.order) != len(want) {
		t.Fatalf("phases = %v, want %v", log.order, want)
	}
	for i, name := range want {
		if log.order[i] != name {
			t.Fatalf("phases = %v, want %v", log.order, want)
		}
	}
	if !rep.Checkpointed || !rep.BackedUp {
		t.Errorf("report must mark checkpoint and backup: %+v", rep)
	}
}

func TestRunRetentionFailureAborts(t *testing.T) {
	log := &phaseLog{count: map[string]int{}, fail: map[string]bool{"blobs": true}}
	ph := RetentionPhases{
		CompactJournals:  log.hook("journals"),
		ReleaseToolBlobs: log.hook("blobs"),
		TrimTelemetry:    log.hook("telemetry"),
	}
	if _, err := RunRetention(context.Background(), ph); err == nil {
		t.Fatal("failing phase must abort the pass")
	}
	if log.count["telemetry"] != 0 {
		t.Error("phases after the failure must not run; the next pass retries")
	}
	// Next pass retries from the start: idempotent phases re-run.
	log.fail["blobs"] = false
	if _, err := RunRetention(context.Background(), ph); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if log.count["journals"] != 2 {
		t.Errorf("journals ran %d times, want 2 (idempotent re-run)", log.count["journals"])
	}
}

func TestRunRetentionSkipsNilHooks(t *testing.T) {
	log := &phaseLog{count: map[string]int{}}
	ph := RetentionPhases{BackupAll: log.hook("backup")}
	rep, err := RunRetention(context.Background(), ph)
	if err != nil {
		t.Fatalf("RunRetention: %v", err)
	}
	if !rep.BackedUp || rep.Checkpointed {
		t.Errorf("only wired phases run: %+v", rep)
	}
}

func TestRetentionSchedulerDaily(t *testing.T) {
	s := NewRetentionScheduler(3)
	at := func(h int) time.Time {
		return time.Date(2026, 10, 31, h, 0, 0, 0, time.UTC)
	}
	if s.Due(at(4)) {
		t.Error("must not fire outside the window hour")
	}
	if !s.Due(at(3)) {
		t.Error("must fire inside the window hour when never ran today")
	}
	s.MarkRan(at(3))
	if s.Due(at(3)) {
		t.Error("must run once per day")
	}
	if !s.Due(at(3).Add(24 * time.Hour)) {
		t.Error("must fire again the next day")
	}
}

func TestRetentionSchedulerMaybeRun(t *testing.T) {
	s := NewRetentionScheduler(3)
	log := &phaseLog{count: map[string]int{}}
	ph := RetentionPhases{BackupAll: log.hook("backup")}
	at := time.Date(2026, 10, 31, 3, 30, 0, 0, time.UTC)
	rep, ran, err := s.MaybeRun(context.Background(), at, ph)
	if err != nil || !ran || !rep.BackedUp {
		t.Fatalf("MaybeRun = %+v %v %v", rep, ran, err)
	}
	if _, ran, _ := s.MaybeRun(context.Background(), at, ph); ran {
		t.Error("second call the same day must not run")
	}
	if _, ran, _ := s.MaybeRun(context.Background(), at.Add(time.Hour), ph); ran {
		t.Error("outside the window must not run")
	}
}
