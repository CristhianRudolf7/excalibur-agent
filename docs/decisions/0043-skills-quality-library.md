# Skills library quality round: reference library, discovery, ablation (Sections 12.3-12.5)

The agent-quality workstream needs a much stronger harness for small
models under hard constraints: `internal/providers`, the model
gateway wire protocol, `internal/api`, `packages/contracts`,
`integrations/`, the MCP/ACP servers and the bench Harbor invocation
are all frozen. Everything below stays inside `internal/skills`
(content and selection) plus one minimal additive seam in
`internal/cli/exec_engine.go` so the mechanism actually runs in
`exec`/`eval` and lands in the records.

## 1. Ten new catalog groups instead of stuffing software-engineering

The task asks for a real reference library (shell, packaging,
build, languages, git, networking, containers, databases, data
processing, scientific computing, ML, media, security, LaTeX,
profiling). Adding ~60 skills to `software-engineering` would push
the programming L1 catalog over the min(2%, 4K) budget and degrade
`debugging` (which carries `root-cause-first`) to a header. Ten new
curated groups (`terminal`, `packaging`, `build`, `languages`,
`networking`, `containers`, `databases`, `data`, `ml`, `media`)
keep groups small and searchable and append after the existing
programming pertinent list, so the existing budget behavior and
tests are preserved. The builtin census moves 193 -> 256.
Alternative: one giant group per language family; rejected because
group-filtered `search_skills` and catalog headers are the small
model's navigation and both degrade with mega-groups.

## 2. load_skill section/ref selectors, optional search query

`load_skill` gains optional `section` (one `##` heading) and `ref`
(one `references/` file) parameters; `search_skills` keeps working
when `query` is omitted (lists the group). Both are additive:
existing calls carry neither field and behave exactly as before,
results still land in L4 history so the L0-L2 cache prefix never
moves, and the Section 13.2 tool contract (fast, read-only,
idempotent) is unchanged. References ship inside the embedded
`builtin` tree and are served from disk for filesystem roots.
Alternative: one flat bigger SKILL.md per skill; rejected because
focused slices cost a small model far fewer tokens per lookup.

## 3. Environment-aware auto-suggest without embeddings

`SuggestWithEnv` enriches the query with cheap environment signals
(project top-level file names, one LookPath probe per known binary)
mapped to catalog vocabulary, plus a static synonym map (the cheap
semantic leg: `py`->python, `tls`->ssl/certificate, `linker`->link
...). The BM25-style ranking and the optional `VectorScorer` leg
are untouched, so H11 embeddings plug in later without changing
selection. Vote weights keep the instruction dominant: literals 1,
their aliases 0.5, project files 0.5 (their aliases 0.25);
binaries only corroborate: a binary-derived term that also appears
in the instruction or the files votes the environment weight, while
an uncorroborated binary (docker installed, nothing docker about
the task) votes a residual quarter instead of injecting noise;
filename artifacts named in the instruction (`notes.txt`) vote
half unless the file also exists in the project (then the
environment weight reinforces them); base-system binaries
(`git`, `gcc`, `python3`, `curl`, ...) are not probed at all
because they vote identically on every task and drown the
diagnostic tools — language runtimes still resolve through
project files and their package managers; English function words
and
stray single letters (except `c`/`r`, which name languages) are
dropped; tokens stem light plurals (`packages`->`package`); the
`.git` directory maps to git vocabulary. The tokenizer now keeps
`+` so `c++` survives as one term (previously it split to `c`;
the `c++`->cpp alias covers ranking either way). Measured on
realistic task phrasing: `make` tasks surface `make-builds`
first, CSV/awk tasks surface `csv-json-cleaning` and
`awk-pattern-recipes`, git tasks surface
`version-control-hygiene` and `git-history-surgery` in the
top 3. Alternative: directory-tree walks and content sniffing;
rejected as too expensive for task start.

## 4. Ablation switch EXCALIBUR_SKILLS=off/on/auto (default auto)

Every new mechanism reads one switch so trials measure off vs on
vs on+auto-suggest: `off` wires no index (historical stubs, empty
catalog, no suggestions), `on` wires catalog+search+load without
injected suggestions, `auto` adds task-start auto-suggest.
`EXCALIBUR_SKILLS_COMPACT=1` selects names-only catalog lines.
All switches live in `internal/skills` (`LibraryMode`,
`CatalogConfig.Compact`); `exec`/`eval` read them from the
environment, so no config-file or CLI-flag surface changes.
Default is auto/compact-off: the library helps most when present
and proactively suggested, in the documented full grouped form.

## 5. Minimal exec seam: catalog, suggestions and skills.json

`runEnginePath` builds the builtin index, renders the programming
L1 catalog into `Snapshot.SkillCatalog`, injects top suggestions
into `Snapshot.Task.SuggestedSkills`, passes the index to
`BuildEmbedded` (which wires the real `search_skills`/`load_skill`
providers; `off` keeps the historical stubs), and writes
`skills.json` (mode, compact, catalog hash, suggestions) plus a
`skills_wired` event. The eval profile table, tool surface and
bench invocation are untouched; the dynamic catalog hash already
records content drift. Alternative: leaving exec unwired and proving
the mechanism by unit tests only; rejected because the workstream
requires end-to-end evidence in real `exec` records.
