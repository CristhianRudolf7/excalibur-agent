---
title: 'Extensions and the store'
description: 'Understand built-in apps, local extensions, and the current store limits.'
---

Open Extensions in the web application to find the built-in Classroom, Robotics Lab, and robot-arm entries. Their install or remove controls manage which entries appear in your local application navigation. They do not download a remote extension package or install Robotics Lab's Python dependencies.

## Choose a built-in app

For example, open the Classroom entry, make it available in navigation, and then [create a course](/docs/extensions/classroom/). Each app has its own prerequisites. Robotics Lab has a separate, explicit dependency installation step described in [its guide](/docs/extensions/robotics-lab/).

The built-in catalog is small and shipped with the application. A production remote store backend and a populated published catalog are not available in this version. Do not treat a store-looking control as proof that remote tools or workflows can be downloaded.

## Inspect local extensions

The command-line application has a separate local extension manager:

```sh
excalibur ext list
```

It lists extensions under the configured extension root. The `ext` family also has commands for scaffolding, installing from a local directory, enabling, disabling, removing, inspecting logs, and reporting health. Use `excalibur ext --help` to see the available subcommands. Some extension subcommands do not implement their own `--help`; do not append it to an install command as though it were always a read-only help action.

A local extension directory must contain its extension manifest and the files it requires. Installing a directory is different from enabling an item in the web catalog. Check its supported platforms and required commands before using it.

Only request an installation when you intend to add that extension. A listed extension is not necessarily ready to run if a required executable or service is missing. The [CLI reference](/docs/reference/cli/) and [security guide](/docs/reference/security/) explain the surrounding controls.
