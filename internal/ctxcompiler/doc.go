// Package ctxcompiler implements the layered context compiler of
// Section 11: stable-prefix layering (11.1), small active context
// budgets (11.2), batched observation masking (11.3), compaction from
// the model's own max input with the old 500 000-token ceiling kept
// only as the optional EXCALIBUR_CONTEXT_CAP cap (11.4, owner
// decision 0051), literal durable instructions (11.5), per-provider
// cache adapters (11.6) and token counting (11.7).
//
// The compiler is deterministic: compiling the same Snapshot always
// produces exactly the same bytes, which is what makes provider
// prefix caches hit. It depends only on internal/types, the provider
// request types of internal/providers, and the small source interfaces
// declared in sources.go; production sources (memory, skills, tools)
// stay behind those interfaces and tests use the fakes in the fake
// subpackage.
package ctxcompiler
