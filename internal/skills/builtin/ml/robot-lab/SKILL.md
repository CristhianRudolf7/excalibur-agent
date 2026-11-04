---
name: robot-lab
description: Train simulated arm and dog robots with low-level keyframes, learn markdown session skills, and create environments from chat text.
metadata:
  excalibur:
    group: ml
    version: 3.0.0
    platforms:
    - linux
    tools:
    - robot_lab
---
# Robot Lab

## When to use

Discover `robot_lab` with `search_tools` for simulated arm (table) or dog (room)
training. Installation is explicit. Physics advances during motion and `wait`;
between calls motors hold their targets.

## Resume a previous simulation

For retry/repeat/resume requests, including "vuelve a intentar", call `sessions`
with `robot`, `environment` or `query` filters. Select the newest matching saved
session, then `session_resume {session}`. Never use `session_start` for a retry.
Resume even succeeded, stopped or exhausted sessions: saved skills and replay
remain, a new attempt opens, and the configured attempt allowance is added.

The tool automatically stops another live session by agent, reports its ID in
`stopped_session`, retries resume once and focuses the requested session. If that
session is already live, it returns and focuses it without resetting the attempt.
Read the card and relevant skills with `skill_use`, then continue training.

## Procedure

1. Read the robot card: joints, limits, reference pose, sensors, cameras and tool
   schemas. Inspect the environment with `environment_get`/`environment_preview`.
   Only new training uses `session_start` with robot, environment, task and goal.
2. List `skills`, read/use relevant markdown instructions with `skill_use`, and
   write `note(kind=plan)`. Plan small keyframes; check reachability with `ik`.
3. Prefer `act(tool=sequence)` with 4–12 keyframes per call. Read state, contacts,
   goal and attempt after every call; dog: support, margin and terrain. Use `look`
   for images. Never claim success unless `goal.met=true` or status is `succeeded`.
4. After EVERY failed or abandoned attempt, reflect with `note(kind=reflection)`.
   `skill_write` or improve a markdown skill: save the mistake, lesson AND exact
   keyframes that worked so far, with `failures:[N]`. Then `attempt_reset` with a
   reflection; give `no_skill_reason` (max 2000 characters) only without a useful
   lesson. Repeat until goal or attempt limit (default 40, settings 1–100).
5. Summarize created/improved skills and their uses/success rates. Every write is
   accepted and versioned with failure evidence. Reuse via `skills_copy {from,to}`
   or `copy_skills_from` on start; execute saved keyframes explicitly through `act`.

## Control and environments

Angles are degrees, lengths meters, time simulation seconds. Sequences accept
1–16 keyframes, at most 30 s total; settle defaults to 0.2 s (0–2). Rejected
unreachable targets cause no motion; `ik` advances no time. `wait` holds 0.05–5 s.
Dog: move_joints, move_feet, sequence, ik, wait. Keep three feet planted and COM
inside their support triangle; unlisted feet keep world targets when trunk moves.
Arm: move_joints, move_gripper_to, gripper, sequence, ik, wait. Grasp assist needs
both finger pads on the same dynamic object; opening releases it. Read the card
for arguments, cameras and `stop_if` conditions.

Create/edit environments ONLY from chat text with `environment_create` or
`environment_update`: version 1, unique slug IDs, kind room|table, simple objects,
`source:{type:"text",notes?}`. Sizes are full dimensions, positions centers, yaw
degrees; omitted z rests on floor/table. Ask about ambiguous dimensions, record
assumptions and preview. Built-ins: `escalera-estrecha` (dog, `robot_on` descanso,
4 feet) and `estante-pequeno` (arm, `object_on` caja on estante). Goals require
continuous stability (`stable_s`, default 1 s); a held box is not placed.

## Pitfalls

Respect user stop; resume a stopped session only on a new explicit request.
After timeout/transport failure, inspect `session_status` before another mutation.
The tool's confirmed live-conflict recovery is bounded to one resume retry.
Continuations require the same current live session and owner; stops cancel them,
and newer user messages take priority. Lab turns get 120 steps and at least
30 minutes when `EXCALIBUR_MASTER_STEPS` is unset; relay cap is `3 * max_attempts`.

## Verification

Use simulator goal truth, saved attempts, replay/logs and skill versions/failure
evidence. An image is not proof of success. Report the actual outcome and attempt.
See `docs/development/robot-lab.md` for backend checks and integration limits.
