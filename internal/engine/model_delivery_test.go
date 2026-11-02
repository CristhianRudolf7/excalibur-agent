package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type slowDeliveryEndpoint struct {
	delay time.Duration
	calls int
}

func (m *slowDeliveryEndpoint) Chat(ctx context.Context, _ types.ModelRole, _ providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	m.calls++
	timer := time.NewTimer(m.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return providers.ChatResponse{}, models.CallMeta{}, ctx.Err()
	case <-timer.C:
		return finalResp("VERDICT: PASS"), models.CallMeta{}, nil
	}
}

func TestOptionalReviewDeadlinePreservesFinalization(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", "0")
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	startRun(t, e, r, js)
	r.Profile.TimeBudgetOnly = true
	r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(900 * time.Millisecond) }
	slow := &slowDeliveryEndpoint{delay: time.Second}
	e.deps.Models = slow
	_, _, err := e.verifierChat(context.Background(), r, "verifier.strong", "full", "small", 512, func(s string) bool { return s != "" })
	if !errors.Is(err, tasks.ErrVerifierNoVerdict) || slow.calls != 1 || r.deliveryPendingReason() == "" {
		t.Fatalf("err=%v calls=%d pending=%s", err, slow.calls, r.deliveryPendingReason())
	}
	// A second optional review cannot spend the remaining reserve again.
	_, _, _ = e.verifierChat(context.Background(), r, "verifier.strong", "full", "small", 512, func(string) bool { return true })
	if slow.calls != 1 {
		t.Fatal("timed-out review retried")
	}
	fast := &fakeEndpoint{}
	e.deps.Models = fast
	stop, err := e.step(context.Background(), r)
	if err != nil || stop || fast.calls() != 1 {
		t.Fatalf("finalization opportunity lost: stop=%v err=%v calls=%d", stop, err, fast.calls())
	}
	stop, err = e.step(context.Background(), r)
	if err != nil || !stop || fast.calls() != 1 || r.State() != types.RunCompletedPending {
		t.Fatalf("unbounded finalization: stop=%v err=%v calls=%d state=%s", stop, err, fast.calls(), r.State())
	}
}

func TestDeliveryDeadlineBudgetSourcesAndSwitch(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	now := time.Now()
	e.deps.Clock = func() time.Time { return now }
	for _, source := range []string{"explicit", "default"} {
		r := explicitRun()
		r.StartedAt = now.Add(-1100 * time.Millisecond)
		r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
		r.Profile.WallSource = source
		child, cancel := e.modelDeliveryContext(context.Background(), r, false)
		deadline, ok := child.Deadline()
		cancel()
		if !ok {
			t.Fatal("missing review deadline")
		}
		if source == "explicit" && time.Until(deadline) > time.Millisecond {
			t.Fatal("explicit limit ignored")
		}
		if source == "default" && time.Until(deadline) < time.Second {
			t.Fatal("assumed estimate treated as a hard deadline")
		}
	}
	r := explicitRun()
	r.StartedAt = now
	r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
	t.Setenv("EXCALIBUR_MODEL_DELIVERY_RESERVE", "0")
	child, cancel := e.modelDeliveryContext(context.Background(), r, false)
	defer cancel()
	if _, ok := child.Deadline(); ok {
		t.Fatal("switch ignored")
	}
}

func TestConfirmationModelCallIsBoundedAndPending(t *testing.T) {
	t.Setenv("EXCALIBUR_FINISH_TIME_RESERVE", "0")
	t.Setenv("EXCALIBUR_FINISH_CONFIRM_PROGRESS", "0")
	e, js := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := explicitRun()
	startRun(t, e, r, js)
	r.Profile.TimeBudgetOnly = true
	r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
	e.deps.Clock = func() time.Time { return r.StartedAt.Add(900 * time.Millisecond) }
	r.armConfirm(FinishArgs{Status: "completed", Summary: "prepared delivery", Evidence: "checks observed"})
	slow := &slowDeliveryEndpoint{delay: time.Second}
	e.deps.Models = slow
	stop, err := e.step(context.Background(), r)
	if err != nil || !stop || r.State() != types.RunCompletedPending || slow.calls != 1 {
		t.Fatalf("stop=%v err=%v state=%s calls=%d", stop, err, r.State(), slow.calls)
	}
}

func TestDeliveryReserveDoesNotBoundAssistantCalls(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	r := testRun(Profile{})
	r.StartedAt = time.Now()
	r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
	child, cancel := e.modelDeliveryContext(context.Background(), r, false)
	defer cancel()
	if _, ok := child.Deadline(); ok {
		t.Fatal("interactive review shortened")
	}
}

func TestExpiredExplicitReviewHorizonCannotBeExtendedByParent(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	now := time.Now()
	e.deps.Clock = func() time.Time { return now }
	r := explicitRun()
	r.StartedAt = now.Add(-2 * time.Second)
	r.Tracker.Limit = tasks.Limit{MaxSeconds: 1}
	r.Profile.WallSource = "explicit"
	parent, cancelParent := context.WithTimeout(context.Background(), time.Second)
	defer cancelParent()
	child, cancel := e.modelDeliveryContext(parent, r, false)
	defer cancel()
	deadline, ok := child.Deadline()
	if !ok || time.Until(deadline) > time.Millisecond {
		t.Fatal("parent extended an expired explicit wall limit")
	}
}
