# 0026 — Memory schema seam, local id prefixes, provider embeddings now

Date: 2026-10-31. Area: memory (H11: Section 10).

## What

Four places where the implementation keeps the architecture
behavior while avoiding parallel-edit collisions:

1. **Auxiliary tables via `memory.EnsureSchema`, not migration
   v5.** `internal/store/migrations` is owned in parallel by the
   storage milestone, and its registry test pins `Latest() == 4`.
   Core memory tables already exist (v3/v4). Memory-owned
   additions (`doc_fragment_history`, `doc_edit_conflicts`,
   `review_state`, `review_reports`, `pending_vectors`,
   `citation_runs`, plus the `doc_fragments.provenance`,
   `doc_fragments.proposal`, `documents.title`, `memories.short`
   and `memories.conversation` columns) are created idempotently
   by `EnsureSchema` inside porter transactions. A later
   consolidation into a numbered migration stays possible; nothing
   about the behavior depends on where the DDL lives.
2. **Memory-local id prefixes over `types.NewULID`.** Subordinate
   rows use `ent_`, `edg_`, `rlt_`, `idd_`, `frg_`, `doc_`,
   `chn_`, `sev_`, `rep_` plus the shared `mem_` for memories.
   `internal/types` stays untouched, so the shared contract
   (prefix validation, typed ids) cannot break for other
   milestones.
3. **Provider embeddings now, not local-only.** Section 10.8
   sequences remote providers after the first milestone, but the
   H11 task requires provider embeddings with
   `text-embedding-3-small` as the configured default, and no
   local model exists in this environment. `HTTPEmbedder` (any
   OpenAI-compatible `/embeddings` endpoint, bounded retries,
   single fallback-key use on quota/rate-limit, no key logging)
   is the default path; the local-sidecar shape remains the
   `Embedder` interface. Vector storage is float32 shadow plus
   vec0 — int8 quantization waits for a shared-schema change.
4. **English kind/status vocabulary.** The document's Spanish
   values map one-to-one: hecho→fact, preferencia→preference,
   evento→event, compromiso→commitment, creencia→belief,
   relación→relation, procedimiento→procedure;
   candidato→candidate, vigente→active, caducado→expired,
   reemplazado→superseded, en_disputa→disputed, retirado→retired;
   usuario→user, agente→agent, fuente_externa→external. The repo is
   English-only per AGENTS.md.

## Alternatives considered

Editing shared files (a v5 migration, new `internal/types`
prefixes, a `config.MemoryConfig.EmbeddingModel` field) was
rejected: additive in theory, but three milestones edit those
files concurrently and the merge risk outweighs the elegance.
The service owns a `Settings` struct mirroring the Appendix B
values instead.
