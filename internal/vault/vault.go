package vault

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// KEK origins stored in vault_keys.kek_kind.
const (
	kekSecretService = "secret_service"
	kekPassphrase    = "passphrase"
)

// DefaultIdleTimeout is the Appendix B auto-lock: never with Secret
// Service (zero), 12 hours with a passphrase.
const DefaultPassphraseIdleTimeout = 12 * time.Hour

// namePattern constrains entry names: lowercase letters, digits,
// dashes and underscores, 1-64 chars.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Options configures a Vault.
type Options struct {
	// Keyring supplies the master key (Secret Service on Linux).
	// Nil means passphrase-only operation.
	Keyring platform.Keyring
	// KeyringService and KeyringUser address the KEK entry.
	KeyringService string
	KeyringUser    string
	// PassphraseFile, when set, unlocks the vault unattended at Open
	// (explicitly declared in config, permissions 0600 enforced).
	PassphraseFile string
	// IdleTimeout locks the vault after inactivity. Zero means never
	// (Secret Service default); passphrase mode defaults to 12 hours
	// unless overridden.
	IdleTimeout time.Duration
	// IdleTimeoutSet distinguishes an explicit zero (never) from unset.
	IdleTimeoutSet bool
	// EvalProfile blocks opening entirely (Section 23.2).
	EvalProfile bool
}

// Metadata describes one secret without its value. Values never leave
// the vault except through Resolve into a tool process.
type Metadata struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Meta         map[string]string `json:"metadata"`
	Scopes       []string          `json:"scopes"`
	AllowedTools []string          `json:"allowed_tools"`
	Domains      []string          `json:"domains"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	LastUsedAt   *time.Time        `json:"last_used_at,omitempty"`
	ExpiresAt    *time.Time        `json:"expires_at,omitempty"`
	EntryVersion int               `json:"entry_version"`
	DEKVersion   int               `json:"dek_version"`
}

// SetParams carries a create or update of an entry value with policy.
type SetParams struct {
	Name         string
	Type         string
	Value        []byte
	Meta         map[string]string
	Scopes       []string
	AllowedTools []string
	Domains      []string
	ExpiresAt    *time.Time
}

// PolicyParams carries a metadata/policy-only change.
type PolicyParams struct {
	Meta         *map[string]string
	Scopes       *[]string
	AllowedTools *[]string
	Domains      *[]string
	ExpiresAt    **time.Time
}

// UseRecord answers "where was this key used" (never the value).
type UseRecord struct {
	SecretName string    `json:"secret_name"`
	Tool       string    `json:"tool"`
	RunID      string    `json:"run_id"`
	Dest       string    `json:"dest"`
	Project    string    `json:"project"`
	At         time.Time `json:"at"`
}

// Vault is the secrets vault of Section 20.1: entries encrypted per
// entry with XChaCha20-Poly1305 in vault.db, a random DEK wrapped by a
// master key from the platform Keyring or an Argon2id passphrase,
// references resolved only inside tool processes, and automatic
// redaction support. The zero value is invalid; use Open.
type Vault struct {
	db   *store.DB
	opts Options

	mu         sync.Mutex
	dek        *[32]byte
	dekVersion int
	kekKind    string

	redactor *Redactor

	lastActivity time.Time
	idleTimeout  time.Duration
	stopWatch    chan struct{}
	watchRunning bool

	// pendingDrafts holds chat-detected candidates captured while the
	// vault is locked (no DEK to encrypt them with yet).
	pendingDrafts map[string][]byte

	// watchTick overrides the auto-lock poll interval (tests only;
	// zero means one minute).
	watchTick time.Duration

	closed bool
}

// Open opens (creating) vault.db at path. With EvalProfile it returns
// ErrEvalBlocked without touching disk. With PassphraseFile set it
// unlocks immediately; otherwise the vault starts locked.
func Open(ctx context.Context, path string, opts Options) (*Vault, error) {
	if opts.EvalProfile {
		return nil, ErrEvalBlocked
	}
	if opts.KeyringService == "" {
		opts.KeyringService = "excalibur-vault"
	}
	if opts.KeyringUser == "" {
		opts.KeyringUser = "vault-dek"
	}
	db, err := openStore(ctx, path)
	if err != nil {
		return nil, err
	}
	v := &Vault{
		db:            db,
		opts:          opts,
		redactor:      NewRedactor(),
		pendingDrafts: map[string][]byte{},
	}
	if v.opts.IdleTimeoutSet {
		v.idleTimeout = v.opts.IdleTimeout
	}
	if opts.PassphraseFile != "" {
		pp, err := LoadPassphraseFile(opts.PassphraseFile)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := v.Unlock(ctx, pp); err != nil {
			ZeroBytes(pp)
			_ = db.Close()
			return nil, err
		}
		ZeroBytes(pp)
	}
	return v, nil
}

// Close locks the vault and closes vault.db.
func (v *Vault) Close() error {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return nil
	}
	v.closed = true
	v.stopWatcherLocked()
	v.clearDEKLocked()
	v.mu.Unlock()
	return v.db.Close()
}

// IsLocked reports whether the vault is locked.
func (v *Vault) IsLocked() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dek == nil
}

// KEKKind reports the active master-key origin ("secret_service" or
// "passphrase"), or "" when locked or never initialized.
func (v *Vault) KEKKind() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.kekKind
}

// needsUnlock is the waiting-on-user question payload used when a tool
// needs a secret while the vault is locked.
func needsUnlock(name string) error {
	return fmt.Errorf("%w: unlock the vault to use secret %q (question: unlock-vault)", ErrLocked, name)
}

// Unlock opens the vault: the KEK comes from the platform Keyring when
// reachable, else from the passphrase (Argon2id). A nil/empty
// passphrase with an unreachable keyring returns ErrNoKey and leaves
// the rest of the harness running. First use initializes the DEK and
// wraps it with whichever KEK origin is available.
func (v *Vault) Unlock(ctx context.Context, passphrase []byte) error {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return store.ErrClosed
	}
	v.mu.Unlock()

	kek, kind, salt, haveKeys, dekVer, wrapped, wrapNonce, err := v.loadKeyState(ctx)
	if err != nil {
		return err
	}
	_ = salt

	if !haveKeys {
		return v.initialize(ctx, passphrase)
	}

	switch kind {
	case kekSecretService:
		if v.opts.Keyring == nil {
			if len(passphrase) == 0 {
				return fmt.Errorf("%w: secret-service entry with no keyring configured", ErrNoKey)
			}
			return v.unlockWithPassphrase(ctx, passphrase, dekVer, wrapped, wrapNonce, haveKeys, salt)
		}
		stored, kerr := v.opts.Keyring.Get(v.opts.KeyringService, v.opts.KeyringUser)
		if kerr == nil {
			defer ZeroBytes(stored)
			return v.installDEK(ctx, stored, dekVer, wrapped, wrapNonce, kekSecretService)
		}
		if len(passphrase) == 0 {
			return fmt.Errorf("%w: %v", ErrNoKey, kerr)
		}
		// Keyring entry missing but a passphrase was supplied: adopt
		// passphrase origin only if the stored row agrees (re-wrap).
		if isNotFound(kerr) {
			return v.adoptPassphrase(ctx, passphrase)
		}
		return v.unlockWithPassphrase(ctx, passphrase, dekVer, wrapped, wrapNonce, haveKeys, salt)
	case kekPassphrase:
		if len(passphrase) == 0 {
			return fmt.Errorf("%w: passphrase required", ErrNoKey)
		}
		_ = kek
		return v.unlockWithPassphrase(ctx, passphrase, dekVer, wrapped, wrapNonce, haveKeys, salt)
	default:
		return fmt.Errorf("vault: unknown kek kind %q", kind)
	}
}

// unlockWithPassphrase unwraps the DEK with an Argon2id-derived KEK.
func (v *Vault) unlockWithPassphrase(ctx context.Context, passphrase []byte, dekVer int, wrapped, wrapNonce []byte, _ bool, salt []byte) error {
	if len(salt) != saltLen {
		return fmt.Errorf("vault: corrupt salt for dek v%d", dekVer)
	}
	kek, err := deriveKEK(passphrase, salt)
	if err != nil {
		return err
	}
	defer zero(kek)
	return v.installDEK(ctx, kek, dekVer, wrapped, wrapNonce, kekPassphrase)
}

// installDEK unwraps, mlocks, installs the DEK and rebuilds redaction.
func (v *Vault) installDEK(ctx context.Context, kek []byte, dekVer int, wrapped, wrapNonce []byte, kind string) error {
	dek, err := unwrapDEK(kek, dekVer, wrapNonce, wrapped)
	if err != nil {
		return err
	}
	values, names, err := v.loadAllValues(ctx, dek)
	if err != nil {
		zero(dek[:])
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		zero(dek[:])
		return store.ErrClosed
	}
	v.clearDEKLocked()
	v.dek = &dek
	lockMemory(v.dek[:])
	v.dekVersion = dekVer
	v.kekKind = kind
	v.touchLocked()
	v.redactor = BuildRedactor(values, names)
	v.startWatcherLocked()
	// Move in-memory pending drafts into the encrypted table now that
	// a DEK is available.
	if len(v.pendingDrafts) > 0 {
		pending := v.pendingDrafts
		v.pendingDrafts = map[string][]byte{}
		v.mu.Unlock()
		for id, val := range pending {
			_ = id
			_, _ = v.saveDraft(ctx, val, "chat-paste")
			zero(val)
		}
		v.mu.Lock()
	}
	return nil
}

// initialize creates the first DEK: random KEK in the keyring when
// reachable, else an Argon2id KEK from the passphrase.
func (v *Vault) initialize(ctx context.Context, passphrase []byte) error {
	var kek []byte
	kind := ""
	var salt [saltLen]byte
	if v.opts.Keyring != nil {
		if _, err := v.opts.Keyring.Get(v.opts.KeyringService, v.opts.KeyringUser); err == nil || !isNotFound(err) {
			if err == nil {
				// Entry exists but vault_keys row is missing: adopt it.
				stored, _ := v.opts.Keyring.Get(v.opts.KeyringService, v.opts.KeyringUser)
				defer ZeroBytes(stored)
				dek, derr := newDEK()
				if derr != nil {
					return derr
				}
				nonce, wrapped, werr := wrapDEK(stored, 1, dek)
				if werr != nil {
					zero(dek[:])
					return werr
				}
				if serr := v.storeKeysRow(ctx, 1, wrapped, nonce, kekSecretService, nil); serr != nil {
					zero(dek[:])
					return serr
				}
				return v.installDEKFromDEK(ctx, dek, 1, kekSecretService)
			}
		}
		// No entry: create a random KEK in the keyring unless a
		// passphrase was explicitly supplied (passphrase wins then).
		if len(passphrase) == 0 {
			kek = make([]byte, 32)
			if _, err := rand.Read(kek); err != nil {
				return fmt.Errorf("vault: kek: %w", err)
			}
			defer zero(kek)
			if err := v.opts.Keyring.Set(v.opts.KeyringService, v.opts.KeyringUser, kek); err != nil {
				return fmt.Errorf("%w: %v", ErrNoKey, err)
			}
			kind = kekSecretService
		}
	}
	if kind == "" {
		if len(passphrase) == 0 {
			return fmt.Errorf("%w: no keyring entry and no passphrase for first unlock", ErrNoKey)
		}
		var err error
		salt, err = newSalt()
		if err != nil {
			return err
		}
		kek, err = deriveKEK(passphrase, salt[:])
		if err != nil {
			return err
		}
		defer zero(kek)
		kind = kekPassphrase
	}
	dek, err := newDEK()
	if err != nil {
		return err
	}
	nonce, wrapped, err := wrapDEK(kek, 1, dek)
	if err != nil {
		zero(dek[:])
		return err
	}
	var saltBytes []byte
	if kind == kekPassphrase {
		saltBytes = salt[:]
	}
	if err := v.storeKeysRow(ctx, 1, wrapped, nonce, kind, saltBytes); err != nil {
		zero(dek[:])
		return err
	}
	return v.installDEKFromDEK(ctx, dek, 1, kind)
}

// adoptPassphrase migrates a secret-service vault to passphrase origin
// when the keyring entry is gone but the user supplies a passphrase:
// re-wraps the current DEK. Only valid while unlocked... instead this
// path re-derives: it cannot decrypt without the old KEK, so it
// reports ErrNoKey and keeps the vault locked.
func (v *Vault) adoptPassphrase(_ context.Context, _ []byte) error {
	return fmt.Errorf("%w: keyring entry missing; restore from backup (vault export) or re-enter via keyring", ErrNoKey)
}

// installDEKFromDEK installs a freshly generated DEK (already the
// current version) without unwrapping.
func (v *Vault) installDEKFromDEK(ctx context.Context, dek [32]byte, ver int, kind string) error {
	values, names, err := v.loadAllValues(ctx, dek)
	if err != nil {
		zero(dek[:])
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		zero(dek[:])
		return store.ErrClosed
	}
	v.clearDEKLocked()
	v.dek = &dek
	lockMemory(v.dek[:])
	v.dekVersion = ver
	v.kekKind = kind
	v.touchLocked()
	v.redactor = BuildRedactor(values, names)
	v.startWatcherLocked()
	return nil
}

// Lock zeroes and drops the DEK. Tool resolutions after this return the
// unlock question until Unlock succeeds.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.stopWatcherLocked()
	v.clearDEKLocked()
}

// clearDEKLocked zeroes the installed DEK. Callers hold v.mu.
func (v *Vault) clearDEKLocked() {
	if v.dek != nil {
		unlockMemory(v.dek[:])
		zero(v.dek[:])
		v.dek = nil
	}
	v.dekVersion = 0
	v.kekKind = ""
	v.redactor = NewRedactor()
}

// touchLocked records activity for auto-lock. Callers hold v.mu.
func (v *Vault) touchLocked() { v.lastActivity = time.Now() }

// Touch records user/tool activity to defer auto-lock.
func (v *Vault) Touch() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek != nil {
		v.touchLocked()
	}
}

// startWatcherLocked launches the auto-lock watcher (one goroutine via
// safego.Go). Callers hold v.mu.
func (v *Vault) startWatcherLocked() {
	timeout := v.idleTimeout
	if !v.opts.IdleTimeoutSet {
		if v.kekKind == kekPassphrase {
			timeout = DefaultPassphraseIdleTimeout
		} else {
			timeout = 0
		}
		v.idleTimeout = timeout
	}
	if timeout <= 0 || v.watchRunning {
		return
	}
	v.stopWatch = make(chan struct{})
	v.watchRunning = true
	stop := v.stopWatch
	safego.Go("vault-autolock", nil, func() { v.watchLoop(stop, timeout) })
}

// stopWatcherLocked stops the watcher. Callers hold v.mu.
func (v *Vault) stopWatcherLocked() {
	if v.watchRunning {
		close(v.stopWatch)
		v.watchRunning = false
	}
}

// watchLoop locks the vault after timeout of inactivity.
func (v *Vault) watchLoop(stop <-chan struct{}, timeout time.Duration) {
	tick := time.Minute
	v.mu.Lock()
	if v.watchTick > 0 {
		tick = v.watchTick
	}
	v.mu.Unlock()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			v.mu.Lock()
			idle := time.Since(v.lastActivity)
			locked := v.dek == nil
			v.mu.Unlock()
			if locked {
				return
			}
			if idle >= timeout {
				v.Lock()
				return
			}
		}
	}
}

// Set creates or replaces an entry. The write is one porter
// transaction: a crash mid-write leaves the previous version intact.
func (v *Vault) Set(ctx context.Context, p SetParams) (Metadata, error) {
	if err := validateName(p.Name); err != nil {
		return Metadata{}, err
	}
	if !ValidType(p.Type) {
		return Metadata{}, fmt.Errorf("%w: %q", ErrBadType, p.Type)
	}
	if len(p.Value) == 0 {
		return Metadata{}, fmt.Errorf("vault: empty value for %q", p.Name)
	}
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return Metadata{}, needsUnlock(p.Name)
	}
	dek := *v.dek
	dekVer := v.dekVersion
	v.mu.Unlock()

	metaJSON, err := json.Marshal(stringMap(p.Meta))
	if err != nil {
		return Metadata{}, err
	}
	scopes := withGeneral(p.Scopes)
	scopesJSON, _ := json.Marshal(scopes)
	toolsJSON, _ := json.Marshal(p.AllowedTools)
	domainsJSON, _ := json.Marshal(normalizeDomains(p.Domains))
	var expires *string
	if p.ExpiresAt != nil {
		s := p.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expires = &s
	}

	var out Metadata
	err = v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		var id string
		var entryVer int
		rows, err := ex.Query(ctx, `SELECT id, entry_version FROM secrets WHERE name=?`, p.Name)
		if err != nil {
			return err
		}
		found := rows.Next()
		if found {
			var ev int
			if err := rows.Scan(&id, &ev); err != nil {
				_ = rows.Close()
				return err
			}
			entryVer = ev + 1
		}
		rerr := rows.Err()
		_ = rows.Close()
		if rerr != nil {
			return rerr
		}
		if !found {
			id = "sec_" + hexID()
			entryVer = 1
		}
		nonce, sealed, err := sealEntry(dek, id, p.Name, p.Type, entryVer, p.Value)
		if err != nil {
			return err
		}
		now := utcNow()
		if !found {
			_, err = ex.Exec(ctx, `INSERT INTO secrets(id,name,type,metadata,scopes,allowed_tools,domains,ciphertext,nonce,entry_version,dek_version,created_at,updated_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				id, p.Name, p.Type, string(metaJSON), string(scopesJSON), string(toolsJSON), string(domainsJSON), sealed, nonce, entryVer, dekVer, now, now, expires)
			if err != nil {
				return err
			}
		} else {
			_, err = ex.Exec(ctx, `UPDATE secrets SET type=?,metadata=?,scopes=?,allowed_tools=?,domains=?,ciphertext=?,nonce=?,entry_version=?,dek_version=?,updated_at=?,expires_at=? WHERE id=?`,
				p.Type, string(metaJSON), string(scopesJSON), string(toolsJSON), string(domainsJSON), sealed, nonce, entryVer, dekVer, now, expires, id)
			if err != nil {
				return err
			}
		}
		out, err = v.readMetadataLocked(ctx, ex, id)
		return err
	})
	if err != nil {
		return Metadata{}, err
	}
	v.afterValueChange(ctx)
	return out, nil
}

// UpdatePolicy changes metadata/policy without touching the value.
func (v *Vault) UpdatePolicy(ctx context.Context, name string, p PolicyParams) (Metadata, error) {
	if err := validateName(name); err != nil {
		return Metadata{}, err
	}
	var out Metadata
	err := v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		md, err := v.readMetadataByName(ctx, ex, name)
		if err != nil {
			return err
		}
		sets := []string{"updated_at=?"}
		args := []any{utcNow()}
		if p.Meta != nil {
			b, err := json.Marshal(stringMap(*p.Meta))
			if err != nil {
				return err
			}
			sets = append(sets, "metadata=?")
			args = append(args, string(b))
			md.Meta = stringMap(*p.Meta)
		}
		if p.Scopes != nil {
			b, _ := json.Marshal(withGeneral(*p.Scopes))
			sets = append(sets, "scopes=?")
			args = append(args, string(b))
			md.Scopes = withGeneral(*p.Scopes)
		}
		if p.AllowedTools != nil {
			b, _ := json.Marshal(*p.AllowedTools)
			sets = append(sets, "allowed_tools=?")
			args = append(args, string(b))
			md.AllowedTools = *p.AllowedTools
		}
		if p.Domains != nil {
			b, _ := json.Marshal(normalizeDomains(*p.Domains))
			sets = append(sets, "domains=?")
			args = append(args, string(b))
			md.Domains = normalizeDomains(*p.Domains)
		}
		if p.ExpiresAt != nil {
			if *p.ExpiresAt == nil {
				sets = append(sets, "expires_at=NULL")
				md.ExpiresAt = nil
			} else {
				s := (*p.ExpiresAt).UTC().Format(time.RFC3339Nano)
				sets = append(sets, "expires_at=?")
				args = append(args, s)
				md.ExpiresAt = *p.ExpiresAt
			}
		}
		args = append(args, name)
		q := `UPDATE secrets SET ` + strings.Join(sets, ",") + ` WHERE name=?`
		res, err := ex.Exec(ctx, q, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		out = md
		return nil
	})
	if err != nil {
		return Metadata{}, err
	}
	v.Touch()
	return out, nil
}

// Delete removes an entry. Irreversible: callers (CLI/API/tool) must
// confirm with the user first (Section 6.4).
func (v *Vault) Delete(ctx context.Context, name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	err := v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		res, err := ex.Exec(ctx, `DELETE FROM secrets WHERE name=?`, name)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	v.afterValueChange(ctx)
	return nil
}

// Describe returns metadata for one entry, never the value.
func (v *Vault) Describe(ctx context.Context, name string) (Metadata, error) {
	if err := validateName(name); err != nil {
		return Metadata{}, err
	}
	var out Metadata
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var err error
		out, err = v.readMetadataByName(ctx, q, name)
		return err
	})
	return out, err
}

// List returns metadata for all entries, never values.
func (v *Vault) List(ctx context.Context) ([]Metadata, error) {
	var out []Metadata
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,created_at,updated_at,last_used_at,expires_at,entry_version,dek_version FROM secrets ORDER BY name`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			md, err := scanMetadata(rows)
			if err != nil {
				return err
			}
			out = append(out, md)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []Metadata{}
	}
	return out, nil
}

// Rotate generates a fresh DEK, re-seals every entry and re-wraps in a
// single porter transaction. A crash mid-rotate leaves either the old
// version (rollback) or the complete new one; Open finishes or rolls
// back a half-applied rotation by DEK version.
func (v *Vault) Rotate(ctx context.Context) (oldVer, newVer int, err error) {
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return 0, 0, ErrLocked
	}
	oldDEK := *v.dek
	oldV := v.dekVersion
	v.mu.Unlock()

	newDEK, err := newDEK()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			zero(newDEK[:])
		}
	}()

	kek, kind, salt, err := v.currentKEK(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer zero(kek)
	_ = salt

	newVer = oldV + 1
	nonce, wrapped, err := wrapDEK(kek, newVer, newDEK)
	if err != nil {
		return 0, 0, err
	}
	err = v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO vault_keys(dek_version,wrapped,wrap_nonce,kek_kind,salt,created_at) VALUES(?,?,?,?,?,?)`,
			newVer, wrapped, nonce, kind, salt, utcNow()); err != nil {
			return err
		}
		return v.resealAll(ctx, ex, oldDEK, oldV, newDEK, newVer, kek)
	})
	if err != nil {
		return 0, 0, err
	}
	// Install the new DEK and rebuild redaction.
	values, names, lerr := v.loadAllValuesWith(ctx, newDEK)
	if lerr != nil {
		zero(newDEK[:])
		// Rotation committed; vault keeps working after re-unlock.
		v.Lock()
		return oldV, newVer, lerr
	}
	v.mu.Lock()
	v.clearDEKLocked()
	v.dek = &newDEK
	lockMemory(v.dek[:])
	v.dekVersion = newVer
	v.touchLocked()
	v.redactor = BuildRedactor(values, names)
	v.startWatcherLocked()
	v.mu.Unlock()
	return oldV, newVer, nil
}

// Usage lists where a secret was used (never values).
func (v *Vault) Usage(ctx context.Context, name string, limit int) ([]UseRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var out []UseRecord
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT secret_name,tool,run_id,dest,project,at FROM secret_uses WHERE secret_name=? ORDER BY id DESC LIMIT ?`, name, limit)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var r UseRecord
			var at string
			if err := rows.Scan(&r.SecretName, &r.Tool, &r.RunID, &r.Dest, &r.Project, &at); err != nil {
				return err
			}
			r.At, _ = time.Parse(time.RFC3339Nano, at)
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []UseRecord{}
	}
	return out, nil
}

// VerifyAll decrypts every entry and reports failures. Used after
// crash recovery to prove no entry is corrupt (Section 20.2).
func (v *Vault) VerifyAll(ctx context.Context) error {
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return ErrLocked
	}
	dek := *v.dek
	v.mu.Unlock()
	return v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,name,type,ciphertext,nonce,entry_version,dek_version FROM secrets`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, name, typ string
			var ct, n []byte
			var ev, dv int
			if err := rows.Scan(&id, &name, &typ, &ct, &n, &ev, &dv); err != nil {
				return err
			}
			dd := dek
			if dv != v.currentDEKVersion() {
				krow, kerr := v.readKeysRowQ(ctx, q, dv)
				if kerr != nil {
					return kerr
				}
				kek, _, _, kerr := v.currentKEK(ctx)
				if kerr != nil {
					return kerr
				}
				dk, kerr := unwrapDEK(kek, dv, krow.wrapNonce, krow.wrapped)
				zero(kek)
				if kerr != nil {
					return kerr
				}
				dd = dk
				zero(dk[:])
			}
			plain, err := openEntry(dd, id, name, typ, ev, n, ct)
			if err != nil {
				return err
			}
			zero(plain)
		}
		return rows.Err()
	})
}

func (v *Vault) currentDEKVersion() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.dekVersion
}

// afterValueChange rebuilds the redactor and touches activity.
func (v *Vault) afterValueChange(ctx context.Context) {
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return
	}
	dek := *v.dek
	v.mu.Unlock()
	values, names, err := v.loadAllValues(ctx, dek)
	if err != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.dek != nil {
		v.redactor = BuildRedactor(values, names)
		v.touchLocked()
	}
}

// loadAllValues decrypts every entry value for redactor rebuilds.
func (v *Vault) loadAllValues(ctx context.Context, dek [32]byte) ([][]byte, []string, error) {
	return v.loadAllValuesWith(ctx, dek)
}

func (v *Vault) loadAllValuesWith(ctx context.Context, dek [32]byte) ([][]byte, []string, error) {
	var values [][]byte
	var names []string
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,name,type,ciphertext,nonce,entry_version FROM secrets`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, name, typ string
			var ct, n []byte
			var ev int
			if err := rows.Scan(&id, &name, &typ, &ct, &n, &ev); err != nil {
				return err
			}
			plain, err := openEntry(dek, id, name, typ, ev, n, ct)
			if err != nil {
				for _, b := range values {
					zero(b)
				}
				return err
			}
			values = append(values, plain)
			names = append(names, name)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	return values, names, nil
}

// readMetadataLocked reads one entry's metadata by id inside a write.
func (v *Vault) readMetadataLocked(ctx context.Context, ex store.Execer, id string) (Metadata, error) {
	rows, err := ex.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,created_at,updated_at,last_used_at,expires_at,entry_version,dek_version FROM secrets WHERE id=?`, id)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return Metadata{}, ErrNotFound
	}
	var md Metadata
	if err := scanMetadataInto(rows, &md); err != nil {
		return Metadata{}, err
	}
	return md, rows.Err()
}

func (v *Vault) readMetadataByName(ctx context.Context, q interface {
	Query(ctx context.Context, query string, args ...any) (store.Rows, error)
}, name string) (Metadata, error) {
	rows, err := q.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,created_at,updated_at,last_used_at,expires_at,entry_version,dek_version FROM secrets WHERE name=?`, name)
	if err != nil {
		return Metadata{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return Metadata{}, ErrNotFound
	}
	var md Metadata
	if err := scanMetadataInto(rows, &md); err != nil {
		return Metadata{}, err
	}
	return md, rows.Err()
}

func scanMetadata(rows store.Rows) (Metadata, error) {
	var md Metadata
	if err := scanMetadataInto(rows, &md); err != nil {
		return Metadata{}, err
	}
	return md, nil
}

func scanMetadataInto(rows store.Rows, md *Metadata) error {
	var metaJSON, scopesJSON, toolsJSON, domainsJSON string
	var created, updated string
	var lastUsed, expires *string
	var ev, dv int
	var id, name, typ string
	if err := rows.Scan(&id, &name, &typ, &metaJSON, &scopesJSON, &toolsJSON, &domainsJSON, &created, &updated, &lastUsed, &expires, &ev, &dv); err != nil {
		return err
	}
	md.ID = id
	md.Name = name
	md.Type = typ
	md.Meta = map[string]string{}
	_ = json.Unmarshal([]byte(metaJSON), &md.Meta)
	_ = json.Unmarshal([]byte(scopesJSON), &md.Scopes)
	_ = json.Unmarshal([]byte(toolsJSON), &md.AllowedTools)
	_ = json.Unmarshal([]byte(domainsJSON), &md.Domains)
	md.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	md.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if lastUsed != nil {
		if t, err := time.Parse(time.RFC3339Nano, *lastUsed); err == nil {
			md.LastUsedAt = &t
		}
	}
	if expires != nil {
		if t, err := time.Parse(time.RFC3339Nano, *expires); err == nil {
			md.ExpiresAt = &t
		}
	}
	md.EntryVersion = ev
	md.DEKVersion = dv
	return nil
}

// validateName checks entry names.
func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: %q (lowercase letters, digits, -, _; max 64 chars)", ErrBadName, name)
	}
	return nil
}

func stringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// defaultScopes applies the Section 20.1 default: entries without an
// explicit scope list are general. Explicit scopes are exact: no
// implicit general access, so a project-scoped entry stays hidden from
// other projects.
func withGeneral(scopes []string) []string {
	if len(scopes) == 0 {
		return []string{ScopeGeneral}
	}
	return append([]string(nil), scopes...)
}

func normalizeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	seen := map[string]bool{}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		d = strings.TrimPrefix(d, "https://")
		d = strings.TrimPrefix(d, "http://")
		if i := strings.Index(d, "/"); i >= 0 {
			d = d[:i]
		}
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

func hexID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "no keyring entry")
}
