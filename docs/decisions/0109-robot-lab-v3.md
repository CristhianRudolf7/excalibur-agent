# 0109: Robot Lab v3 Go boundary

Robot Lab now exposes only arm and dog, with low-level joints, end-point targets
and keyframe sequences. The tool and daemon continuation teach card/skill reads,
IK checks, batches of 4–12 keyframes, state inspection, and markdown lessons with
exact working keyframes before resetting a failed attempt.

The Go API rejects media operations and executable skills; skill writes contain
instructions and optional failure references. Environment input is text only.
Generated types include stable placement goals and `robot_on`. The built-in
skill and milestone guide document the two shipped environments and preserve
session-local versions, failure evidence, usage accounting, replay and stop.

Lab turns get 120 steps and a minimum 30-minute timeout when the operator has
not set `EXCALIBUR_MASTER_STEPS`; longer timeouts and the continuation cap of
`3 * max_attempts` are preserved. Tests exercise the budgets through a turn that
exceeds the former 48-step limit, without network calls.

Existing protected composition callers retain `VisionLoop`, `VisionAsker`,
`NewTool` and the service's function hook. The hook returns `(nil, nil)` and
performs no work. `look`, its `act` alias and environment previews still return
images. The retired upload method name remains only as a compatibility constant
because the unowned gateway references it; it does not enable an operation.
The director must remove that gateway exemption and reconcile the shared schema
and registries with the generated v3 consumers during integration. Worker
ownership prevents those edits in this milestone's branch.

The benchmark engine and real-arm bridge are unchanged. Runtime physics and web
behavior require companion implementation and integrated verification; Go
fixtures do not establish live training success.
