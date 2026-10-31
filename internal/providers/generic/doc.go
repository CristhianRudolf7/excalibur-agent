// Package generic adapts any OpenAI-compatible Chat Completions
// server to the providers.Provider contract: streaming chat, tool
// calls, usage as reported, reasoning effort passthrough, normalized
// errors and cancellation via context.Context.
//
// It covers the long tail of Section 19.2 (vLLM, llama.cpp, Qwen,
// GLM, Kimi and other compatible services). The base URL is a
// required option: it comes from the user configuration file via the
// gateway (H3), since no standard environment variable exists. The
// key is optional because many local servers need none. Usage is
// passed through as the server reports it, never estimated; servers
// without usage details yield zero usage. Reasoning effort passes
// through as {"reasoning_effort": effort}; servers that do not know
// the field ignore it.
//
// Architecture reference: Sections 19.2, 19.5, 19.6 and 11.6.
package generic
