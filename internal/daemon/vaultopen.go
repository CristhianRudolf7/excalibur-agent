// Vault opening for the daemon and the CLI (Section 20.1).
//
// The vault starts in one of three ways. A declared passphrase file
// (0600, checked by the vault) unlocks it at once, for unattended
// starts. Otherwise the platform keyring unlocks it when reachable,
// and the first use initializes the data key there. Without either,
// the vault starts locked and the rest of the harness keeps running:
// tools that need a secret then wait on the user (Section 20.1).
package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// VaultOpenOptions declares how the vault opens.
type VaultOpenOptions struct {
	// Path is vault.db in the data directory.
	Path string
	// Keyring supplies the master key when reachable. Nil means
	// passphrase-only operation.
	Keyring platform.Keyring
	// PassphraseFile unlocks at open; it must be mode 0600.
	PassphraseFile string
}

// OpenVault opens the vault and applies the start flow of Section
// 20.1. The returned vault may be locked; a missing master key is not
// an error, because the daemon must start without it.
func OpenVault(ctx context.Context, opts VaultOpenOptions) (*vault.Vault, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("daemon: vault needs a path")
	}
	v, err := vault.Open(ctx, opts.Path, vault.Options{
		Keyring:        opts.Keyring,
		PassphraseFile: opts.PassphraseFile,
	})
	if err != nil {
		return nil, fmt.Errorf("daemon: open vault: %w", err)
	}
	if v.IsLocked() {
		// Keyring-backed vaults unlock here without a prompt; with no
		// key available the error is ErrNoKey and the vault stays locked.
		if err := v.Unlock(ctx, nil); err != nil && !errors.Is(err, vault.ErrNoKey) {
			_ = v.Close()
			return nil, fmt.Errorf("daemon: unlock vault: %w", err)
		}
	}
	return v, nil
}

// VaultSecretResolver is the provider-key hook over an open vault: the
// vault wins over the environment and config (Sections 19.2, 20.1).
// It resolves at the time the provider is built.
func VaultSecretResolver(v *vault.Vault) func(ctx context.Context, name string) (string, error) {
	if v == nil {
		return nil
	}
	return ProviderHook(VaultAdapter{V: v}, "")
}
