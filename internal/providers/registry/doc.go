// Package registry builds providers.Provider instances from
// configuration. Keys resolve in the Section 19.2 and 20.1 order:
// the SecretResolver hook (the secrets vault, from H10) first, then
// the named environment variable, then the explicit configuration
// value.
//
// The constructor map is open: Register adds provider kinds without
// touching this file, so the OpenAI-family adapters of milestone H1
// plug in from their own packages. The built-ins here are the
// Anthropic and Gemini adapters.
package registry
