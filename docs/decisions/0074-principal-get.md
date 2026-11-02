# 0074: Read the local principal through the client gateway

The web profile needs one real identity before further client data is connected.
Add the read-only `principal.get` client method with no required parameters and
`{id, name, timezone, language}` string fields. Valid parameters are ignored;
malformed parameters return `CodeInvalidParams`. Store failures return
`CodeInternal`. No existing API entry, event, schema or model tool changes.

The daemon seeds an empty `principals` table through its existing SQLite porter,
inside the command's short transaction. The conditional insert uses the stable
`local` key and `ON CONFLICT DO NOTHING`; simultaneous starts cannot duplicate
it. Existing rows, timestamps and foreign principal IDs are untouched. Reads
prefer `local`, otherwise the existing row in ID order. There is no configured
principal selector in the current daemon configuration.

Defaults are `EXCALIBUR_USER_NAME`, otherwise the OS account's GECOS full name,
otherwise its login, then USER, then LOGNAME, and finally "User". OS access stays behind additive platform functions, with
unsupported-operation stubs for non-Linux builds. Timezone uses a valid local
IANA identifier or UTC. Language uses LC_ALL, LC_MESSAGES then LANG, selecting
Spanish for an es locale and English otherwise. Host defaults never overwrite
an existing profile. Profile initialization is best effort: inspection and seed
write failures are logged as warnings and never prevent daemon startup. The
service remains attached and returns `CodeInternal` with "could not read local
profile" while the row is absent or unreadable; later reads can recover when a
row becomes available. A missing or unsupported OS account lookup uses the
fallback chain and does not prevent seeding or startup.

`EXCALIBUR_PRINCIPAL_ENABLED` defaults ON; `0` or `false` disables seeding and
makes the new method unavailable. It affects daemon client reads only. Existing
assistant, embedded and eval behavior is preserved. Profile editing, selecting
multiple accounts, authentication and connecting other screens are deferred.

The frontend uses a reusable typed JSON-RPC transport and a shared profile
query. Loading and disconnected states expose no fake identity. A profile
dialog shows the name; all remaining screens keep their mock data. Default
browser tests mock the RPC; a separate integration project builds and launches
a real daemon against disposable state.

Contract parity gains one client method. The tool prefix, catalog, SDK,
assistant and frozen eval surfaces and hashes remain unchanged. Existing parity
tests verify that invariant; no pins are updated.
