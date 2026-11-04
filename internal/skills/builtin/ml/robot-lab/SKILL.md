---
name: robot-lab
description: Train simulated robots with deterministic tools, inspect images, improve session skills, and build environments from text or media.
metadata:
  excalibur:
    group: ml
    version: 2.0.0
    platforms:
    - linux
    tools:
    - robot_lab
---
# Robot Lab

## When to use

Use `robot_lab` for simulated training sessions with the arm, dog with arm, drone,
or humanoid. Discover it with `search_tools`. Installation is an explicit user
operation. If unavailable, explain how to enable the lab and install its runtime
and robot assets. Physics advances only while a predefined action or skill runs;
the world remains frozen between calls.

## Procedure

### Run a training session

1. Call `robots` and read the selected body's card, tools, argument schemas,
   cameras and limits. Use `environments` and `environment_get` to choose a world.
   The `arm` needs a `table`; `dog`, `drone` and `humanoid` need a `room`.
2. Translate the user's task into a simulator goal. Call `session_start` with
   `robot`, `environment`, `task` and `goal`. Retain the returned session ID.
3. Read the returned body card and session skills. Use `skill_read` for details,
   and `skill_use` before following an existing skill's instructions. Write a
   short plan with `note`, `kind: "plan"`.
4. Alternate `act` and `look`. `act` takes `session`, the predefined `tool` name
   and its `args` object. `look` returns an image, visible object estimates and
   state without advancing physics. Inspect the actual goal and attempt status.
5. After a failure, explain the observed cause with a `reflection` note. Write or
   improve a skill with `skill_write` and `failures: [attempt_number]` so its
   failure evidence is retained. Then call `attempt_reset` with a nonempty
   `reflection` and try the improved approach.
6. Continue until simulator truth reports success or the attempt limit ends the
   session. Never claim success unless `goal.met` is true or session status is
   `succeeded`. Use `session_status` after uncertain outcomes. `session_stop`
   ends training; a user stop must never be undone by starting another session.

Do not blindly retry `act`, `skill_run`, `session_start`, `session_stop` or
`attempt_reset` after a timeout or transport failure. Read session status and
observe before deciding the next action. The lab handles bounded continuation
between conversational turns; do not treat one completed reply as completed
training.

Goals use horizontal distances in meters:

```json
{"type":"object_near","object":"ball","target":{"robot_start":true},"radius":0.5}
```

Other goals are `robot_near` with a target and radius, `object_held` with an
object, `object_on` with an object and `target: {"object": "bin"}`, or `all` with
nested `goals`. Targets are `{point: [x,y]}`, `{object: "id"}` or
`{robot_start: true}`. A fallen robot cannot satisfy a goal.

### Write and reuse skills

Skills belong to a single training session. They are stored outside the
repository under `<skills-root>/<session>/<skill>/`, with `SKILL.md`, optional
`skill.py`, version history and `fallos/` failure evidence. To reuse skills from
another session, pass `copy_skills_from` on `session_start` or call
`skills_copy` with `from` and `to`. Do not assume skills are globally available.

`skill_write` requires `session`, a slug `name`, `description` and `instructions`.
Optional `code` defines `run(robot, **args)`, composing the robot's predefined
tools. Read the body card to get actual tool names and arguments; do not invent
actuator controls. Example code that inspects state and records a plan:

```python
def run(robot, **args):
    visible = robot.look()
    robot.log("Inspect the visible objects before approaching")
    return {"visible": visible, "state": robot.state()}
```

The skill API also exposes `robot.objects()` and `robot.<tool>(**args)`. Only
`math` imports are allowed. File access, arbitrary imports, `exec`, `eval` and
dunder attributes are rejected. Skill execution is bounded to 60 simulation
seconds. Goal, fall, safety, user stop and timeout interrupt execution.

When code changes and saved failures exist, the runtime replays each failure for
at most 30 simulation seconds. It accepts a version only when the new pass count
is at least the old count. Check `accepted` and `regression`; a rejected candidate
is archived while the current skill remains unchanged. Versions track uses and
success rate. Grasp assist is a simulator convenience: closing near an object
within 0.04 m attaches it; opening releases it.

### Create an environment

Use `environment_create` or `environment_update` with an `environment` object.
Inspect with `environment_preview`, which returns an image for the agent.
Environments consist of simple shapes; dimensions are full dimensions in meters,
positions are centers, and yaw is in degrees.

```json
{
  "version": 1,
  "id": "practice-room",
  "name": "Practice room",
  "kind": "room",
  "size": [5, 4],
  "wall_height": 2.5,
  "objects": [
    {"id":"ball","name":"Ball","shape":"sphere","size":[0.12],"pos":[1,0],"color":"#ff6600","dynamic":true},
    {"id":"desk","name":"Desk","shape":"box","size":[1.2,0.6,0.75],"pos":[-1,1],"dynamic":false}
  ],
  "spawn": {"x":0,"y":0,"yaw":0},
  "source": {"type":"text","notes":"Dimensions specified by the user"}
}
```

Sphere size is `[diameter]`, box is `[x,y,z]`, cylinder/capsule is
`[diameter,height]`. Omit position z to rest on the floor or table. Use unique
slug IDs, `#rrggbb` colors, at most 200 objects, and room sides between 1 and 30 m.
Objects must fit inside the world; spawn must be outside fixed furniture. A
`table` defaults to a 1.2 × 0.8 m top at 0.75 m height.

For image/video uploads, call `media` with the returned `media_id` to inspect its
frames and the user's specification. Ask the user about ambiguous measurements.
Use reference objects such as doors around 0.9 × 2.0 m and tables around 0.75 m
high, and give the user's stated measurements priority. Record assumptions in
`source.notes`, preserve `source.media_id`, and preview the result before
training. Do not silently treat image pixel coordinates as meters.

## Pitfalls

A completed tool call is not proof that the task succeeded. Check simulator goal
truth. Do not reuse another session's skill names until copying them. Do not
reset a failed attempt without recording a reflection and preserving useful
failure evidence. Avoid blindly retrying uncertain physical actions, guessing
ambiguous dimensions from media, or restarting a session the user stopped.

## Verification

Read the body card, preview the chosen environment, inspect observations and
attempt status, and confirm skill regression acceptance. Report the simulator's
session outcome and the attempt that produced it. Replay and logs preserve
frame-by-frame evidence; do not infer success from a plausible camera image.
