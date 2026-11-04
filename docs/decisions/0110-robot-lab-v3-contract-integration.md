# 0110: Robot Lab v3 shared contract integration

The shared RPC schema and registries now expose the v3 boundary described in
0109. The repository generator produces the Go API, embedded schema and web
contract from `packages/contracts/robotlab.json`. Removed methods have no
compatibility constants or transport exceptions.

HTTP and WebSocket RPC requests share the ordinary 4 MiB envelope limit. HTTP
rejects oversized bodies before dispatch; WebSocket closes oversized messages
with the message-too-big status. Tests cover exact boundaries, batches, nested
method names and trailing whitespace.

This completes the shared-file handoff from 0109. Protected composition callers
continue to compile against the inert vision hooks.

Action log entries also declare optional structured `state`. The runtime already
persists it for replay readouts; declaring it prevents typed API round trips from
dropping the robot state. Older entries without state remain valid.
