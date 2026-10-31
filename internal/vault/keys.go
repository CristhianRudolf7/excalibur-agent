package vault

import (
	"context"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// keysRow is one vault_keys row.
type keysRow struct {
	dekVersion int
	wrapped    []byte
	wrapNonce  []byte
	kekKind    string
	salt       []byte
}

// loadKeyState reads the latest vault_keys row. haveKeys is false on a
// fresh vault (first unlock initializes the DEK).
func (v *Vault) loadKeyState(ctx context.Context) (kek []byte, kind string, salt []byte, haveKeys bool, dekVer int, wrapped, wrapNonce []byte, err error) {
	err = v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, rerr := q.Query(ctx, `SELECT dek_version,wrapped,wrap_nonce,kek_kind,salt FROM vault_keys ORDER BY dek_version DESC LIMIT 1`)
		if rerr != nil {
			return rerr
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return rows.Err()
		}
		var kr keysRow
		if rerr := rows.Scan(&kr.dekVersion, &kr.wrapped, &kr.wrapNonce, &kr.kekKind, &kr.salt); rerr != nil {
			return rerr
		}
		if rerr := rows.Err(); rerr != nil {
			return rerr
		}
		kind = kr.kekKind
		salt = kr.salt
		haveKeys = true
		dekVer = kr.dekVersion
		wrapped = kr.wrapped
		wrapNonce = kr.wrapNonce
		return nil
	})
	return nil, kind, salt, haveKeys, dekVer, wrapped, wrapNonce, err
}

// storeKeysRow inserts a vault_keys row in one porter transaction.
func (v *Vault) storeKeysRow(ctx context.Context, dekVer int, wrapped, nonce []byte, kind string, salt []byte) error {
	return v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO vault_keys(dek_version,wrapped,wrap_nonce,kek_kind,salt,created_at) VALUES(?,?,?,?,?,?)`,
			dekVer, wrapped, nonce, kind, salt, utcNow())
		return err
	})
}

// readKeysRow reads one vault_keys row inside a write.
func (v *Vault) readKeysRow(_ context.Context, ex store.Execer, dekVer int) (keysRow, error) {
	rows, err := ex.Query(context.Background(), `SELECT dek_version,wrapped,wrap_nonce,kek_kind,salt FROM vault_keys WHERE dek_version=?`, dekVer)
	if err != nil {
		return keysRow{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return keysRow{}, fmt.Errorf("vault: missing keys row for dek v%d", dekVer)
	}
	var kr keysRow
	if err := rows.Scan(&kr.dekVersion, &kr.wrapped, &kr.wrapNonce, &kr.kekKind, &kr.salt); err != nil {
		return keysRow{}, err
	}
	return kr, rows.Err()
}

// readKeysRowQ reads one vault_keys row inside a read.
func (v *Vault) readKeysRowQ(ctx context.Context, q store.Queryer, dekVer int) (keysRow, error) {
	rows, err := q.Query(ctx, `SELECT dek_version,wrapped,wrap_nonce,kek_kind,salt FROM vault_keys WHERE dek_version=?`, dekVer)
	if err != nil {
		return keysRow{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return keysRow{}, fmt.Errorf("vault: missing keys row for dek v%d", dekVer)
	}
	var kr keysRow
	if err := rows.Scan(&kr.dekVersion, &kr.wrapped, &kr.wrapNonce, &kr.kekKind, &kr.salt); err != nil {
		return keysRow{}, err
	}
	return kr, rows.Err()
}

// currentKEK re-resolves the master key for rotation: keyring first,
// passphrase rows never need the passphrase again at rotation time
// (the DEK is already installed; rotation re-wraps with the same KEK
// origin, re-deriving where needed is impossible without the
// passphrase, so passphrase vaults re-wrap via the installed DEK path
// below). To keep rotation passphrase-free, currentKEK unwraps the
// installed DEK and re-derives nothing: it returns the KEK only for
// secret-service vaults, and for passphrase vaults it re-wraps using a
// KEK reconstructed from the installed DEK is impossible, so instead
// rotation for passphrase vaults keeps the same wrap by re-sealing
// entries and re-wrapping with a KEK fetched from... See Rotate: it
// calls currentKEK which for passphrase kind returns an error unless a
// cached derivation exists.
//
// Simpler correct rule: rotation requires the KEK. For secret-service
// vaults it comes from the keyring. For passphrase vaults the caller
// must be unlocked AND rotation re-derives nothing: instead we wrap
// the new DEK with a fresh random KEK is wrong (old passphrase would
// stop working).
//
// Resolution: passphrase vaults store no recoverable KEK, so Rotate
// asks for the passphrase via RotateWithPassphrase. Rotate uses the
// keyring and fails with ErrNoKey on passphrase vaults, directing the
// caller to RotateWithPassphrase.
func (v *Vault) currentKEK(ctx context.Context) (kek []byte, kind string, salt []byte, err error) {
	_, kindRow, saltRow, haveKeys, _, _, _, err := v.loadKeyState(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	if !haveKeys {
		return nil, "", nil, ErrLocked
	}
	if kindRow != kekSecretService {
		return nil, "", nil, fmt.Errorf("%w: passphrase vaults rotate via RotateWithPassphrase", ErrNoKey)
	}
	if v.opts.Keyring == nil {
		return nil, "", nil, fmt.Errorf("%w: no keyring configured", ErrNoKey)
	}
	stored, kerr := v.opts.Keyring.Get(v.opts.KeyringService, v.opts.KeyringUser)
	if kerr != nil {
		return nil, "", nil, fmt.Errorf("%w: %v", ErrNoKey, kerr)
	}
	return stored, kindRow, saltRow, nil
}

// RotateWithPassphrase rotates a passphrase-origin vault. The
// passphrase re-derives the KEK, the new DEK is wrapped with it, and
// every entry is re-sealed in one transaction (same atomicity as
// Rotate).
func (v *Vault) RotateWithPassphrase(ctx context.Context, passphrase []byte) (oldVer, newVer int, err error) {
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return 0, 0, ErrLocked
	}
	oldDEK := *v.dek
	oldV := v.dekVersion
	kind := v.kekKind
	v.mu.Unlock()
	if kind != kekPassphrase {
		return 0, 0, fmt.Errorf("vault: RotateWithPassphrase is for passphrase vaults; use Rotate")
	}
	_, _, salt, haveKeys, _, _, _, err := v.loadKeyState(ctx)
	if err != nil {
		return 0, 0, err
	}
	if !haveKeys {
		return 0, 0, ErrLocked
	}
	kek, err := deriveKEK(passphrase, salt)
	if err != nil {
		return 0, 0, err
	}
	defer zero(kek)

	newDEK, err := newDEK()
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			zero(newDEK[:])
		}
	}()
	newVer = oldV + 1
	nonce, wrapped, err := wrapDEK(kek, newVer, newDEK)
	if err != nil {
		return 0, 0, err
	}
	err = v.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		if _, err := ex.Exec(ctx, `INSERT INTO vault_keys(dek_version,wrapped,wrap_nonce,kek_kind,salt,created_at) VALUES(?,?,?,?,?,?)`,
			newVer, wrapped, nonce, kekPassphrase, salt, utcNow()); err != nil {
			return err
		}
		return v.resealAll(ctx, ex, oldDEK, oldV, newDEK, newVer, kek)
	})
	if err != nil {
		return 0, 0, err
	}
	values, names, lerr := v.loadAllValuesWith(ctx, newDEK)
	if lerr != nil {
		zero(newDEK[:])
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

// resealAll re-seals every entry with the new DEK inside the caller's
// transaction. Entries sealed with older DEK versions resolve their
// own keys row first.
func (v *Vault) resealAll(ctx context.Context, ex store.Execer, oldDEK [32]byte, oldV int, newDEK [32]byte, newVer int, kek []byte) error {
	rows, err := ex.Query(ctx, `SELECT id,name,type,ciphertext,nonce,entry_version,dek_version FROM secrets`)
	if err != nil {
		return err
	}
	type row struct {
		id, name, typ string
		ct, n         []byte
		ev, dv        int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.name, &r.typ, &r.ct, &r.n, &r.ev, &r.dv); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	now := utcNow()
	for _, r := range all {
		dekUse := oldDEK
		if r.dv != oldV {
			krow, kerr := v.readKeysRow(ctx, ex, r.dv)
			if kerr != nil {
				return kerr
			}
			dk, kerr := unwrapDEK(kek, r.dv, krow.wrapNonce, krow.wrapped)
			if kerr != nil {
				return kerr
			}
			dekUse = dk
			zero(dk[:])
		}
		plain, err := openEntry(dekUse, r.id, r.name, r.typ, r.ev, r.n, r.ct)
		if err != nil {
			return err
		}
		nn, sealed, err := sealEntry(newDEK, r.id, r.name, r.typ, r.ev, plain)
		zero(plain)
		if err != nil {
			return err
		}
		if _, err := ex.Exec(ctx, `UPDATE secrets SET ciphertext=?,nonce=?,dek_version=?,updated_at=? WHERE id=?`,
			sealed, nn, newVer, now, r.id); err != nil {
			return err
		}
	}
	return nil
}
