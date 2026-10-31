# 0034 — W5 closure: eval surface enforcement, real catalog hash, status docs

- Status: accepted.
- Date: 2026-10-31.
- Scope: Appendix E closure pass over H1–H13 (no contract break; additive only).

## What the audit found

Going through Appendix E milestone by milestone against the
verification tables (§3.7–§26.13), Appendix C (coverage) and Appendix D
(triggers), the implementation covers every row with backend tests
(~600 test functions, all green, plus the §26.1–§26.11 walkthroughs as
`tests/walkthrough_*_test.go`). Three items were not faithful to the
document and were fixed here:

1. **Eval profile hash was a literal placeholder** (`SkillsCatalogHash:
   "frozen-placeholder"` in `internal/eval/profile.go`). Section 23.2
   and Appendix F require recorded, comparable hashes; a placeholder
   never moves when the skill catalog changes, so two different
   benchmark arms could share a profile hash. Fixed:
   `EvalSkillsCatalogHash()` fingerprints the embedded builtin skill
   index (`skills.NewBuiltinIndex().Scan()` + `CatalogHash()`), which
   is exactly what an eval trial exposes (eval aborts on non-empty
   data dirs, so no user/project/installed roots exist). Pinned by
   `TestEvalSkillsHashTracksCatalog` and the exact-surface test
   `TestEvalSurfaceMatchesProfileTable`.
2. **Eval tool-surface gating existed but was never enforced.**
   `EvalToolAllowed`/`EffectiveTools` (Section 23.2 table) were only
   consulted by tests; the embedded engine exposed the full builtin
   core prefix and every deferred tool through `search_tools` /
   `call_tool`, and project-management `task_db` ops were never
   rejected. Fixed in `internal/cli/exec_engine.go`:
   `profileGatedRunner` rejects out-of-surface calls (including
   per-op `task_db` gating) as tool errors the model can recover
   from, the eval model prefix is filtered to the table, and
   `pruneDeferredToProfile` unmarks non-table deferred tools so
   `search_tools`/`call_tool` cannot reach them either. Pinned by
   `TestExecEngineEvalSurfaceClamp`, `TestProfileGatedRunnerUnits`
   and `TestPruneDeferredToProfile`.
3. **Stale wave-0 comment** on `platform.ErrNotImplemented` claimed the
   Linux backends were still pending H2. Corrected: Linux is
   implemented; the sentinel is retained for compatibility only.

## Alternatives considered

- For (2), failing the whole run on the first out-of-surface call
  instead of surfacing a tool error. Rejected: the standard harness
  behavior for unknown tools is a recoverable tool error, and eval
  integrity only needs the call to have no effect, which the gate
  guarantees (rejection happens before execution, and the ATIF
  records it).
- For (1), hashing only the enabled skill groups instead of the whole
  embedded catalog. Rejected: the exec path exposes the whole
  embedded index, so the hash must cover the whole exposed surface
  to detect any drift.

## Deferred (documented, out of scope for this version)

Per §0 and Appendix E, unchanged: image/video/voice/GPU models
(simulated behind their contract seams, e.g. `MemoryVisionBackend`),
autopoiesis/skill learning (§12.6, §12.7, §24), security beyond the
vault (§20), mobile, multi-user. Live paid-provider checks stay
behind the `live` tag and were not run.
