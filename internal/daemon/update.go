// Online update with rollback (Section 25.3).
//
// The channel serves a signed manifest (ed25519) with versions,
// hashes and notes. The updater verifies signature and hash, stages
// the new binary next to the current one, drains at a quiet moment
// (never with a user question awaiting an immediate answer nor with
// after_completion tools running), backups with VACUUM INTO, swaps,
// migrates before accepting traffic and resumes via the 17.3 boot
// algorithm. The downtime window hosts no reception; clients keep
// unconfirmed input in their outbox (3.5).
//
// Health verification has two distinct rollbacks: a failed migration
// restores the backup copies (no new data entered yet) and the old
// binary; a later failure of the new binary rolls back to the old
// binary WITHOUT restoring the database, because migrations only
// expand then contract (4.6) and data received meanwhile must not be
// lost. Users can pin a version or disable automatic updates.
package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// UpdateChannel selects the release channel (25.3 step 1).
type UpdateChannel string

// Release channels.
const (
	ChannelStable UpdateChannel = "stable"
	ChannelBeta   UpdateChannel = "beta"
)

// Manifest describes one release (25.3 step 1): versions, hashes,
// notes, signed with the channel key.
type Manifest struct {
	Channel UpdateChannel `json:"channel"`
	Version string        `json:"version"`
	SHA256  string        `json:"sha256"`
	Notes   string        `json:"notes"`
	// Payload carries the new binary bytes in tests and local
	// updates; remote updates fetch them from BinaryRef.
	Payload   []byte `json:"-"`
	BinaryRef string `json:"binary_ref,omitempty"`
	Signature string `json:"signature,omitempty"`
}

// manifestBytes is the canonical signed encoding: everything except
// the signature itself.
func manifestBytes(m Manifest) []byte {
	raw, _ := json.Marshal(struct {
		Channel UpdateChannel `json:"channel"`
		Version string        `json:"version"`
		SHA256  string        `json:"sha256"`
		Notes   string        `json:"notes"`
	}{m.Channel, m.Version, m.SHA256, m.Notes})
	return raw
}

// SignManifest signs m with the channel private key (tests and the
// release tooling; the daemon only verifies).
func SignManifest(m *Manifest, priv ed25519.PrivateKey) {
	m.Signature = hex.EncodeToString(ed25519.Sign(priv, manifestBytes(*m)))
}

// VerifyManifest checks channel, signature and payload hash. A bad
// signature rejects the update before anything is staged (25.5).
func VerifyManifest(m Manifest, pub ed25519.PublicKey, payload []byte) error {
	if m.Channel != ChannelStable && m.Channel != ChannelBeta {
		return fmt.Errorf("daemon: update: unknown channel %q", m.Channel)
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil {
		return fmt.Errorf("daemon: update: bad signature encoding: %w", err)
	}
	if !ed25519.Verify(pub, manifestBytes(m), sig) {
		return fmt.Errorf("daemon: update: signature rejected")
	}
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return fmt.Errorf("daemon: update: hash mismatch")
	}
	return nil
}

// Drain parks the daemon for the swap (25.3 step 4): stop admitting
// new runs, bring active ones to a safe point paused(recovery), wait
// for uninterruptible tools with a timeout, confirm the journal, back
// up and stop. Connected clients get a "back in seconds" notice.
type Drain interface {
	// StopIntake closes admission for new runs.
	StopIntake() error
	// ParkActive pauses active runs at a safe point; it returns the
	// parked count.
	ParkActive() (int, error)
	// WaitTools waits for uninterruptible (after_completion) tools
	// up to timeout.
	WaitTools(timeout time.Duration) error
	// ConfirmJournal fsyncs the durable journal state.
	ConfirmJournal() error
}

// ImageStore stages and swaps binaries beside the running one.
type ImageStore interface {
	// Stage writes payload as the candidate binary.
	Stage(payload []byte) (stagedPath string, err error)
	// Swap atomically promotes the staged binary, keeping the old
	// one for rollback.
	Swap() error
	// RollbackBinary restores the previous binary without touching
	// the databases.
	RollbackBinary() error
	// CurrentVersion reports the running version.
	CurrentVersion() string
}

// DataBackup snapshots and restores the databases (VACUUM INTO
// copies, 4.6). Restore is only for the migration-failure path:
// nothing new entered the bases yet.
type DataBackup interface {
	Snapshot(ctx context.Context) error
	Restore() error
}

// Migrator applies pending schema migrations before the new binary
// accepts traffic (25.3 step 5).
type Migrator interface {
	Migrate(ctx context.Context) error
}

// BootHealth checks the new binary after start: failed start or an
// anomalous error rate in the first minutes rolls back the binary
// only (25.3 step 6).
type BootHealth interface {
	Check() error
}

// AfterCompletionGuard reports tools that must never be interrupted
// and user questions awaiting an immediate answer: while either is
// non-empty the updater waits instead of draining (25.3 step 3).
type AfterCompletionGuard interface {
	RunningTools() []string
	PendingUrgentQuestions() []string
}

// UpdatePolicy tunes the updater.
type UpdatePolicy struct {
	Channel UpdateChannel
	// PinnedVersion, when set, only applies that version.
	PinnedVersion string
	// Auto disables or enables background updates.
	Auto bool
	// DrainTimeout bounds the wait for after_completion tools.
	DrainTimeout time.Duration
}

// UpdateReport is the evidence kept for 25.5.
type UpdateReport struct {
	From       string
	To         string
	Parked     int
	Drained    bool
	Migrated   bool
	RolledBack string
	At         time.Time
}

// Updater runs the 25.3 state machine from injectable parts.
type Updater struct {
	Policy  UpdatePolicy
	PubKey  ed25519.PublicKey
	Drain   Drain
	Images  ImageStore
	Data    DataBackup
	Migrate Migrator
	Health  BootHealth
	Guard   AfterCompletionGuard
	Now     func() time.Time
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now().UTC()
}

// Apply verifies, drains, swaps, migrates and health-checks one
// release. Every failure maps to its 25.3 rollback:
//
//   - bad signature or hash: reject before staging anything;
//   - pinned version mismatch: skip silently (not a failure);
//   - drain or stage failure: abort, nothing changed;
//   - migration failure: restore the backup copies (no new data yet)
//     and roll back the binary;
//   - post-boot health failure: roll back the binary WITHOUT
//     restoring the database (4.6 expand-then-contract), so received
//     messages survive.
func (u *Updater) Apply(ctx context.Context, m Manifest, payload []byte) (UpdateReport, error) {
	var rep UpdateReport
	rep.At = u.now()
	if u.Policy.PinnedVersion != "" && m.Version != u.Policy.PinnedVersion {
		return rep, nil
	}
	if u.Images == nil {
		return rep, fmt.Errorf("daemon: update needs an image store")
	}
	rep.From = u.Images.CurrentVersion()
	rep.To = m.Version
	if err := VerifyManifest(m, u.PubKey, payload); err != nil {
		return rep, err
	}
	if u.Guard != nil {
		if tools := u.Guard.RunningTools(); len(tools) > 0 {
			return rep, fmt.Errorf("daemon: update waits: after_completion tools running: %v", tools)
		}
		if qs := u.Guard.PendingUrgentQuestions(); len(qs) > 0 {
			return rep, fmt.Errorf("daemon: update waits: urgent questions pending: %v", qs)
		}
	}
	if u.Drain != nil {
		if err := u.Drain.StopIntake(); err != nil {
			return rep, fmt.Errorf("daemon: update stop intake: %w", err)
		}
		n, err := u.Drain.ParkActive()
		if err != nil {
			return rep, fmt.Errorf("daemon: update park active: %w", err)
		}
		rep.Parked = n
		timeout := u.Policy.DrainTimeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		if err := u.Drain.WaitTools(timeout); err != nil {
			return rep, fmt.Errorf("daemon: update drain tools: %w", err)
		}
		if err := u.Drain.ConfirmJournal(); err != nil {
			return rep, fmt.Errorf("daemon: update confirm journal: %w", err)
		}
		rep.Drained = true
	}
	if u.Data != nil {
		if err := u.Data.Snapshot(ctx); err != nil {
			return rep, fmt.Errorf("daemon: update snapshot: %w", err)
		}
	}
	if _, err := u.Images.Stage(payload); err != nil {
		return rep, fmt.Errorf("daemon: update stage: %w", err)
	}
	if err := u.Images.Swap(); err != nil {
		return rep, fmt.Errorf("daemon: update swap: %w", err)
	}
	if u.Migrate != nil {
		if err := u.Migrate.Migrate(ctx); err != nil {
			rbErr := u.rollbackData()
			if rbErr != nil {
				return rep, fmt.Errorf("daemon: update migration failed (%v) and restore failed: %w", err, rbErr)
			}
			rep.RolledBack = "binary+data (migration failed before new data)"
			return rep, fmt.Errorf("daemon: update migration failed, restored: %w", err)
		}
		rep.Migrated = true
	}
	if u.Health != nil {
		if err := u.Health.Check(); err != nil {
			if rerr := u.Images.RollbackBinary(); rerr != nil {
				return rep, fmt.Errorf("daemon: update health failed (%v) and binary rollback failed: %w", err, rerr)
			}
			rep.RolledBack = "binary only (database kept: messages received meanwhile survive)"
			return rep, fmt.Errorf("daemon: update health failed, binary rolled back: %w", err)
		}
	}
	return rep, nil
}

// rollbackData restores backups and the previous binary after a
// migration failure.
func (u *Updater) rollbackData() error {
	if u.Data != nil {
		if err := u.Data.Restore(); err != nil {
			return err
		}
	}
	if u.Images != nil {
		if err := u.Images.RollbackBinary(); err != nil {
			return err
		}
	}
	return nil
}

// FileImageStore is a minimal on-disk ImageStore for the daemon: the
// candidate sits next to the current binary and Swap renames
// atomically, keeping the previous binary as .prev for RollbackBinary.
type FileImageStore struct {
	// Dir holds excalibur, excalibur.next and excalibur.prev.
	Dir string
	// Version tracks the running version for reports.
	Version string
}

// binaryPaths resolves the three image paths.
func (s *FileImageStore) binaryPaths() (cur, next, prev string) {
	return joinBin(s.Dir), joinBin(s.Dir) + ".next", joinBin(s.Dir) + ".prev"
}

func joinBin(dir string) string {
	if dir == "" {
		return "excalibur"
	}
	return dir + string(os.PathSeparator) + "excalibur"
}

// CurrentVersion implements ImageStore.
func (s *FileImageStore) CurrentVersion() string { return s.Version }

// Stage implements ImageStore.
func (s *FileImageStore) Stage(payload []byte) (string, error) {
	_, next, _ := s.binaryPaths()
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(next, payload, 0o755); err != nil {
		return "", err
	}
	return next, nil
}

// Swap implements ImageStore.
func (s *FileImageStore) Swap() error {
	cur, next, prev := s.binaryPaths()
	if _, err := os.Stat(next); err != nil {
		return fmt.Errorf("daemon: swap without staged binary: %w", err)
	}
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, prev); err != nil {
			return err
		}
	}
	if err := os.Rename(next, cur); err != nil {
		return err
	}
	return nil
}

// RollbackBinary implements ImageStore.
func (s *FileImageStore) RollbackBinary() error {
	cur, _, prev := s.binaryPaths()
	if _, err := os.Stat(prev); err != nil {
		return fmt.Errorf("daemon: no previous binary to roll back to: %w", err)
	}
	if err := os.Rename(cur, cur+".bad"); err != nil {
		return err
	}
	return os.Rename(prev, cur)
}
