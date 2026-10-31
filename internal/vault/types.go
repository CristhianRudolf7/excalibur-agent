package vault

import "errors"

// Secret types of Section 20.1. Every entry has one type, a unique
// name, secret bytes kept encrypted, and visible metadata.
const (
	TypeAPIKey             = "api_key"
	TypeProviderCredential = "provider_credential"
	TypeWebsitePassword    = "website_password"
	TypeHost               = "host"
	TypeSSHKey             = "ssh_key"
	TypeToken              = "token"
	TypeNote               = "note"
)

// validTypes enumerates the secret types of the 20.1 table.
var validTypes = map[string]bool{
	TypeAPIKey:             true,
	TypeProviderCredential: true,
	TypeWebsitePassword:    true,
	TypeHost:               true,
	TypeSSHKey:             true,
	TypeToken:              true,
	TypeNote:               true,
}

// Sentinel errors of the vault contract.
var (
	// ErrLocked is returned when the vault is locked and an operation
	// needs the data key. Tool executions waiting on a secret must
	// surface an unlock question (waiting-on-user), never fail.
	ErrLocked = errors.New("vault: vault is locked")
	// ErrNotFound is returned when no entry matches a name or id.
	ErrNotFound = errors.New("vault: secret not found")
	// ErrExists is returned when creating an entry under a taken name.
	ErrExists = errors.New("vault: secret already exists")
	// ErrDenied is returned when a resolution breaks the entry policy
	// (scopes, allowed tools, domains).
	ErrDenied = errors.New("vault: secret use denied by entry policy")
	// ErrExpired is returned when resolving an entry past its expiry.
	ErrExpired = errors.New("vault: secret expired")
	// ErrNoKey is returned when the master key is unavailable
	// (keyring down, forgotten passphrase).
	ErrNoKey = errors.New("vault: master key unavailable")
	// ErrEvalBlocked is returned when the eval profile tries to open
	// or use the vault (Section 23.2: eval never opens the vault).
	ErrEvalBlocked = errors.New("vault: eval profile never opens the vault")
	// ErrBadName is returned for blank or malformed entry names.
	ErrBadName = errors.New("vault: bad secret name")
	// ErrBadType is returned for unknown secret types.
	ErrBadType = errors.New("vault: unknown secret type")
	// ErrDraftNotFound is returned when confirming an unknown draft.
	ErrDraftNotFound = errors.New("vault: draft not found")
)

// ValidType reports whether t is one of the Section 20.1 types.
func ValidType(t string) bool { return validTypes[t] }

// Scopes marks where a secret may be used: "general" and/or project
// ids (Section 9 scopes). An empty scope list means general only.
const ScopeGeneral = "general"

// DestKind names where a resolved value is injected (Section 20.1):
// process environment, HTTP header, browser form field, temp file, or
// the model-gateway provider call.
type DestKind string

// Resolution destinations.
const (
	DestEnv      DestKind = "env"
	DestHeader   DestKind = "header"
	DestForm     DestKind = "form"
	DestFile     DestKind = "file"
	DestProvider DestKind = "provider"
)
