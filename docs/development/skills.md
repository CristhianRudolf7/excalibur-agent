# H8/H12 — skills: spec, index, catalog, loading, installs

Contract and backend tests of the skills area: Agent Skills spec
validation (12.1), the RAM index with file watching (12.2), groups
and the grouped L1 catalog (12.3), suggestion and on-demand
loading (12.4), the full built-in catalog (12.5) and external
installs (12.8). Sections 12.6-12.7 (learning, pairwise
preference) are deferred with autopoiesis and are not implemented.

## Contract

Package `internal/skills` (with `internal/skills/fake` for other
milestones' tests) implements Sections 12.1-12.5 and 12.8:

- **Format (12.1)**: `ParseSkillFile` reads `SKILL.md` (YAML fence
  with `name` and `description`, Markdown body). `Validate`
  enforces: name matches the directory, 1-64 chars, lowercase /
  digits / hyphens without leading, trailing or repeated hyphens;
  description 1-1024 chars saying what the skill does and when to
  use it; `compatibility` at most 500 chars; exactly one known
  group; known state; known tools when the cross-check is enabled.
  Invalid skills are excluded and every finding stays visible via
  `Index.Diagnostics`. `metadata.excalibur` carries `group`,
  `version`, `platforms`, `requires` (bin/env), `tools`, `display`
  (ES/EN UI names) and the initial `state`; top-level
  `allowed-tools` is parsed and exposed as an additional toolset
  restriction for the engine to honor.
- **Index (12.2)**: roots in precedence order — project skills
  (`.agents/skills`), user, installed packages, built-in system
  (embedded `builtin/<group>/<skill>/SKILL.md`). Entries hold
  name, description, group, path, content hash, version, state
  (`active`, `draft`, `obsolete`, `archived`), platforms,
  statistics and result counters. The highest-precedence duplicate
  wins and the shadowing is recorded in `Index.Conflicts`.
  `Watcher` combines an fsnotify watch (root plus skill
  directories, new directories picked up on creation) with a
  30-minute fallback rescan; a changed hash invalidates the
  entry, revalidates it and refreshes the lexical state.
  `Snapshot`/`Delta` freeze a session's catalog so reindexes never
  break its input-cache prefix, and report updated/removed skills
  the session actually uses.
- **Groups and L1 catalog (12.3)**: 32 groups with EN/ES names
  and descriptions (`Groups`). `RenderCatalog` lists pertinent
  groups fully (name plus description capped at 120 chars, or
  names only with `CatalogConfig.Compact`) and all
  other non-empty groups as headers pointing at `search_skills`,
  within min(2% of the window, 4K tokens), degrading later groups
  to headers and truncating rather than overflowing on tiny
  windows. `DefaultPertinentForRole` is the deterministic leg
  (programming lists the engineering groups after the original
  five, in budget priority order; conversation unchanged); doubt
  goes to the decider (H12).
- **Selection and loading (12.4)**: `Suggest` scores the task
  description with BM25-style weights over name, description and
  body headings plus the optional vector leg (`VectorScorer`, nil
  by default; H11 plugs embeddings in), returning at most 3
  suggestions with a one-line reason for L3. Query terms pass
  through a static synonym map first (the cheap semantic leg for
  terse model phrasing). `SuggestWithEnv` enriches the query with
  task-start environment evidence (`EnvSignals`: top-level project
  files plus one LookPath probe per known diagnostic binary, via
  `CollectEnvSignals`) without changing the ranking algorithm:
  instruction words vote full weight (literals 1, aliases 0.5),
  project files half (aliases a quarter), and binaries only
  corroborate instruction/file terms (uncorroborated binaries vote
  a residual). Stopwords, stray single letters (except `c`/`r`),
  filename artifacts (half unless the file is really present) and
  light plural stemming keep terse model phrasing on vocabulary.
  `Search` implements `search_skills(query?, group?)` returning
  name, description, group and path; an omitted query lists the
  group. `Load` implements `load_skill(name)` returning
  the body for the end of history (L0-L2 hashes untouched),
  with declared deferred tools (resolved with the skill per
  13.3), requires, allowed-tools, and the available sections and
  references; `LoadSection` serves one `##` section and `LoadRef`
  serves one `references/` file, with errors that list what
  exists. Mandatory instructions never
  travel as suggestions; they stay in the card's fixed
  instructions. `RecordOutcome` tracks per-model results with and
  without each skill and disables a group's suggestions for a
  model on negative evidence (minimum sample, gap threshold).
- **Ablation switch**: `LibraryMode` (`off`/`on`/`auto`,
  `EXCALIBUR_SKILLS`, default `auto`) gates the catalog, search,
  load and auto-suggest so trials measure off vs on vs
  on+auto-suggest; `EXCALIBUR_SKILLS_COMPACT=1` selects the
  names-only catalog. `exec`/`eval` read both from the
  environment: `on`/`auto` render the programming L1 catalog into
  the snapshot, wire the real skill tools and write `skills.json`
  (mode, compact flag, catalog hash, suggestions) plus a
  `skills_wired` event; `auto` adds task-start suggestions from
  the request and collected environment signals.
- **Built-in catalog (12.5)**: 256 English `SKILL.md` files under
  `internal/skills/builtin/<group>/<name>/`, one focused skill
  per catalog row, each with when-to-use, procedure, pitfalls and
  verification, frontmatter group/version/platforms/tools and
  ES/EN display strings. 7 integrated groups ship with the harness;
  25 curated groups passed format validation here and await
  Section 23 evaluation before `active` means more than
  well-formed. The ten engineering groups (`terminal`,
  `packaging`, `build`, `languages`, `networking`, `containers`,
  `databases`, `data`, `ml`, `media`) form the terminal/software
  reference library with concrete commands, pitfalls and
  verification steps; depth material ships as `references/` files
  served on demand by `load_skill(ref=...)` without entering the
  prompt.
- **External installs (12.8)**: `Install` stages a package
  (`DirSource`, `TarGzSource` — confined extraction, no
  symlinks followed) without executing anything, validates the
  format with a visible diagnosis, checks requires (binaries on
  PATH, env) and tools into a user-facing `InstallSummary`,
  activates into the installed root atomically, and pins
  hash+version in `.excalibur-installs.json`.
  `CheckInstalled` detects drift, revalidates changed content and
  records it as a new version, or reports rejection keeping the
  pin.
- **Tools**: `SearchSkillsTool`/`LoadSkillTool` implement the
  Section 13.2 `search_skills`/`load_skill` contract (fast,
  read-only, idempotent) through the shared `tools.Registry`;
  every tool result is append-only history, so the stable prefix
  is unaffected.
- **Fake**: `fake.Catalog`/`FakeCatalog` give other milestones a
  test double for search/suggest/load/render.

Interpretations recorded in
`docs/decisions/0020-skills-index-interpretations.md`: initial
state from frontmatter, staged directory names are meaningless at
install, unknown tools warn instead of rejecting at install,
first-line truncation on tiny windows, lexical-only suggestion
until H11 vectors land. Quality-round interpretations in
`docs/decisions/0039-skills-quality-library.md`: ten new groups
instead of one mega-group, focused `load_skill` slices,
environment-aware auto-suggest without embeddings, the
`EXCALIBUR_SKILLS` ablation switch, and the minimal exec seam.

## Backend tests

- `spec`/`validate`: bad names (case, edges, repeats), overlong
  description, compatibility over 500 chars, directory/name
  mismatch, unknown group/state/tool, missing file, unclosed
  fence — each excluded with its diagnosis code; unknown section
  and reference names rejected with available-name listings.
- `index`: project-over-user precedence with recorded conflict;
  SKILL.md modification changes the hash, refreshes the index and
  shows as an `updated` session delta; install drift detected;
  `references/` files indexed per skill from embed and disk.
- `render`/`catalog`: 200K window respects min(2%, 4K) with 256
  skills and 32 groups;
  non-pertinent groups render headers only with `search_skills`
  pointers and no skill lines; fully listed descriptions fit 120
  chars; tiny windows truncate within budget; compact catalogs
  carry names only and stay smaller than the full form.
- `suggest`/`search`/`load`: at most 3 suggestions with reasons;
  load appends to L4 while L0-L2 hashes stay identical
  (checked through `ctxcompiler.Compile`); declared tools resolve
  with the skill; empty-query group listing; archived skills
  excluded from search/suggest/load; per-model negative evidence
  disables one group for one model only; env signals surface the
  right skill for Rust/Python/Go/Node/CMake/TLS checkouts with no
  instruction text; synonyms map terse phrasing (`py packaging`)
  to catalog vocabulary; section/ref loads serve focused slices
  with sibling pointers; the mode switch gates catalog, search,
  load and suggestions off/on/auto.
- `install`: valid package activates and indexes; bad-name
  package rejected with diagnosis; same content reinstalls as
  `same-hash`; later content change detected, revalidated and
  versioned; missing binaries/env and unknown tools reported in
  the summary.
- `builtin`: all 256 catalog skills pass the spec with unique
  names, one known group each, known tools only, and the exact
  per-group counts of Section 12.5; design/diagram review sample
  pinned.
- `skilltools`: definitions validate and register through
  `tools.MapRegistry` as fast tools; invoke paths for search
  (hit fields), group listing, load (body plus deferred tools),
  focused section/ref loads with sibling pointers, unknown-skill
  input errors, and query-less search listing the group.
- `watch`: an SKILL.md change is picked up without a manual
  rescan (fsnotify path; 30-minute ticker is the fallback).

## Deferred (not in H8/H12)

No automatic learning or preference judgments (12.6, 12.7, 24);
no vector scorer (interface ready, H11 plugs it in); no store
persistence for install records beyond the installed-root sidecar
file; curated groups await Section 23 evaluation; visual review
of design/diagram artifacts stays manual per run.
