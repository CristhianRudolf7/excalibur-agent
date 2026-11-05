---
title: 'Connect a model provider'
description: 'Choose a provider and make its credentials available to Excalibur.'
---

Excalibur needs a model provider before it can answer requests. Have the provider's model identifier ready. For a hosted service, you also need its API credentials. For a local Ollama server, install and start that server separately and make sure the selected model is available there.

## Try a local provider

Add this table to your user `config.toml`, replacing the model placeholder:

```toml
[models]
provider = "ollama"
model = "YOUR_MODEL_ID"
```

On Linux and macOS the default file is `~/.config/excalibur/config.toml`, or the equivalent under `XDG_CONFIG_HOME`. On Windows it is under `%APPDATA%\Excalibur`. Ollama uses its local default endpoint unless you set `base_url`. The Ollama adapter does not require an API key.

After saving the file, run the first request in [Quick start](/docs/getting-started/quick-start/). Restart your daemon after changing settings used by an already running daemon.

## Use a hosted service

The API provider adapters include `anthropic`, `gemini`, `openai`, `deepseek`, and `openrouter`. Use the provider identifier and an actual model identifier from your account. A provider name alone does not select a model for you.

The standard credential environment variables are:

| Provider   | Credential variable                  |
| ---------- | ------------------------------------ |
| Anthropic  | `ANTHROPIC_API_KEY`                  |
| Gemini     | `GEMINI_API_KEY` or `GOOGLE_API_KEY` |
| OpenAI     | `OPENAI_API_KEY`                     |
| DeepSeek   | `DEEPSEEK_API_KEY`                   |
| OpenRouter | `OPENROUTER_API_KEY`                 |

Set the credential through your terminal or service's secret configuration. Do not paste it into chat or commit it in a project configuration file. The process running Excalibur must inherit the variable.

A compatible endpoint can use `generic` with an explicit `base_url`. Account sign-in is also available through Settings → Connections → ChatGPT / Codex account; connecting that account and selecting model routing are separate steps. See [Configuration](/docs/reference/configuration/) for environment overrides and vault references.
