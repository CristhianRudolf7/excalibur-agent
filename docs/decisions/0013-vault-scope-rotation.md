# 0013: Vault scope and rotation decisions (Section 20.1)

## Scope default semantics

The document says entries carry `scopes` (general or projects where
the secret may be used) without pinning the empty-list meaning.
Decision: an entry with no explicit scopes is `general`-only; explicit
scopes are exact, with NO implicit general access. Rationale: the
alternative (every entry implicitly general) makes project scoping
decorative and breaks the 20.2 delegate test (a delegate task must only
resolve entries scoped to its project). Recorded here per the
discrepancy rule; behavior covered by `TestResolve_Scopes` and
`TestVault_EvalAndDelegate`.

## Passphrase rotation needs the passphrase

The KEK of a passphrase vault is derived, never stored, so `Rotate`
(keyring path) cannot re-wrap it and fails with `ErrNoKey` directing
to `RotateWithPassphrase`, which re-derives the KEK from the supplied
passphrase. Same single-transaction atomicity in both paths; mixed
DEK versions still decrypt via their own `vault_keys` row.

## Drafts while locked are in-memory

`SaveDraft` with no installed DEK keeps the candidate in a
memory-only pending set (still redacted from the message) and moves it
to the encrypted `drafts` table on unlock. Alternative (persisting
unencrypted) would violate the never-in-clear rule.

## Short values skip exact redaction

Values shorter than 8 bytes are excluded from the Aho-Corasick
automaton to avoid false positives (e.g. redacting "password" or
"1234567" everywhere); pattern redaction still applies.

## SecretResolver hook without import coupling

`Vault.SecretResolverFunc` returns a `func(ctx, name) (string, error)`
structurally identical to the provider registry's `SecretResolver`
type, adapted at the call site instead of importing the registry
package. No behavior change, no cycle risk.
