# Robot Lab runtime service

Robot Lab runs physics sessions and experiment control in a supervised Python
sidecar. The daemon owns the process; clients use the existing JSON-RPC gateway.
The service starts Python on the first runtime operation, waits for `ready`,
and multiplexes calls by ID over JSON lines. Status and installation are local
operations. The deferred agent tool uses this same service and supplies vision
orchestration through `Service.VisionStep`.

Runtime source is embedded in the Go binary and extracted atomically to
`<data>/robotlab/src/<content-hash>/`. Python runs with that working directory,
`MUJOCO_GL=osmesa`, `PYTHONUNBUFFERED=1`, `OMP_NUM_THREADS=2` and
`MKL_NUM_THREADS=2`. Sessions and experiments use `<data>/robotlab/`.
The runtime's experiment runner owns sandbox enforcement: experiment child
processes require bubblewrap network isolation, a read-only root, a writable
experiment directory, resource limits and low priority. The Go service itself
supervises the runtime process group; it does not sandbox the trusted runtime.

Use the install card or call `robotlab.install` explicitly to create
`<data>/robotlab/venv/` and install the embedded `requirements.lock`. Status
never installs packages. Installation needs system Python >=3.10, venv support
and package-index access. Torch uses its CPU wheel index. Install progress is
reported through `robotlab.install` events. Concurrent requests share the same
job. The OSMesa system library is detected without loading it; a missing library
is reported in `requirements.osmesa`. Install it with the operating system's
package manager before using offscreen rendering. A failed install does not
mark the environment installed; repeat the explicit install request to retry.

For development, set `EXCALIBUR_ROBOTLAB_PYTHON` to an existing interpreter with
the locked packages, then restart the daemon. This bypasses the managed venv
for execution and status; it still runs the binary's extracted source. The
install method always targets the managed venv and never modifies the override
interpreter. Set `EXCALIBUR_ROBOTLAB=off` to disable operations. Status remains
available for displaying a disabled card. Non-Linux hosts report unsupported.

Subscribe to `robotlab.frame` explicitly through `conversation.subscribe` for
live visualization. Frames are ephemeral: they are never retained, replayed,
written to telemetry or placed in an outbox. Busy socket writers discard frames;
clients render the next received state. Other Robot Lab events use normal bus
replay. Sidecar subscriber memory is bounded; reliable overflow uses a temporary
spool, while frames are discarded first. See decision 0094 for transport details.

Unit tests use a tiny Go helper process and fake installation commands. They do
not need Python, MuJoCo, package downloads or paid APIs. Run scoped checks with
`nice -n 19`, `go test -p 2` and the repository's mandatory systemd memory cap.
The optional `TestRealRuntimeOptIn` check is skipped unless
`EXCALIBUR_ROBOTLAB_PYTHON` is set; it lists scenarios and opens one session
without training or model calls. The Python runtime and scenario packages must
be integrated into the binary before running that check.

Child processes receive an allowlisted environment: basic OS paths, locale,
timezone, temporary/runtime directories and `EXCALIBUR_ROBOTLAB*` switches.
The service fixes OSMesa, Python buffering/bytecode and two-thread OMP/MKL
settings. Daemon API credentials and inherited `PYTHONPATH`, `PYTHONHOME` and
loader injection variables are excluded. Installation alone also inherits
`PIP_*`, proxy variables (uppercase and lowercase) and `SSL_CERT_*` so private
package indexes, proxies and certificate bundles continue to work. Those
installation settings do not reach the physics runtime.
