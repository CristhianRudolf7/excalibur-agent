// Package models implements the model gateway: roles, routing, prices,
// the decider and provider failover on top of internal/providers
// adapters.
//
// Architecture reference: Section 19 (models, routing, decider and
// providers). Implemented in milestones H3 (single-model gateway), H4
// and H12 (roles, escalation, decider).
package models
