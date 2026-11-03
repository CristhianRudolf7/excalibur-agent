# Principal time zone preferences

Add `principal.update` and `timezone_source` to the local principal contract.
Migration 12 adds the source with `default` for existing and new rows. New daemon
profiles use UTC regardless of host time zone. Memory owns profile reads and
validated updates; the daemon retains its existing best-effort seeding and error
boundary. UTC is an accepted IANA-compatible name; the host-dependent `Local`
pseudo-zone and empty names are rejected.

Updates accept `timezone` and `timezone_source` (`default`, `auto`, `manual`). An
automatic startup report is atomically ignored while the stored source is manual.
The optional `enable_auto: true` on an auto update means the user explicitly
turned detection on. This distinguishes that action from a late startup report,
allows one atomic transition, and avoids briefly resetting a manual preference.
Both update and get return the effective persisted profile.
