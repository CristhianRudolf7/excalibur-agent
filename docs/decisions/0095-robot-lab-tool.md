Superseded by [0097: Robot Lab v2](0097-robot-lab-v2.md).

# 0095: Robot Lab agent tool, vision loop and skill

Sections 12–14 permit a deferred domain tool for an application with its own UI.
Robot Lab adds one `robot_lab` tool with the contract's 16 actions, available in
default/delegate and excluded from eval. It remains discoverable when disabled
or uninstalled and returns explicit-user installation guidance. No installation
operation is exposed to the model. The flat schema validates fields per action.
The tool is conservatively `exec`, non-idempotent, cooperatively cancellable,
with a 95-second timeout and 32 KB executor output budget. Frames and image bytes
never enter tool summaries; snapshots expose mark IDs and boxes only. Metric
trends compare the first and last returns in the requested window per run.

`VisionLoop` performs one marked top-camera observation, model decision, arm
pick_place and second-image verification. Total time defaults to 90 seconds;
each model phase permits one JSON repair, each answer is capped at 16 KB and
snapshots at 8 MiB/128 marks. Unknown object marks and bin IDs are rejected
before acting. The arm's bins are A (blue), B (green), and reject (red/X).
Neither ground-truth labels nor internal object names enter model prompts.
Cancellation is checked between phases; a failed verification returns false.
The UI receives focus and vision phase events, including honest unavailable
errors when no image-capable model is bound.

`ModelVisionBackend.AskImage` additively exposes arbitrary image questions
through the same bounded models gateway used by document OCR. Existing
`EXCALIBUR_VISION_MODEL` selection and provider admission apply. Document OCR's
switch remains independent. Provider adapters, model wire shapes, and the shared
Sidecar/Publisher/VisionAsker contracts remain unchanged. `robotlab.NewTool`
binds the same loop to an optional service client hook during composition.
`EmbeddedOptions.RobotLab` and daemon `RobotLabTool` options inject collaborators.
The CLI effective surface also lists Robot Lab as deferred for default/delegate,
even when disabled; the eval admission gate rejects direct and nested calls.
Production daemon assembly binds its runtime Sidecar, Service publisher, media
vision backend and client hook before serving requests. Embedded CLI runs own a
lazy runtime using the existing installation and close it when the run ends.
Neither path installs dependencies. Embedded vision setup remains available when
document OCR is disabled and Robot Lab is enabled.

The `ml/robot-lab` builtin skill teaches SDK experiments, comparisons,
from-scratch algorithms, pose mappings and vision. It declares only its domain
tool; the skill template test now recognizes that domain execution path. Builtin
counts increase by one (ml: 4 → 5; total: 257 → 258). The new skill is explicitly
excluded from `NewEvalBuiltinIndex`; frozen snapshots and eval hashes stay fixed.

Only the catalog names hash changes in `internal/daemon/tools_parity_test.go`:

- Previous: `cb7e3eb40a79a35320c2bd4bf3ba0c94aa875e1c57d8d252490190187dec3dbd`.
- New: `faf7126df38b287bde13f95602f6a7b810f1ce611f2a3ef80ea8cccd93142cc8`.
- Fixed prefix: `95b1a140c09eb43f24691ca087d359a7630592d9c147ae2fb9b84e00fc06adae`.
- Eval profile: `6bfd22cc417b7ddefb81d2193c181dcc2406d5f951ab6a90f50d1db1f9f4e025`.

Fake-sidecar tests exercise every action directly, through the executor and
through deferred call_tool on embedded and daemon surfaces. Separate tests cover
eval exclusion, disabled discovery, focus events, metric summaries, model image
transport, parser repairs, unknown marks/bins, deadlines, cancellation, skill
failure and negative verification. Tests use no paid APIs or training runs.

Post-service integration tests exercise `Service.Op("robotlab.vision.step")`
and the assembled model tool against one fake media backend, including missing
vision configuration. Embedded runtime tests verify lazy startup and eval exclusion.

Final integration checks pass for formatting, vet, static analysis, Linux/Darwin/
Windows builds, the Robot Lab/wireengine/daemon/skills/tools race suites, and focused CLI
Robot Lab/profile/eval tests. Full CLI race verification remains limited by
pre-existing connector failures: `TestThreeRoutesAgree`, `TestMCPDelegateFlow`
and `TestMCPQuestionReply`. The latter also times out in MCP pipe cleanup on the
untouched starting commit `1b8d59b`; the other two were reproduced there during
earlier validation. Connector files are outside this change's scope. These
failures must be resolved separately before claiming a green full CLI suite.

After merging main (which added its own deferred tools and moved the fixed prefix and eval
hashes for its own reasons), the catalog names hash including `robot_lab` is
`76b4c294a8758c7affee763e29dc6f33760f13ddddd90a7903c5f5dbf522099e`. The fixed prefix
(`13e36f12…1557`) and eval profile hash (`4ecb6a58…ec28`) are exactly main's; Robot Lab does
not move either.
