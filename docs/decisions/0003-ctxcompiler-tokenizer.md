# 0003 — Token counting without a bundled BPE tokenizer

Date: 2026-10-31. Status: accepted.

## What

Section 11.7 says to use the provider tokenizer where one is
available locally (OpenAI family), naming tiktoken-go. The package
evaluated `github.com/pkoukk/tiktoken-go v0.1.8` (MIT) and does NOT
adopt it: at runtime it downloads the BPE ranks over HTTP on first
use (`load.go`: `http.Get` on `openaipublic.blob.core.windows.net`,
cached under `TIKTOKEN_CACHE_DIR` or the OS temp dir). Consequences:

- Unit tests would depend on network access, violating the hermetic
  backend-test rule (simulated servers only, never paid APIs, and no
  network fetches in `go test ./...`).
- First production use on a fresh machine would fail offline and
  phone home to a third party, which the secrets-adjacent daemon
  must not do silently.

## Instead

- `Counter` is the counting seam; `Estimator` is the character-based
  counter calibrated with the usage every response returns
  (moving-average factor per model, 10% safety margin on the 11.3
  and 11.4 thresholds).
- `Tokenizer` is the plug-in point for an exact provider tokenizer
  that is available locally *without* network; `CounterForModel`
  prefers it when injected and falls back to the estimator.
- `go.mod` stays stdlib-only; no license entry is added to
  `docs/decisions/dependencies.md` because no dependency is adopted.

## Revisit when

A tokenizer release bundles the BPE data offline (embedded ranks or
a vendored file with a pinned hash) and resolves encodings without
network. Then add it behind `CounterForModel` with a hermetic test
and record the version and license in `dependencies.md`.
