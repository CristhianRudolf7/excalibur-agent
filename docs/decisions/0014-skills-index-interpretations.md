# Skills index interpretations (Sections 12.1-12.5, 12.8)

The architecture fixes what the skill subsystem must do but leaves
four mechanical choices open. Each choice below takes the closest
feasible behavior and changes nothing observable beyond the gap.

## 1. Initial lifecycle state comes from frontmatter

Section 12.2 lists entry states (`active`, `draft`, `obsolete`,
`archived`) without saying where the initial value is stored.
`metadata.excalibur.state` is accepted as the initial state and
defaults to `active` when absent. Later transitions go through
`Index.SetState`. States live in the RAM index (12.2: the index is
in RAM); no persistence is claimed.

## 2. Staged package directories have no meaningful name

Section 12.8 stages a package in a temporary folder, whose name is
random. The directory/name match of 12.1 therefore cannot apply at
install time: the skill name always comes from the frontmatter
`name`, which must itself be valid. The match is enforced where it
is meaningful — when the skill lands in a real root and `Scan`
indexes it.

## 3. Unknown declared tools warn at install, reject at scan

Section 12.8 says install validates the format and "also checks"
the declared requires and tools. Format errors (12.1) reject the
package; unknown tools and missing requires are reported in the
user-facing summary instead of rejecting, because tools land from
other subsystems that may not be registered yet. When the index
scans with a tool cross-check enabled, such skills are excluded
with a visible diagnosis until their tools exist.

## 4. Tiny-window catalogs truncate instead of overflowing

Section 12.3 caps the L1 catalog at min(2% of the window, 4K
tokens). For windows so small that not even one header line fits,
the renderer truncates the first line to the budget instead of
emitting an over-budget block. Codex, cited in the same section,
likewise drops content when the budget is exceeded.

## 5. Suggestion vectors plug in later

Section 12.4 asks for BM25 plus vectors over name, description and
body headings. The lexical leg (BM25-style scoring with per-field
weights) is implemented; the vector leg is a nil-by-default
`VectorScorer` interface. The memory milestone (H11) plugs the
embedding-backed scorer in without changing the selection
algorithm. Until then suggestion and search are purely lexical,
which keeps them deterministic and testable.
