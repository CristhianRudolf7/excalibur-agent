# Path identity and macOS temporary outputs

Sections 3.4 and 7.3 require the same task paths to retain their meaning
across the platform and completion checks. On macOS, `/var` and `/tmp`
have symlink-resolved spellings under `/private`. The additive
`platform.CanonicalPath` helper compares filesystem identities, resolving
existing ancestors even before an output is created. It preserves cleaned
paths on resolution errors and does not guess case equivalence. Platform
interfaces and tool definitions remain unchanged.

The strict artifact extractor accepts macOS temporary directories without
admitting all of `/var`. Each requirement keeps `Source: trimLen(sent, 120)`
for every extraction kind, preserving the original model-facing text and
serialized records byte for byte. An unexported `sentence` field retains
the complete sentence; `IntentSource()` returns it when present and falls
back to `Source` for manually constructed or decoded requirements. The
private field is omitted from JSON and YAML.

Only the hard-artifact classifier (create/install/in-place intent,
negation and example exclusion) and the declared-service classifier use
`IntentSource()`. Probe sync, delayed trial sessions and deliverable checks
share the hard-artifact classifier. Their long temporary paths must not
hide trailing instructions or negations. Display and rendering code keeps
using the original bounded `Source`. Regression tests compare exact
pre-change JSON and YAML bytes, exercise every extraction form, and verify
the fallback after decoding.

Portable regression tests use symlinked temporary roots; Darwin tests
also cover the native temporary directory spellings and rejected system
paths. Linux tests run locally; Darwin tests require a macOS runner.

Attempt output checks map aliased paths into their copy. Probe observations
compare resolved parents while skipping symlink leaves, retaining the
requested deliverable spelling in advice. Harness boundary checks compare
both path spellings, including for literal shell operands and future paths.
Blocked claims also extract paths relative to the run workdir.

Repository briefing lines reserve all three bytes of the UTF-8 ellipsis and
clip at a rune boundary. The 160-byte limit applies to unreadable-directory
messages as well as ordinary briefs, including long temporary paths.
