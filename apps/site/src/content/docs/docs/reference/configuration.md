---
title: 'Configuration'
description: 'Locate configuration files and set provider, voice, and tool options.'
---

Excalibur reads TOML configuration. Most users should begin with the user file and add project-specific settings only when they need different behavior in one project. Keep credential values out of files committed to version control.

## Find the user file

| Platform        | Default user file                 |
| --------------- | --------------------------------- |
| Linux and macOS | `~/.config/excalibur/config.toml` |
| Windows         | `%APPDATA%\Excalibur\config.toml` |

On Linux and macOS, `XDG_CONFIG_HOME` replaces `~/.config` when set. The project file is `.excalibur/config.toml`; Excalibur searches upward from the starting directory for it.

Configuration merges defaults, system, user, project, and an explicitly supplied scope file in that order. Environment values then override file values, followed by supplied flag overrides where the command supports them. Tables merge; scalar values and arrays replace earlier values.

## Set a model

This example uses a separately installed local Ollama server. Replace the placeholder with a model available on that server:

```toml
[models]
provider = "ollama"
model = "YOUR_MODEL_ID"

[tools]
default_timeout = "60s"
```

The timeout is the default tool timeout, not a promise that a whole request finishes within one minute. Commands and tools can have their own bounds.

Other model keys include `base_url`, `api_key_env`, `api_key_secret`, and `credentials_file`. For a daemon with an available vault, `api_key_secret` can refer to an existing entry such as `secret://model-key`. Creating the reference does not create or unlock the secret.

## Override a setting

The general environment form is `EXCALIBUR_SECTION__FIELD`, for example `EXCALIBUR_TOOLS__DEFAULT_TIMEOUT=60s`. Model shortcuts include `EXCALIBUR_PROVIDER`, `EXCALIBUR_MODEL`, and `EXCALIBUR_BASE_URL`. A provider's standard API-key variable can still determine its credential.

Speech options live under `[voice]`, including `enabled`, `provider`, `stt_model`, `tts_model`, and `voice`. Physical robot access has a separate `[robot]` `enabled` switch. These settings do not install optional services.

Restart the daemon after changing its startup settings. If a value seems ignored, check project and environment overrides. See [Connect a model provider](/docs/getting-started/providers/) and [Security and permissions](/docs/reference/security/) before adding credentials.
