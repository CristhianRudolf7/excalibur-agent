package vault

import (
	"context"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// vaultRegistry builds the migration registry for vault.db.
// Schema version 1 carries the full Section 20.1 and Appendix A
// tables; expand-then-contract per Section 4.6.

// schemaV1 creates the vault tables of Appendix A: secrets (one row
// per entry with ciphertext, nonce, entry and DEK versions),
// vault_keys (wrapped DEK per version), secret_uses (tool, run, dest,
// never the value) and drafts (encrypted chat-pasted candidates).
const schemaV1 = `
CREATE TABLE IF NOT EXISTS secrets (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	type TEXT NOT NULL,
	metadata TEXT NOT NULL DEFAULT '{}',
	scopes TEXT NOT NULL DEFAULT '[]',
	allowed_tools TEXT NOT NULL DEFAULT '[]',
	domains TEXT NOT NULL DEFAULT '[]',
	ciphertext BLOB NOT NULL,
	nonce BLOB NOT NULL,
	entry_version INTEGER NOT NULL DEFAULT 1,
	dek_version INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_used_at TEXT,
	expires_at TEXT
);
CREATE TABLE IF NOT EXISTS vault_keys (
	dek_version INTEGER PRIMARY KEY,
	wrapped BLOB NOT NULL,
	wrap_nonce BLOB NOT NULL,
	kek_kind TEXT NOT NULL,
	salt BLOB,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS secret_uses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	secret_name TEXT NOT NULL,
	tool TEXT NOT NULL,
	run_id TEXT NOT NULL DEFAULT '',
	dest TEXT NOT NULL DEFAULT '',
	project TEXT NOT NULL DEFAULT '',
	at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_secret_uses_name ON secret_uses(secret_name);
CREATE TABLE IF NOT EXISTS drafts (
	id TEXT PRIMARY KEY,
	created_at TEXT NOT NULL,
	ciphertext BLOB NOT NULL,
	nonce BLOB NOT NULL,
	dek_version INTEGER NOT NULL,
	hint TEXT NOT NULL DEFAULT ''
);
`

// vaultRegistry builds the migration registry for vault.db.
func vaultRegistry() store.MigrationRegistry {
	reg := store.NewRegistry()
	reg.Register(store.Migration{
		Version: 1,
		Name:    "vault-schema-v1",
		Up: func(ctx context.Context, ex store.Execer) error {
			if _, err := ex.Exec(ctx, schemaV1); err != nil {
				return fmt.Errorf("vault: schema v1: %w", err)
			}
			return nil
		},
	})
	return reg
}

// openStore opens vault.db behind its own porter (Section 4.2/4.3):
// separate file, synchronous=FULL, 0600 enforced by the store layer.
func openStore(ctx context.Context, path string) (*store.DB, error) {
	opts := store.DefaultOptions()
	opts.SynchronousFULL = true
	db, err := store.Open(ctx, path, opts, vaultRegistry())
	if err != nil {
		return nil, fmt.Errorf("vault: open store: %w", err)
	}
	return db, nil
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
