// Package agents implements subagent profiles, identities and role
// toolsets (Sections 13.2, 15.2, 15.9).
//
// A profile is a template with default tools, model role, budgets and
// context targets. The orchestrator clamps every delegation card to the
// profile: a parent may reduce the toolset, never expand it. Custom
// user agents are profiles with their own identity document.
package agents
