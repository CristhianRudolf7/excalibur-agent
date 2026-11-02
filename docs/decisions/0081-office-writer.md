# Basic Office writing and frozen eval compatibility

The Office writer implements Sections 13.3, 13.6, 14.7 and 17 with pure Go
OOXML generation and byte-span XML edits. Unknown uncompressed parts and
unrelated XML bytes remain intact. Generated packages are re-opened and
validated before atomic publication. Edits require the original SHA-256 and
check it again immediately before replacement. A create without overwrite
publishes with an exclusive hard link so a concurrent creator cannot be erased.

The existing media registration seam now provides real deferred
`document_create` and `document_edit` tools to embedded and daemon engines.
`MediaDeps.Workdir` is an additive wiring field; `BuildEmbedded` forwards its
existing workdir. The tools are available in default/delegate; eval's table,
fixed prefix and profile remain unchanged. The all-tools name fingerprint
changes intentionally from
`b9bedd828535af82488a5a7034279a376a3165377cdac3c87adddf7d7b4408eb` to
`7b2da516f0404a74bc04532908a7594977ed4ebe90f69256790d5679cd8cdb89`.
Only that name pin changes in the daemon parity test.

The legacy eval skill fingerprint includes all built-in groups, although eval
enables only builtin and software-engineering. The three rewritten Office
skills belong to documents and cannot affect eval execution. To retain the
frozen skill and profile hashes, eval now retains their three original hash
contributions in its compatibility fingerprint. All other catalog entries
continue contributing their actual hashes. This is deliberately narrow: it
does not relax the table, change enabled groups, or silently repin the profile.
A future explicitly versioned eval profile can hash only its enabled groups.

`EXCALIBUR_OFFICE_WRITE` defaults on and disables invocation when off. It does
not remove discovery entries. Lower-level packages do not read environment
switches. No shared extraction, API, provider or transport contracts change.

Row deletion deliberately rejects complex sheet structures, including all
shared/array formulas and table-bearing sheets, rather than silently damaging
references it cannot safely update. There is no rendering, tracked-change
editing, chart/pivot creation or formula evaluation. See the creation and edit
package READMEs for the exact JSON contracts and remaining limitations.

Verification found that `EnabledSkillGroups` is descriptive metadata rather than
an enforced index boundary; existing eval tests require skills from other groups
(such as shell and build recipes). Applying a broad group filter would regress
that baseline. The additive `skills.WithExcludedNames` option and a small CLI
index factory therefore exclude only these three rewritten Office skills from
eval search, loading and suggestions. Every other entry retains its existing
visibility. `EXCALIBUR_OFFICE_EVAL_ISOLATION` defaults on and offers the legacy
unfiltered index for explicit ablation. Tests cover Office isolation, preservation
of the shell/build recipes and writer invocation denial. A general eval skill
policy remains a separate proposal, not an incidental Office change.

## Integration note

At integration the eval isolation was unified with the PDF skill snapshot:
eval reads byte-for-byte snapshots of the four document skills from
`internal/skills/frozen_eval`, so its catalog hash and profile hash keep their
recorded values without excluding skills or special-casing hashes. The
`EXCALIBUR_OFFICE_EVAL_ISOLATION` switch was removed.
