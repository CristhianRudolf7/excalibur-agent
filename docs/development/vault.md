# Secrets vault (Section 20.1)

The encrypted secrets vault lives in
`internal/vault` (plus `internal/vault/fake` for clients' tests). No
other package is modified: the tools executor, provider gateway, CLI
and gateway call into this package's core.

## Contract

- **Storage**: separate `vault.db` behind its own porter
  (`store.Open` with `SynchronousFULL`), file mode `0600`. Tables
  `secrets`, `vault_keys`, `secret_uses`, `drafts` (see Appendix A row
  shapes).
- **Encryption**: per-entry XChaCha20-Poly1305 with random nonce per
  write; AAD binds `(id, name, type, entry_version)`, so a ciphertext
  copied to another row does not decrypt. Random 256-bit DEK, wrapped
  by a master key (KEK): random KEK in the platform `Keyring`
  (Secret Service on Linux) when reachable, else Argon2id from a
  passphrase (Appendix B: 64 MiB, 3 iterations, 4 threads, 16-byte
  salt). DEK is `mlock`ed best-effort, handled as `[]byte`, zeroed
  after use.
- **Lifecycle**: starts locked (except unattended unlock via a `0600`
  passphrase file declared in config). Auto-lock: never with Secret
  Service, 12 h idle with passphrase (configurable). Locked tools get
  an unlock question (`waiting-on-user`), never a failure.
- **Entry**: `Set` creates/replaces in one porter transaction (crash
  leaves the previous version). `Rotate`/`RotateWithPassphrase`
  generate a fresh DEK and re-seal every entry in one transaction.
  Scope semantics: empty scope list means `general`; explicit scopes
  are exact (no implicit general). Delegate tasks (`Delegated: true`)
  resolve only entries scoped to their project. The `eval` profile
  never opens the vault (`ErrEvalBlocked` at `Open` and `Resolve`).
- **Entry in**: never via chat. CLI reads the value from the terminal
  (no echo) or `--stdin`, never as an argument. Chat-pasted secrets
  are detected by `DetectChatSecrets` BEFORE persisting the message;
  the stored text keeps markers, the value moves to an encrypted draft
  (`SaveDraft`/`ConfirmDraft`/`DiscardDraft`; held in memory while
  locked, encrypted on unlock).
- **Use**: the model only sees `secret://name` references and
  metadata. `Resolve` checks expiry, scopes, `allowed_tools` and form
  `domains`, records the use (never the value) in `secret_uses`, and
  returns `[]byte` the caller zeroes. Injection helpers: `InjectEnv`,
  `InjectHeader`/`SetAuthHeader`, `CheckFormPolicy`, `InjectFile`
  (`0600` temp file in `$XDG_RUNTIME_DIR`, removed after use).
  `SecretResolverFunc` adapts the vault to the provider gateway's
  `SecretResolver` hook (vault wins over env/config); structurally
  identical, no import coupling.
- **Redaction**: `Redactor` (Aho-Corasick over live values plus
  base64/URL variants, rebuilt on unlock and every value change) plus
  known-format patterns. Markers `‹secret:name›` / `‹secret:pattern›`.
  Values shorter than 8 bytes skip exact matching (false-positive
  guard) but keep pattern cover.
- **Agent tool**: `Dispatch` ops `list`, `describe`, `generate`,
  `request`, `request_change`, `request_delete`, `usage`,
  `confirm_draft`, `discard_draft`, `list_drafts`, `redact`. Never
  returns values; value actions return `PendingRequest` for the user's
  secure form/confirmation. `generate` stores a crypto-random value;
  only metadata returns.
- **API core**: `Create`/`Update` (values only in these dedicated
  calls), `ListMetadata`/`DescribeEntry` (never values),
  `LockVault`/`UnlockVault`, `DeleteEntry` (requires confirmed flag),
  `Reveal` (explicit user terminal only; the agent path must never
  call it), `EditPolicy`, `RotateKeys`, `ExportVault`/`ImportVault`
  (Argon2id + XChaCha20-Poly1305 envelope, no cleartext),
  `UsageOf`, `GenerateEntry`.
- **Errors**: `ErrLocked` (with unlock question), `ErrNotFound`,
  `ErrDenied` (policy/expiry, with a clear reason naming the secret,
  tool or domain), `ErrNoKey`, `ErrEvalBlocked`.

## Backend tests (Section 20.2)

Every row is covered in `internal/vault`:

| Section 20.2 row | Test |
|---|---|
| All types, no cleartext in data dir / vault.db / blobs / journal / telemetry / ATIF | `TestVault_AllTypes_NoCleartext` (file bytes + full table dump vs raw, base64 and URL variants; `0600` check) |
| Copied ciphertext fails on AAD | `TestCrypto_AADBinding` |
| Env output in raw, base64, URL redacts to `‹secret:name›` | `TestRedact_EnvBase64URL` (+ pattern/rotate/locked variants) |
| Pasted API key + password: message clean, keep question, discard removes | `TestDetect_ChatPaste`, `TestDetect_LockedDraft`, `TestDetect_NoSecrets` |
| `secret://banco-web` denied for exec and wrong domain, fills on right domain | `TestResolve_Policy` (+ scopes/expiry) |
| Locked vault -> unlock question, continues after unlock | `TestVault_LockedNeedsUnlock` |
| kill -9 during set/rotate: old or new version, never corrupt | `TestRotate_CrashSafe` (close/reopen + `VerifyAll`), `TestRotate_PassphraseVault` |
| export/import roundtrip, file holds no cleartext | `TestExport_Import`, `TestExport_FileRoundtrip`, `TestExport_LockedFails` |
| eval blocked; delegate scoped to its project | `TestVault_EvalAndDelegate`, `TestResolve_Scopes` |

Plus: `TestSecretsTool_NeverValues` (all tool ops leak-free),
`TestAPI_Core` (full vault.* core), `TestInject_Paths` (env/header/
file/form/scrub), `TestGenerate_Basics`, crypto wrap/KDF tests,
passphrase-file `0600` enforcement, Secret Service preference and
restart, auto-lock (`TestVault_AutoLock`), `fake` package contract
test.
