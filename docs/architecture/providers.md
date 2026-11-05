# Model providers

The adapters below are implemented under `internal/providers` and built by
`internal/providers/registry`. Production entry points use the models gateway.
Tests use simulated servers; live evidence is recorded separately.

| Provider | API / protocol | Cache and token notes |
|---|---|---|
| OpenAI | Responses and Chat Completions | Automatic cache from 1024 tokens; stable `prompt_cache_key` per (role, scope) |
| Anthropic | Messages | Explicit `cache_control` marks (max 4); 5m or 1h TTL |
| Google Gemini (AI Studio) | Gemini API | Implicit cache on stable prefix |
| DeepSeek | OpenAI-compatible (Chat Completions) | Automatic prefix cache |
| OpenRouter | OpenAI-compatible (Chat Completions) | Cache depends on the backing provider |
| Ollama (local) | Local OpenAI-compatible API | Server prefix cache; zero cost |
| Generic OpenAI-compatible | Chat Completions, configurable URL | Covers vLLM, llama.cpp, Qwen, GLM, Kimi, ...; usage as reported |

Provider configuration is resolved from layered config, environment, the operator
credentials file and `secret://` vault references. See
[production wiring](../development/wire-engine.md),
[provider implementation coverage](provider-implementation.md) and
[live checks](../development/live-smoke.md). The daemon also supports a connected
Codex account; audio uses separately configured credentials.
