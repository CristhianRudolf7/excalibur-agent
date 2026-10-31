package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// TestReviewExpiresAndReports covers steps 1-3 and 9: batch
// extraction, admission and expiry flips land in a persisted,
// consultable report (Section 10.6).
func TestReviewExpiresAndReports(t *testing.T) {
	db := openTestDB(t)
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, start, "general")
	svc.extract = &scriptExtractor{cands: []Candidate{{
		Kind: KindFact, Statement: "Reviewed fact from episodes", Sources: []Source{{Kind: "message", Ref: "msg_old_1"}},
	}}}

	writeMessage(t, db, "msg_old_1", "cnv_1", "general", "user", "remember that reviewed fact from episodes", start.Add(-time.Hour))
	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindEvent, Statement: "Already stale happening", ExpiresAt: start.Add(-time.Minute)})

	rep, err := svc.Background().ReviewScope(context.Background(), "general", ReviewOptions{Light: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Admitted < 1 {
		t.Fatalf("admitted = %d, want >= 1", rep.Admitted)
	}
	if rep.Expired < 1 {
		t.Fatalf("expired = %d, want >= 1", rep.Expired)
	}
	reports, err := svc.ListReports(context.Background(), []string{"general"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Fatal("nightly report not persisted")
	}
	// Second night: watermark advanced, nothing new admitted.
	rep2, err := svc.Background().ReviewScope(context.Background(), "general", ReviewOptions{Light: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep2.Admitted != 0 {
		t.Fatalf("second pass admitted = %d, want 0 (watermark)", rep2.Admitted)
	}
}

// TestReviewRetiresSourcelessExternal covers step 4: external rows
// whose sources all died retire; user rows without sources stay
// (Section 10.6).
func TestReviewRetiresSourcelessExternal(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact, Trust: TrustExternal,
		Statement: "External claim from vanished page", Sources: []Source{{Kind: "message", Ref: "msg_gone"}}})
	kept, _ := mustAdmit(t, svc, Candidate{Scope: "general", Kind: KindFact,
		Statement: "User said this stays", Sources: []Source{{Kind: "message", Ref: "msg_gone"}}})

	rep, err := svc.Background().ReviewScope(context.Background(), "general", ReviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Retired != 1 {
		t.Fatalf("retired = %d, want 1", rep.Retired)
	}
	got, err := svc.GetMemory(context.Background(), []string{"general"}, kept.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusActive {
		t.Fatalf("user row status = %q, want active", got.Status)
	}
}

// TestReviewNotesCompactionSafeguard covers step 7: old notes fold
// into a compiled truth plus one timeline line each; a lossy rewrite
// refuses (Section 10.6).
func TestReviewNotesCompactionSafeguard(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, now, "general")

	docID, err := svc.ResolveDoc(context.Background(), "general", DocNotebook)
	if err != nil {
		t.Fatal(err)
	}
	old := now.Add(-31 * 24 * time.Hour).UnixMilli()
	for i := 0; i < 4; i++ {
		frag, err := svc.AppendFragment(context.Background(), []string{"general"}, docID, "log", "old note line", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Write(context.Background(), store.PriorityNormal, func(ctx context.Context, ex store.Execer) error {
			_, err := ex.Exec(ctx, `UPDATE doc_fragments SET created_at=? WHERE id=?`, old, frag.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = svc.AppendFragment(context.Background(), []string{"general"}, docID, "log", "fresh note", "")

	rep, err := svc.Background().ReviewScope(context.Background(), "general", ReviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.NotesRejected != 0 {
		t.Fatalf("lossless compaction rejected = %d", rep.NotesRejected)
	}
	timeline, err := svc.ReadDoc(context.Background(), []string{"general"}, docID, "section:timeline")
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline.Frags) != 4 {
		t.Fatalf("timeline lines = %d, want 4 (one per removed note)", len(timeline.Frags))
	}
	compiled, err := svc.ReadDoc(context.Background(), []string{"general"}, docID, "section:compiled")
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Frags) != 1 {
		t.Fatalf("compiled truth fragments = %d, want 1", len(compiled.Frags))
	}
}

// TestQuietWindowSelection pins the histogram choice: activity at
// 09:00-17:00 local pushes the window to the small hours
// (Section 10.6 step 2).
func TestQuietWindowSelection(t *testing.T) {
	db := openTestDB(t)
	svc := newTestService(t, db, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), "general")
	svc.settings.Timezone = "Europe/Madrid"

	base := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for h := 9; h < 17; h++ {
		writeMessage(t, db, "msg_hist_"+string(rune('a'+h)), "cnv_h", "general", "user", "busy work",
			time.Date(base.Year(), base.Month(), base.Day(), h, 0, 0, 0, time.UTC))
	}
	start, length, err := svc.NextWindow(context.Background(), "general", base.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if length != reviewWindowMinutes {
		t.Fatalf("window length = %d", length)
	}
	hour := start / 60
	if hour >= 8 && hour < 20 {
		t.Fatalf("quiet window starts at %02d:00 inside busy hours", hour)
	}
}

// TestShouldReviewFallback gates the pre-day-five pass on 03:30
// local plus two idle hours (Section 10.6 activation).
func TestShouldReviewFallback(t *testing.T) {
	db := openTestDB(t)
	svc := newTestService(t, db, time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), "general")
	svc.settings.Timezone = "UTC"

	// Fresh scope: 0 active days, idle for a day, now 03:45 local.
	writeMessage(t, db, "msg_idle", "cnv_i", "general", "user", "old hello",
		time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC))
	ok, why := svc.ShouldReview(context.Background(), "general", time.Date(2026, 10, 20, 3, 45, 0, 0, time.UTC))
	if !ok {
		t.Fatalf("fallback should run: %s", why)
	}
	// Active user one hour ago: gate holds the pass.
	writeMessage(t, db, "msg_fresh", "cnv_i", "general", "user", "just now",
		time.Date(2026, 10, 20, 2, 50, 0, 0, time.UTC))
	ok, _ = svc.ShouldReview(context.Background(), "general", time.Date(2026, 10, 20, 3, 45, 0, 0, time.UTC))
	if ok {
		t.Fatal("fallback should wait out the idle gate")
	}
}

// TestReviewYieldsOnCancel proves the P3 behavior: a canceled
// context checkpoints the watermark and reports the yield instead
// of failing (Section 10.6 step 3).
func TestReviewYieldsOnCancel(t *testing.T) {
	db := openTestDB(t)
	start := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	svc := newTestService(t, db, start, "general")
	for i := 0; i < reviewBatchTurns+10; i++ {
		writeMessage(t, db, "msg_y_"+string(rune('a'+i%26))+itoa2(i), "cnv_y", "general", "user", "episode text", start.Add(-time.Hour))
	}
	svc.extract = &scriptExtractor{cands: []Candidate{{Kind: KindFact, Statement: "late fact"}}} // never reached
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rep, err := svc.Background().ReviewScope(ctx, "general", ReviewOptions{Light: true})
	if err != nil {
		t.Fatalf("yielded review must not fail: %v", err)
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.HasPrefix(w, "yielded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no yield warning: %+v", rep.Warnings)
	}
}

func itoa2(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
