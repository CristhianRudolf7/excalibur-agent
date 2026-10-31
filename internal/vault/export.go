package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// exportFileVersion versions the encrypted backup envelope.
const exportFileVersion = 1

// exportEnvelope is the encrypted backup: Argon2id (own passphrase and
// salt) + XChaCha20-Poly1305 over the JSON payload. The file never
// contains values in clear.
type exportEnvelope struct {
	Version int    `json:"version"`
	Salt    []byte `json:"salt"`
	Nonce   []byte `json:"nonce"`
	Sealed  []byte `json:"sealed"`
}

type exportPayload struct {
	Entries []exportEntry `json:"entries"`
}

type exportEntry struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Value        string            `json:"value"`
	Meta         map[string]string `json:"metadata"`
	Scopes       []string          `json:"scopes"`
	AllowedTools []string          `json:"allowed_tools"`
	Domains      []string          `json:"domains"`
	ExpiresAt    *string           `json:"expires_at,omitempty"`
}

// Export writes an encrypted backup: every entry (names, metadata and
// values) sealed with a backup passphrase (Argon2id + XChaCha20-
// Poly1305). The exported file contains no cleartext values.
func (v *Vault) Export(ctx context.Context, backupPassphrase []byte) ([]byte, error) {
	if len(backupPassphrase) == 0 {
		return nil, fmt.Errorf("vault: backup passphrase required")
	}
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return nil, ErrLocked
	}
	dek := *v.dek
	v.mu.Unlock()

	var payload exportPayload
	err := v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,ciphertext,nonce,entry_version,dek_version,expires_at FROM secrets ORDER BY name`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, name, typ string
			var metaJSON, scopesJSON, toolsJSON, domainsJSON string
			var ct, n []byte
			var ev, dv int
			var expires *string
			if err := rows.Scan(&id, &name, &typ, &metaJSON, &scopesJSON, &toolsJSON, &domainsJSON, &ct, &n, &ev, &dv, &expires); err != nil {
				return err
			}
			dd := dek
			if dv != v.currentDEKVersion() {
				var krow keysRow
				if rerr := func() error {
					return v.db.Read(ctx, func(ctx context.Context, q2 store.Queryer) error {
						var rerr error
						krow, rerr = v.readKeysRowQ(ctx, q2, dv)
						return rerr
					})
				}(); rerr != nil {
					return rerr
				}
				kek, kerr := v.kekForDecrypt(ctx, krow)
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
			e := exportEntry{Name: name, Type: typ, Value: string(plain)}
			zero(plain)
			_ = json.Unmarshal([]byte(metaJSON), &e.Meta)
			_ = json.Unmarshal([]byte(scopesJSON), &e.Scopes)
			_ = json.Unmarshal([]byte(toolsJSON), &e.AllowedTools)
			_ = json.Unmarshal([]byte(domainsJSON), &e.Domains)
			e.ExpiresAt = expires
			payload.Entries = append(payload.Entries, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	salt, err := newSalt()
	if err != nil {
		return nil, err
	}
	kek, err := deriveKEK(backupPassphrase, salt[:])
	if err != nil {
		return nil, err
	}
	defer zero(kek)
	a, err := aeadX(kek)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := randRead(nonce); err != nil {
		return nil, err
	}
	sealed := a.Seal(nil, nonce, raw, []byte("excalibur-vault-export-v1"))
	zero(raw)
	env := exportEnvelope{Version: exportFileVersion, Salt: salt[:], Nonce: nonce, Sealed: sealed}
	out, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ExportToFile writes Export to path with 0600 permissions.
func (v *Vault) ExportToFile(ctx context.Context, path string, backupPassphrase []byte) error {
	data, err := v.Export(ctx, backupPassphrase)
	if err != nil {
		return err
	}
	defer zero(data)
	return writePrivateFile(path, data)
}

// Import restores entries from an encrypted backup into this vault
// (fresh or existing installation). Values decrypt only with the
// backup passphrase; entries merge by name (backup wins).
func (v *Vault) Import(ctx context.Context, data, backupPassphrase []byte) (int, error) {
	if len(backupPassphrase) == 0 {
		return 0, fmt.Errorf("vault: backup passphrase required")
	}
	v.mu.Lock()
	locked := v.dek == nil
	v.mu.Unlock()
	if locked {
		return 0, ErrLocked
	}
	var env exportEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return 0, fmt.Errorf("vault: bad backup file: %w", err)
	}
	if env.Version != exportFileVersion {
		return 0, fmt.Errorf("vault: unsupported backup version %d", env.Version)
	}
	if len(env.Salt) != saltLen {
		return 0, fmt.Errorf("vault: corrupt backup salt")
	}
	kek, err := deriveKEK(backupPassphrase, env.Salt)
	if err != nil {
		return 0, err
	}
	defer zero(kek)
	a, err := aeadX(kek)
	if err != nil {
		return 0, err
	}
	raw, err := a.Open(nil, env.Nonce, env.Sealed, []byte("excalibur-vault-export-v1"))
	if err != nil {
		return 0, fmt.Errorf("vault: wrong backup passphrase or corrupt file: %w", err)
	}
	defer zero(raw)
	var payload exportPayload
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&payload); err != nil {
		return 0, fmt.Errorf("vault: corrupt backup payload: %w", err)
	}
	count := 0
	for _, e := range payload.Entries {
		if err := validateName(e.Name); err != nil {
			return count, err
		}
		if !ValidType(e.Type) {
			return count, fmt.Errorf("%w: %q", ErrBadType, e.Type)
		}
		var exp *[1]string
		_ = exp
		sp := SetParams{
			Name: e.Name, Type: e.Type, Value: []byte(e.Value),
			Meta: e.Meta, Scopes: e.Scopes, AllowedTools: e.AllowedTools, Domains: e.Domains,
		}
		if e.ExpiresAt != nil {
			t, perr := parseTime(*e.ExpiresAt)
			if perr == nil {
				sp.ExpiresAt = &t
			}
		}
		if _, serr := v.Set(ctx, sp); serr != nil {
			ZeroBytes(sp.Value)
			return count, serr
		}
		ZeroBytes(sp.Value)
		count++
	}
	return count, nil
}

// ImportFromFile reads an encrypted backup file and imports it.
func (v *Vault) ImportFromFile(ctx context.Context, path string, backupPassphrase []byte) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	defer zero(data)
	return v.Import(ctx, data, backupPassphrase)
}
