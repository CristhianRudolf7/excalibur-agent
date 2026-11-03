# Robot Lab verification evidence

Robot Lab now preserves structured arm skill results through the session wrapper.
The additive `in_target` boolean and `bin` identifier expose the existing physical
settling check. Other skills retain their string result and existing wire shape.

`VisionDecision` adds `model_verified` and `verify_note` (`simulation` or `model`).
When a skill supplies a matching target and physical evidence, `verified` uses
that evidence. Otherwise it uses the visual model answer. The UI displays both
opinions independently. No shared connector, model protocol, or eval catalog
changes are required. This follows the tool and client extension seams in
Sections 13 and 14.

The arm supplies dedicated close cameras for destination bins, so the second
image presents the bin interior at inspection scale. Simulator class labels
remain excluded from model prompts and vision events.
