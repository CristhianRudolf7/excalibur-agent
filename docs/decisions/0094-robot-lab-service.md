# 0094: Robot Lab runtime service and live frame delivery

Robot Lab adds twenty client methods under `robotlab.*`, typed parameters in
`internal/api/robotlab.go`, and JSON schemas in `packages/contracts/robotlab.json`.
The existing registered-method gateway passthrough routes them to the daemon's
`robotlab.Service`. Shared provider, model, MCP/ACP and benchmark contracts do
not change. The shared `robotlab.go` and Python scenario seams are unchanged.
This implements the service boundary for Sections 3.2, 3.5, 13.4 and 14.8.

The daemon composes one service using its data root and event bus. It owns a
lazy `ProcessSidecar` and an explicit `Installer`. Neither status queries nor
startup install dependencies or launch Python. `EXCALIBUR_ROBOTLAB` defaults
on; off rejects operations with `CodeRobotLabUnavailable` (-32020). Status
remains readable so the UI can show `enabled: false`. Non-Linux status reports
`errors.ErrUnsupported`; no child process is started there.

The embedded root pattern excludes the top-level tests directory. The `all:`
modifier preserves Python package initializers, which Go otherwise excludes
because their names start with underscores. Extraction filters test/cache
directories, bytecode and hidden directories. Extraction hashes file names and contents, writes a
private temporary directory, then atomically renames it into
`<data>/robotlab/src/<sha256>`. An existing extraction is reused. Old completed
hash directories are removed after successful extraction. The embedded root
pattern includes `robotlab/`, `pyproject.toml` and `requirements.lock` as the
Python-owned files are integrated.

The interpreter is `EXCALIBUR_ROBOTLAB_PYTHON` when set, otherwise
`<data>/robotlab/venv/bin/python`. The command runs from the extracted source
with unbuffered Python, OSMesa, two OMP/MKL threads and bytecode writes disabled.
The platform priority interface lowers runtime and installer child priority
(cgroup policy or a nice increment of 15). The process-group interface
detaches the runtime and owns TERM/KILL
shutdown. Calls wait for the protocol `ready` event, correlate replies by
numeric ID and honor caller deadlines with a sixty-second ceiling. A runtime
has thirty seconds to become ready. Cancellation removes the waiter; it does
not assert that an already accepted sidecar mutation was rolled back.

Crashes fail outstanding calls; completed requests are never replayed.
Missing prerequisites before the first successful launch do not consume the
crash budget. Failed restart spawns do consume it and are retried with backoff.
The sidecar restarts with exponential backoff, allowing at most three restarts
in five minutes. Calls during backoff return unavailable. Exhaustion remains
visible in status. Stderr is retained as an 8 KiB tail and included in error
status. Daemon shutdown and parent-context cancellation stop the process
group and subscription workers. Long-lived goroutines use `safego.Go`.

Each sidecar subscriber has a 64-event memory mailbox. The oldest queued
frame is removed under pressure. If reliable events alone fill the mailbox,
overflow goes to a private temporary spool until the callback catches up;
subsequent frames are discarded while reliable overflow is outstanding.
Spooling reconciles bounded memory, independent callbacks and reliable event
ordering. Only non-frame events may enter this spool. It is removed on
unsubscribe; filesystem failures are reported through logging. Normal metrics,
experiment, log, install, vision and focus events use the existing replay bus.
The sidecar's teleop event is also exposed additively as `robotlab.teleop`.

Frames use a new additive `telemetry.Bus.PublishEphemeral` /
`SubscribeEphemeral` path. This path has no replay entry, sequence allocation,
ordinary subscriber queue or sink write. The gateway requires an explicit
`robotlab.frame` type filter in `conversation.subscribe`; an unfiltered chat
subscription does not opt in. A frame reserves an idle socket writer using
`TryLock`, or is discarded immediately. At most one ephemeral write is active
per socket, with a 100 ms deadline. There is no frame send queue, outbox,
persistence or cursor advancement. Existing reliable-event behavior is
unchanged. `SubscribeLive` adds unsubscribe ownership for gateway feeds.

Installation only starts from `robotlab.install`, returns `{started:true}` and
emits phase events. Concurrent requests coalesce. A twenty-minute daemon-owned
context bounds the job. The installer checks system Python >=3.10, creates
the venv, reads the embedded lock, installs its exact torch pin from the CPU
wheel index, installs the remaining locked requirements and verifies imports.
Only a completed install writes the completion marker used by status. OSMesa
availability is checked through standard library locations without loading a
library. Missing OSMesa remains visible as a requirement after installation.

Public parameters reject unknown fields, trailing JSON and missing required
fields. Scenario parameters and algorithm hyperparameters remain extensible
objects. Code is capped at 256 KiB, pose JSON at 64 KiB and the full parameter
object at 2 MiB. Runtime `invalid_params`, `not_found`, `conflict`, `unavailable`
and `internal` codes map to typed API errors. Vision orchestration belongs to
the separately owned tool; the service exposes a composition-time `VisionStep`
callback and `Publish` for its vision/focus events. No model calls occur here.

The additive `robotlab.teleop.get` method accepts `{session}` and forwards to
`teleop.get_mapping`, returning `{source, code}` for either the default or
custom mapping. It uses the existing registered-method route and strict session
parameter validation.

Runtime and installer processes now start with an explicit environment allowlist.
Both retain PATH, HOME, USER, LANG, LC_*, TZ, TMPDIR, XDG_RUNTIME_DIR and
EXCALIBUR_ROBOTLAB* switches. Runtime settings for OSMesa, Python buffering and
bytecode, and OMP/MKL thread counts are fixed by the service. Other Python
configuration, loader injection variables and daemon credentials are excluded.
Only the installer additionally receives PIP_*, HTTP(S)_PROXY, ALL_PROXY,
NO_PROXY (including lowercase forms) and SSL_CERT_* for package access. This
boundary is mandatory whenever Robot Lab is enabled; it has no separate bypass
switch. Tests exercise both supervised child environments with sentinel secrets.
