// Package tools defines the tool contract of Excalibur: the catalog
// definition every tool carries, invocation, normalized results, effect
// classes and the single extension manifest (excalibur-extension.yaml).
//
// Built-in tools register through exactly the same interface as
// extensions; there is no privileged path (Section 14.12). The registry
// here is the in-memory catalog; persistence and the extension loader
// land in milestones H3 (core tools) and H13 (extensions).
//
// Architecture reference: Section 13.1 (catalog), Section 13.2 (core
// tools), Section 14.12 (extensions). Implemented in milestones H0
// (contracts, wave 0), H3 and H13.
package tools
