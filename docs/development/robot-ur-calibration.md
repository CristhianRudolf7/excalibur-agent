# UR camera calibration and pixel location

The daemon maps camera pixels to the robot base XY plane using a normalized
homogeneous DLT homography. Four independent correspondences determine the
mapping; additional points produce a least-squares fit. Centering and
mean-distance normalization are applied to both coordinate sets before the
9-column DLT solve. A small symmetric Jacobi eigensolver avoids a new numerical
dependency. Degenerate/collinear observations and singular transforms are
rejected. `reprojection_error_px` is the RMS Euclidean distance between measured
pixels and base observations projected back into the image. It is zero before
a fit exists; inspect `calibrated` before using it.

## Calibration RPC contract

All methods use the existing `robot.*` gateway passthrough. Every successful
calibration method returns:

```json
{
  "calibrated": true,
  "points": [
    {"pixel": [100, 100], "base": [0.1, 0.2]},
    {"pixel": [500, 100], "base": [0.5, 0.2]},
    {"pixel": [500, 400], "base": [0.5, 0.5]},
    {"pixel": [100, 400], "base": [0.1, 0.5]}
  ],
  "table_z": 0.12,
  "image": {"width": 640, "height": 480},
  "reprojection_error_px": 0
}
```

An incomplete calibration returns `calibrated:false` and an explanatory `error`.
At least four independent points and an explicitly set table height are required
for `calibrated:true`.

| RPC | Parameters | Behavior |
| --- | --- | --- |
| `robot.calibration.status` | `{}` | Reads calibration without starting Python or contacting the arm. |
| `robot.calibration.add_point` | `{"pixel":[u,v]}` | Reads the connected bridge's current six-coordinate TCP pose and records its X/Y against the pixel. Uses the current camera frame dimensions; share a frame first. |
| `robot.calibration.set_table_z` | `{"z":0.12}` or `{"use_current":true}` | Sets base-frame table Z in meters, or reads current TCP Z. Supply exactly one choice. |
| `robot.calibration.clear` | `{}` | Clears points, table height and image dimensions durably. |
| `robot.calibration.set` | `{"points":[{"pixel":[u,v],"base":[x,y]},...],"table_z":0.12,"image":{"width":640,"height":480}}` | Atomically replaces the full calibration; requires at least four independent points. No live connection or frame is required for scripted setups. |

Pixels use the original image dimensions, with origin at the top-left, U to the
right and V down. Coordinates must be finite; pixels must lie inside the image.
Collection is limited to 1024 points and the existing camera dimension limits.
Adding points after a frame-size change fails; clear and collect a new fit.
Malformed or degenerate replacements leave the previous calibration intact.

Daemon assembly and startup bind storage to `<data>/robot/calibration.json`.
Every change writes and syncs a temporary file before atomic replacement. Points,
table height and image dimensions survive restart; the fit is recomputed and
validated on load. Corrupt persisted calibration prevents robot service wiring.
Camera frames themselves retain the existing blob lifecycle. An explicitly
unbound embedded service uses isolated in-memory calibration.

Each successful change publishes the existing `robot.state` event with the
current cached `state` and an additive `calibration` field containing the status
above. Regular state events and heartbeats retain their existing payload.
The shared Go contracts and web RPC mirror add only robot calibration shapes.
This additive contract decision is recorded here within the assigned robot
documentation scope; existing RPC results and motion contracts remain stable.

## Operator workflow

Keep camera position, resolution and the working plane fixed. Jog or freedrive
the TCP tip above each marked table location, then click the corresponding
original-image pixel and call `add_point`. Use at least four well-spread marks;
avoid a line or repeated positions. The existing tool approval requirement still
applies to every jog/move and freedrive entry. Set table Z explicitly or place the
TCP at the intended plane and call `set_table_z` with `use_current:true`.
Inspect the reported error and collect better observations when necessary.
Recalibrate after moving the camera, changing the image geometry, TCP definition
or working plane; a homography represents one plane and does not estimate depth.

## Assistant tool contract

`robot({"action":"look"})` sends a PNG directly to the same language model,
with translucent grid lines every 100 pixels and numeric labels at the image
edges. It uses the already-present `golang.org/x/image` basic font. There is no
additional model, vision service or image resizing. The output retains bridge,
state and camera metadata and adds `image`, `calibration` and `instruction`.
The instruction asks the model to estimate object pixel centers and call locate.

`robot({"action":"locate","pixel":[u,v]})` needs no approval and performs
only a bridge state read. Its result is:

```json
{
  "pixel": [320, 240],
  "base": [0.32, 0.34, 0.12],
  "orientation": [0, 3.14, 0],
  "pose": [0.32, 0.34, 0.12, 0, 3.14, 0],
  "approach_pose": [0.32, 0.34, 0.22, 0, 3.14, 0]
}
```

`base` is the table-plane position in meters. `orientation` is the current TCP
rotation vector; both six-coordinate poses preserve it. `approach_pose` is
0.10 m above the plane. Locate fails clearly when calibration is incomplete,
a frame is missing, dimensions differ, a pixel is outside the image, projection
is singular, or a connected finite TCP orientation is unavailable.

The tool description gives the recipe: look, locate object pixel, request
approval to open the gripper, move to approach, descend to grasp height, close,
lift, move to a named destination such as `entrega` or `caja`, and open. Each
motion and gripper action needs its own explicit approval of the exact action;
a general pick-and-place request never supplies that approval. Account for
object height and TCP/gripper offsets when choosing grasp height. All existing
workspace, step, speed, mode and STOP enforcement stays in the tool/bridge.
The suggested poses are coordinates, not a guarantee that a move will be accepted.

Robot remains a deferred daemon tool in the default profile only. No tool names,
embedded prefix pins, tools_parity tests or eval profile are changed.

## Verification

The tests use synthetic observations, in-memory blobs and simulated HTTP bridges.
They cover exact perspective transforms (including zero H33), noisy overdetermined
fits, independently checked pixel reprojection error, insufficient/duplicate/
collinear/non-finite observations, atomic persistence/reload/clear, invalid writes,
corrupt files, RPC parameter validation, current TCP collection, change events,
real gateway routing, daemon storage binding, locate failures and numeric output,
image dimensions/grid/edge labels, and camera delivery to the same model.
Existing tests cover denied motion with zero bridge effects and independent STOP.

The generated method registry also needs these five robot entries in
`packages/contracts/methods.json`. That file is outside this task's assigned
paths; its update requires the requested scope extension. Until synchronized,
`internal/api`'s `TestContractsParity` reports 131 generated entries versus 136
Go registry entries. The required entries match `internal/api/methods.go`:

```json
[
  {"name":"robot.calibration.status","family":"robot","summary":"Read camera to base calibration."},
  {"name":"robot.calibration.add_point","family":"robot","summary":"Pair a pixel with current TCP XY."},
  {"name":"robot.calibration.set_table_z","family":"robot","summary":"Set table height in the base frame."},
  {"name":"robot.calibration.clear","family":"robot","summary":"Clear camera calibration."},
  {"name":"robot.calibration.set","family":"robot","summary":"Set camera calibration correspondences."}
]
```

## Verification record: 2026-11-03

| Check | Result |
| --- | --- |
| `systemd-run --user --scope -q -p MemoryMax=6G go test ./internal/robot/... ./internal/daemon/... ./internal/api/... ./internal/tools/...` | Robot, daemon and tools passed. API failed only `TestContractsParity`: 131 generated method entries versus 136 in Go; the five entries above remain outside assigned paths pending scope approval. Repeated on the final implementation with the same result. |
| Same scoped packages with `go test -race -p 2` under the 6 GiB cap | Robot, daemon and tools passed; API had only the same method-registry parity failure. |
| Focused daemon robot, real gateway, tool parity and frozen eval profile tests with `-race -p 2` | Passed. No pins changed. |
| `go vet -p 2 ./internal/robot/... ./internal/daemon/... ./internal/api/...` under the 6 GiB cap | Passed. |
| `gofmt -l` on all touched Go files | Clean. |
| Darwin and Windows `go build -p 2` for robot, daemon and API under the 6 GiB cap | Passed. |
| Web `pnpm check` | Passed: TypeScript, lint (existing warnings), formatting, 112 test files and 588 tests. |
| Web `pnpm build` | Passed, with existing chunk-size/dynamic-import warnings. |

The Python bridge was unchanged, so its conditional pytest run was not needed.
No hardware, URSim motion or paid model API was used for this verification.
