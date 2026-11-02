# 0088: Client skill catalog management

The client API adds `skills.groups`, `skills.list`, `skills.get`,
`skills.update`, `skills.set_state` and `skills.delete`. Existing search,
load, install and model tool definitions remain unchanged (Section 12).
The Go registry, JSON export, gateway dispatch and TypeScript wire types
include these additive methods.

The effective index resolves project, user, installed and builtin roots in
that order. Group counts include every effective entry, including disabled
entries. Listing supports group, case-insensitive name/description text and
source filters. Details return the complete source SKILL.md. All sources
are editable when a destination exists; builtins require a configured user
root. Only effective filesystem entries are deletable.

Updates run the same parser and validation callback as Scan, including name,
group, lifecycle and tool diagnostics. Invalid content returns invalid_params
with `data.diagnostics`. Unknown names return unknown_target. Changes use
synced temporary files and same-directory rename, after creating a timestamped
backup of the prior SKILL.md beside it. Installed updates refresh the existing
version/hash record. Builtin updates copy referenced assets into a user override
and never change the embedded filesystem. Deletes remove installed hash pins and move the whole skill directory
to a timestamped `.backups` directory in its owning root; hidden directories are
excluded from discovery. Deleting a user override reveals the builtin again.
Deleting an effective builtin returns invalid_params with the specified message.

Catalog edit/delete/state operations and rescans are serialized. Every content write/delete
rescans the running index. API enabled/disabled map to active/archived via
Index.SetState; draft and obsolete retain their lifecycle names. State overrides
survive rescans in the same process, and reset on restart, consistent with the
existing in-memory SetState contract. Archived entries cannot be loaded.

The production daemon configures user and installed roots at `skills` and
`installed-skills` inside its data directory and a project root at
`.agents/skills` in its startup working directory. Existing injected roots
continue to work. Eval still uses NewEvalBuiltinIndex and never reads these roots.

`EXCALIBUR_SKILLS_MANAGEMENT` defaults on; 0/false/off disable the six new
client methods. Existing methods and all frozen catalogs remain unchanged.
Backend verification is documented in `docs/development/int2-be-2.md`.
