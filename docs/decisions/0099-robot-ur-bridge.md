# 0099: Supervised UR bridge and model-visible tool images

RB1 adds physical robot control through an isolated local Python bridge,
using the extension and client API seams of Sections 3.5, 13, 14.12 and 19.2.
The architecture document remains external.

The additive RPC methods are `robot.status`, `robot.connect`,
`robot.disconnect`, `robot.stop` and `robot.frame`; `robot.state` is the
additive event. Parameter, result and event types live in `internal/api`,
with matching definitions in `packages/contracts`. No existing method or
core interface changes shape. `robot.enabled` defaults true for the dev
daemon and gates both service wiring and assistant tool availability.

The Python bridge owns approval, mode, reachability, workspace and rate
checks. A killable RTDE worker prevents a blocked native call from outliving
the HTTP motion deadline. STOP has independent sockets and no motion lock;
it invalidates the session and requires reconnect before new motion.
Native subprocesses use the existing platform process-group implementation.
Shutdown stops physical activity before group cleanup; recovery never
replays motion. Freedrive adds sampled monitoring and does not replace the
controller's configured physical safety limits.

`tools.Result` and `providers.ToolResult` gain an optional `images` array,
with `{url}` entries; the public extension SDK gets the same additive shape.
Images remain distinct from user-facing artifacts. The engine preserves
these inputs through normal, recovery and explicit-finish history paths.
OpenAI Responses serializes tool outputs as a content array when images
are present and keeps the existing string representation otherwise.
Chat Completions rejects unsupported images explicitly. The robot `look`
action uses this path to supply the uploaded camera image to the existing
model without a second model call.

The daemon registers `robot` as deferred and explicitly allows it in short
conversational turns while enabled. Its local builtin extension manifest
uses `ProvideBuiltin`/`LoadDir`/`Activate`; automatic activation of staged
extensions and a remote store remain out of scope. Embedded tool names,
fixed prefixes, eval tables, skill fingerprints and eval profile hashes
are unchanged. See `docs/development/robot-ur.md` for concrete requests,
limits, setup, rollback and backend tests.
