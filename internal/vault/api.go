package vault

import (
	"context"
	"fmt"
	"time"
)

// Vault API handler core logic (Section 3.5 families vault.* and 20.1).
// The gateway owns transport and auth; every function here is the
// value-safe core it calls. Values travel only in Create/Update/Reveal
// calls, never in conversation messages. Method mapping:
//
//	vault.create       -> Create
//	vault.update       -> Update (value or policy)
//	vault.list_metadata-> ListMetadata
//	vault.lock         -> LockVault
//	vault.unlock       -> UnlockVault
//	vault.delete       -> DeleteEntry
//	vault.get          -> Reveal (explicit user terminal only)
//	vault.edit         -> EditPolicy
//	vault.rotate       -> RotateKeys
//	vault.export       -> ExportVault
//	vault.import       -> ImportVault
//	vault.usage        -> UsageOf
//	vault.describe     -> DescribeEntry
//
// The agent `secrets` tool has no reveal path: only the CLI/API Reveal
// with an explicit user request returns a value, and only to the
// user's own terminal, never to the model.

// CreateParams mirrors api.VaultEntryParams plus the secret value,
// which travels only in this dedicated call.
type CreateParams struct {
	Name         string
	Type         string
	Value        []byte
	Metadata     map[string]string
	Scopes       []string
	AllowedTools []string
	Domains      []string
	ExpiresAt    *time.Time
}

// UpdateParams mirrors CreateParams for vault.update.
type UpdateParams = CreateParams

// Create implements vault.create.
func (v *Vault) Create(ctx context.Context, p CreateParams) (Metadata, error) {
	defer ZeroBytes(p.Value)
	if _, err := v.Describe(ctx, p.Name); err == nil {
		return Metadata{}, fmt.Errorf("%w: %q (use vault.update)", ErrExists, p.Name)
	} else if !isNotFoundErr(err) {
		return Metadata{}, err
	}
	md, err := v.Set(ctx, SetParams{
		Name: p.Name, Type: p.Type, Value: p.Value, Meta: p.Metadata,
		Scopes: p.Scopes, AllowedTools: p.AllowedTools, Domains: p.Domains, ExpiresAt: p.ExpiresAt,
	})
	ZeroBytes(p.Value)
	return md, err
}

// Update implements vault.update (value and/or policy).
func (v *Vault) Update(ctx context.Context, p UpdateParams) (Metadata, error) {
	if len(p.Value) > 0 {
		md, err := v.Set(ctx, SetParams{
			Name: p.Name, Type: p.Type, Value: p.Value, Meta: p.Metadata,
			Scopes: p.Scopes, AllowedTools: p.AllowedTools, Domains: p.Domains, ExpiresAt: p.ExpiresAt,
		})
		ZeroBytes(p.Value)
		return md, err
	}
	// Policy-only update: type change without a value is rejected.
	cur, err := v.Describe(ctx, p.Name)
	if err != nil {
		return Metadata{}, err
	}
	if p.Type != "" && p.Type != cur.Type {
		return Metadata{}, fmt.Errorf("vault: changing type of %q requires a new value (use vault.update with value)", p.Name)
	}
	exp := &p.ExpiresAt
	md, err := v.UpdatePolicy(ctx, p.Name, PolicyParams{
		Meta: &p.Metadata, Scopes: &p.Scopes, AllowedTools: &p.AllowedTools, Domains: &p.Domains, ExpiresAt: exp,
	})
	return md, err
}

// ListMetadata implements vault.list_metadata: never values.
func (v *Vault) ListMetadata(ctx context.Context) ([]Metadata, error) {
	return v.List(ctx)
}

// DescribeEntry implements vault describe/show: never the value.
func (v *Vault) DescribeEntry(ctx context.Context, name string) (Metadata, error) {
	return v.Describe(ctx, name)
}

// LockVault implements vault.lock.
func (v *Vault) LockVault() { v.Lock() }

// UnlockVault implements vault.unlock. Empty passphrase attempts the
// keyring path; failures return ErrNoKey with the rest of the harness
// unaffected.
func (v *Vault) UnlockVault(ctx context.Context, passphrase []byte) error {
	defer ZeroBytes(passphrase)
	return v.Unlock(ctx, passphrase)
}

// DeleteEntry implements vault.delete. The gateway/CLI must confirm
// with the user first (Section 6.4, irreversible).
func (v *Vault) DeleteEntry(ctx context.Context, name string, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("vault: deleting %q needs explicit user confirmation", name)
	}
	return v.Delete(ctx, name)
}

// Reveal implements vault.get: explicit user terminal reveal only. The
// agent path must never call this; it returns the value for display in
// the user's own terminal or clipboard. Callers zero the copy after use.
func (v *Vault) Reveal(ctx context.Context, name string) ([]byte, error) {
	res, err := v.Resolve(ctx, ResolveRequest{Ref: name, Tool: "vault-get", Dest: DestEnv})
	if err != nil {
		return nil, err
	}
	return res.Value, nil
}

// EditPolicy implements vault.edit: scopes, tools, domains, metadata.
func (v *Vault) EditPolicy(ctx context.Context, name string, p PolicyParams) (Metadata, error) {
	return v.UpdatePolicy(ctx, name, p)
}

// RotateKeys implements vault.rotate: new DEK + rewrap in one
// transaction. Passphrase vaults need the passphrase.
func (v *Vault) RotateKeys(ctx context.Context, passphrase []byte) (oldVer, newVer int, err error) {
	defer ZeroBytes(passphrase)
	v.mu.Lock()
	kind := v.kekKind
	locked := v.dek == nil
	v.mu.Unlock()
	if locked {
		return 0, 0, ErrLocked
	}
	if kind == kekPassphrase {
		if len(passphrase) == 0 {
			return 0, 0, fmt.Errorf("%w: passphrase required to rotate a passphrase vault", ErrNoKey)
		}
		return v.RotateWithPassphrase(ctx, passphrase)
	}
	return v.Rotate(ctx)
}

// ExportVault implements vault.export: encrypted backup bytes.
func (v *Vault) ExportVault(ctx context.Context, backupPassphrase []byte) ([]byte, error) {
	defer ZeroBytes(backupPassphrase)
	return v.Export(ctx, backupPassphrase)
}

// ImportVault implements vault.import: merges an encrypted backup.
func (v *Vault) ImportVault(ctx context.Context, data, backupPassphrase []byte) (int, error) {
	defer ZeroBytes(backupPassphrase)
	return v.Import(ctx, data, backupPassphrase)
}

// UsageOf implements vault usage query: where a secret was used.
func (v *Vault) UsageOf(ctx context.Context, name string, limit int) ([]UseRecord, error) {
	if _, err := v.Describe(ctx, name); err != nil {
		return nil, err
	}
	return v.Usage(ctx, name, limit)
}

// GenerateEntry implements vault.generate: random value stored
// directly; only metadata returns.
func (v *Vault) GenerateEntry(ctx context.Context, p GenerateParams) (Metadata, error) {
	if p.Type == "" {
		p.Type = TypeToken
	}
	if p.Name == "" {
		return Metadata{}, ErrBadName
	}
	return v.GenerateAndStore(ctx, p)
}

func isNotFoundErr(err error) bool {
	return err != nil && (isNotFound(err) || contains(err.Error(), ErrNotFound.Error()))
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
