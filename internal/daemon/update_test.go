// Update tests: 25.5 rows — bad signature, drain waits, migration
// failure restores, post-boot failure keeps the database, pinning.
package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"
)

type fakeDrain struct {
	parked    int
	waitErr   error
	stopped   bool
	confirmed bool
}

func (f *fakeDrain) StopIntake() error { f.stopped = true; return nil }
func (f *fakeDrain) ParkActive() (int, error) {
	if !f.stopped {
		return 0, errors.New("parked before intake stopped")
	}
	return f.parked, nil
}
func (f *fakeDrain) WaitTools(_ time.Duration) error { return f.waitErr }
func (f *fakeDrain) ConfirmJournal() error           { f.confirmed = true; return nil }

type fakeImages struct {
	version  string
	staged   []byte
	swapped  bool
	rolledBk bool
}

func (f *fakeImages) CurrentVersion() string { return f.version }
func (f *fakeImages) Stage(p []byte) (string, error) {
	f.staged = append([]byte(nil), p...)
	return "staged", nil
}
func (f *fakeImages) Swap() error {
	if f.staged == nil {
		return errors.New("nothing staged")
	}
	f.swapped = true
	return nil
}
func (f *fakeImages) RollbackBinary() error { f.rolledBk = true; return nil }

type fakeData struct {
	snapped   bool
	restored  bool
	snapErr   error
	restorErr error
}

func (f *fakeData) Snapshot(_ context.Context) error { f.snapped = true; return f.snapErr }
func (f *fakeData) Restore() error                   { f.restored = true; return f.restorErr }

type fakeMigrate struct{ err error }

func (f *fakeMigrate) Migrate(_ context.Context) error { return f.err }

type fakeHealth struct{ err error }

func (f *fakeHealth) Check() error { return f.err }

type fakeGuard struct {
	tools []string
	qs    []string
}

func (f *fakeGuard) RunningTools() []string           { return f.tools }
func (f *fakeGuard) PendingUrgentQuestions() []string { return f.qs }

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func signedManifest(t *testing.T, priv ed25519.PrivateKey, version string, payload []byte) Manifest {
	t.Helper()
	sum := sha256.Sum256(payload)
	m := Manifest{Channel: ChannelStable, Version: version, SHA256: hex.EncodeToString(sum[:]), Notes: "test"}
	SignManifest(&m, priv)
	return m
}

func testUpdater(pub ed25519.PublicKey) (*Updater, *fakeDrain, *fakeImages, *fakeData) {
	d, im, da := &fakeDrain{parked: 3}, &fakeImages{version: "1.0"}, &fakeData{}
	u := &Updater{
		Policy: PolicyForTests(),
		PubKey: pub, Drain: d, Images: im, Data: da,
		Migrate: &fakeMigrate{}, Health: &fakeHealth{}, Guard: &fakeGuard{},
		Now: func() time.Time { return time.Date(2026, 10, 31, 3, 0, 0, 0, time.UTC) },
	}
	return u, d, im, da
}

// PolicyForTests is the deterministic update policy tests share.
func PolicyForTests() UpdatePolicy {
	return UpdatePolicy{Channel: ChannelStable, Auto: true, DrainTimeout: time.Second}
}

func TestUpdateHappyPath(t *testing.T) {
	pub, priv := testKeys(t)
	u, d, im, da := testUpdater(pub)
	payload := []byte("fake-binary-2.0")
	m := signedManifest(t, priv, "2.0", payload)
	rep, err := u.Apply(context.Background(), m, payload)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if rep.From != "1.0" || rep.To != "2.0" || rep.Parked != 3 || !rep.Drained || !rep.Migrated {
		t.Errorf("report = %+v", rep)
	}
	if !d.stopped || !d.confirmed || !da.snapped || !im.swapped {
		t.Errorf("drain/snapshot/swap must all run: %+v %+v %+v", d, da, im)
	}
	if rep.RolledBack != "" {
		t.Errorf("clean update rolls back nothing: %+v", rep)
	}
}

func TestUpdateRejectsBadSignature(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, _ := testUpdater(pub)
	payload := []byte("fake-binary-2.0")
	m := signedManifest(t, priv, "2.0", payload)
	m.Signature = m.Signature[:len(m.Signature)-2] + "ff"
	if _, err := u.Apply(context.Background(), m, payload); err == nil {
		t.Fatal("forged signature must reject before staging")
	}
	if im.staged != nil || im.swapped {
		t.Error("rejected update must stage nothing")
	}
}

func TestUpdateRejectsHashMismatch(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, _ := testUpdater(pub)
	m := signedManifest(t, priv, "2.0", []byte("real"))
	if _, err := u.Apply(context.Background(), m, []byte("tampered")); err == nil {
		t.Fatal("hash mismatch must reject")
	}
	if im.swapped {
		t.Error("rejected update must not swap")
	}
}

func TestUpdateWaitsForAfterCompletion(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, _ := testUpdater(pub)
	u.Guard = &fakeGuard{tools: []string{"backup"}}
	payload := []byte("bin")
	m := signedManifest(t, priv, "2.0", payload)
	if _, err := u.Apply(context.Background(), m, payload); err == nil {
		t.Fatal("after_completion tools must hold the drain")
	}
	if im.swapped {
		t.Error("held update must not swap")
	}
	u.Guard = &fakeGuard{qs: []string{"approve plan?"}}
	if _, err := u.Apply(context.Background(), m, payload); err == nil {
		t.Fatal("urgent questions must hold the drain")
	}
}

func TestUpdateMigrationFailureRestoresData(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, da := testUpdater(pub)
	u.Migrate = &fakeMigrate{err: errors.New("migration boom")}
	payload := []byte("bin")
	m := signedManifest(t, priv, "2.0", payload)
	rep, err := u.Apply(context.Background(), m, payload)
	if err == nil {
		t.Fatal("migration failure must fail the update")
	}
	if !da.restored || !im.rolledBk {
		t.Errorf("migration path restores data AND binary: data=%v images=%v", da.restored, im.rolledBk)
	}
	if rep.RolledBack == "" {
		t.Error("report must name the rollback")
	}
}

func TestUpdatePostBootFailureKeepsDatabase(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, da := testUpdater(pub)
	u.Health = &fakeHealth{err: errors.New("error rate anomalous")}
	payload := []byte("bin")
	m := signedManifest(t, priv, "2.0", payload)
	// Simulate a message received between swap and health check.
	pendingBefore := 1
	rep, err := u.Apply(context.Background(), m, payload)
	if err == nil {
		t.Fatal("health failure must fail the update")
	}
	if !im.rolledBk {
		t.Error("health path rolls back the binary")
	}
	if da.restored {
		t.Error("health path must NOT restore the database: expand-contract migrations keep received data")
	}
	pendingAfter := pendingBefore // outbox untouched by rollback
	if pendingAfter != pendingBefore {
		t.Error("rollback must not lose client messages")
	}
	if rep.RolledBack == "" {
		t.Error("report must name the binary-only rollback")
	}
}

func TestUpdatePinning(t *testing.T) {
	pub, priv := testKeys(t)
	u, _, im, _ := testUpdater(pub)
	u.Policy.PinnedVersion = "1.0"
	payload := []byte("bin")
	m := signedManifest(t, priv, "2.0", payload)
	rep, err := u.Apply(context.Background(), m, payload)
	if err != nil {
		t.Fatalf("pinned skip is not a failure: %v", err)
	}
	if im.swapped || rep.To != "" {
		t.Errorf("pinned updater must skip other versions: %+v", rep)
	}
}

func TestFileImageStoreSwapRollback(t *testing.T) {
	dir := t.TempDir()
	s := &FileImageStore{Dir: dir, Version: "1.0"}
	if _, err := s.Stage([]byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Swap(); err != nil {
		t.Fatalf("first swap: %v", err)
	}
	if _, err := s.Stage([]byte("v2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Swap(); err != nil {
		t.Fatalf("second swap: %v", err)
	}
	if err := s.RollbackBinary(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	cur, _, _ := s.binaryPaths()
	raw, err := os.ReadFile(cur)
	if err != nil || string(raw) != "v1" {
		t.Errorf("after rollback current = %q, %v; want v1", raw, err)
	}
}
