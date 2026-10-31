# Planned providers

Language-model providers of Section 19.2, wired in milestone H1 against
the `internal/providers` wave-0 contract. Status: all planned, none
implemented.

| Provider | API / protocol | Cache and token notes |
|---|---|---|
| OpenAI | Responses and Chat Completions | Automatic cache from 1024 tokens; stable `prompt_cache_key` per (role, scope) |
| Anthropic | Messages | Explicit `cache_control` marks (max 4); 5m or 1h TTL |
| Google Gemini (AI Studio) | Gemini API | Implicit cache on stable prefix |
| DeepSeek | OpenAI-compatible (Chat Completions) | Automatic prefix cache |
| OpenRouter | OpenAI-compatible (Chat Completions) | Cache depends on the backing provider |
| Ollama (local) | Local OpenAI-compatible API | Server prefix cache; zero cost |
| Generic OpenAI-compatible | Chat Completions, configurable URL | Covers vLLM, llama.cpp, Qwen, GLM, Kimi, ...; usage as reported |

Credential resolution order (19.2, 20.1): secrets vault
(`provider_credential`) first, then environment variables, then the
user configuration file. Image, video, voice, remote embeddings and GPU
compute come after H1.

A second document, `provider-implementation.md`, will record real
coverage once adapters land (per the repository rule that
`docs/development/` never describes as implemented what is only
planned, it is not created yet).
