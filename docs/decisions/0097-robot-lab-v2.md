# 0097: Robot Lab v2 sessions, deterministic tools and continuation

Supersedes the Robot Lab decisions 0094 and 0095. This records the minimal shared
API contract changes for the v2 lab; eval, benchmarks, core interfaces and the
real-arm integration remain unchanged.

The previous experiment/teleoperation API is replaced by the v2 sidecar methods
with dots preserved under `robotlab.*`. Go owns status merging, explicit
installation, bounded MIME-sniffed media uploads and schema validation. The
source schema generates the Robot Lab API registry/types, JSON registry entries,
embedded Go validation data and TypeScript contract sections. Runtime validation
owns geometry and robot-specific arguments. Existing exported composition seams
remain compilable; the old separate vision step is a side-effect-free hook.

The deferred `robot_lab` tool offers training sessions, environment creation,
observations and session skills. Images travel in `tools.Result.Images` as PNG
data URLs. Session start records the whiteboard conversation and publishes focus.
All physical/session mutations dispatch once with a 95 second timeout. A failed
attempt requires reflection and an explicit reset. Success comes from simulator
goal truth. Skill reuse is explicit through session copying.

A training conversation receives 48 steps and at least 15 minutes when the steps
override is unset. Continuations use the existing master relay, only after a
turn that called the lab ends, only for its still-live owned session, and at most
`3 * max_attempts` times. Receipts identify session and sequence; persisted relay
history restores the count. Shutdown cancels and joins continuation workers.

The installer consumes the reduced Python lock and invokes the runtime-owned
pinned asset fetcher. Approximately 120 MB of Menagerie assets stay outside git;
only six robot folders are fetched at commit
`98a70c569c4c1b6da467a20012f766ee1f8dc521`, checked against a SHA-256 manifest,
with licenses and `ATTRIBUTION.md` retained. Session skills also stay outside git
under the configured skill root. Linux offscreen rendering defaults to OSMesa
when no rendering choice was supplied.

Validation uses simulated sidecars/installers, contract parity, image assertions
and fake master/turn continuation tests. See
[the development contract](../development/robot-lab.md) for commands, integration
limits and the live demonstration procedure. Full-size HTTP uploads need the
gateway owner's change to its current global 4 MiB request cap.
