# 0087: Share persistent reminders between tools and clients

Bind the existing reminders model-tool provider to the SQLite agenda in the
daemon and non-eval embedded exec/delegate paths. Preserve the tool definition,
schema, fixed prefix, eval profile and in-memory implementation. Embedded
catalogs without an injected persistent service use the existing memory store.
Embedded exec persists reminders but does not start a second timer: the daemon
owns delivery after exec exits.

The additive client methods are reminders.list, reminders.create,
reminders.update and reminders.delete. They share ReminderService with the
model-tool provider. Tool `when` maps to RFC3339 due_at or snooze_until, and
complete maps to status done. Invalid dates are reported as input errors;
arbitrary natural-language dates cannot be scheduled by the durable agenda.
No tool schema changes are needed. Keyed tool creation derives an opaque ID
from the invocation key and atomically inserts without overwriting an existing
row. Retrying after a restart returns the original reminder and emits no
duplicate create event. A missing timezone resolves from the local
principal row, falling back to UTC only when no profile is available. Recurrence
accepts cron, daily, weekly and monthly, converting named intervals to cron in
the reminder's timezone. Created and last-fired timestamps persist in the
existing reminder envelope; legacy created timestamps come from the SQL row.

Client scheduled maps to pending/delivered, snoozed to snoozed, done to done,
and cancelled to canceled. Due reminders use LaunchReminder and the existing
master-notice path. Successful delivery records last_fired_at and emits
reminder.fired. Failed launches keep the reminder due. Safe mode holds delivery.
CRUD emits reminder.updated after persistence; deletion carries the last
reminder snapshot marked cancelled. Snooze requires a future snooze_until.
Updates and timer deliveries serialize on the agenda's reminder lock to avoid
lost edits. The agenda timer ignores held reminders while safe mode is active.

EXCALIBUR_PERSISTENT_REMINDERS is default ON for embedded default/delegate;
0 or false selects the memory implementation for rollback. Eval always uses
memory and never opens the reminder database. The daemon always uses its agenda.
No paid services or live tests are involved.

Tests exercise the tool through its existing stub, unchanged definitions,
SQLite close/reopen, principal timezone, real due firing, safe mode, duplicate
suppression, CRUD validation, edits, snooze, cancellation, deletion, events,
client RPC dispatch and embedded profile isolation.
