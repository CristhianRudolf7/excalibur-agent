// Package providers defines the language-model adapter contract: the
// single internal message/tool format every adapter translates to and
// from, streaming chat, token usage including cache read/write,
// normalized errors and cancellation via context.Context.
//
// Concrete adapters (OpenAI, Anthropic, Gemini, DeepSeek, OpenRouter,
// Ollama, generic OpenAI-compatible) land in milestone H1; unit tests
// use the scripted fake in internal/providers/fake.
//
// Architecture reference: Section 19.2 and Appendix E, row H1.
// Implemented in milestones H0 (contracts, wave 0) and H1 (adapters).
package providers
