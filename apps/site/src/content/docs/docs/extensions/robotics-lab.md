---
title: 'Robotics Lab'
description: 'Install the optional simulation runtime and inspect a robot session.'
---

Robotics Lab runs robot simulations. It is separate from the physical robot-arm interface. A simulated movement does not move attached hardware, and a successful simulation is not proof of safe behavior on a real robot.

## Prepare the runtime

Open Robotics Lab and check its installation status. The simulation runtime needs Python 3.10 or newer and its optional Python dependencies. Linux also needs OSMesa for offscreen rendering. macOS uses built-in CGL rendering and requires Apple Silicon; the pinned dependencies do not support Intel Macs.

Windows and macOS support remain in progress. Runtime support and the availability of a tool in an agent profile can differ. Use the status reported by your installation rather than assuming that every platform has the same working setup.

Select Install explicitly to install the optional dependencies. Opening the status page does not install them automatically. Watch the progress and any error. An available navigation entry does not mean the runtime is ready.

## Start a small simulation

With the runtime ready and an agent connected, ask for a simple goal. For example:

> Start an arm simulation in a table environment. Inspect the robot and scene before attempting a movement.

The implemented simulated robots include an arm and a quadruped. Sessions use an environment and expose observations, robot state, activity, and logs. Camera and scene views help inspect the situation; use the reported state and goal result as well as the picture.

## Review and stop

Ask for the current session status if a movement has an uncertain result. Do not infer success from an attractive pose alone. You can stop a session explicitly and later ask to resume a matching previous session rather than starting unrelated work.

Environment editing is available through text requests. Give concrete dimensions and clarify ambiguous measurements before running the scene. For other optional apps, return to [Extensions](/docs/extensions/overview/).
