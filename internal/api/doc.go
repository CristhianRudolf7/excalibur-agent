// Package api defines the JSON-RPC 2.0 wire contracts of the Excalibur
// gate: envelope types, method names, request/response shapes and typed
// event schemas with cursors.
//
// The same contracts are exported as JSON Schema documents under
// packages/contracts for non-Go clients. The gate (internal/gateway)
// and the CLI (internal/cli) implement these contracts; they add no
// logic of their own.
//
// Architecture reference: Section 3.5 (client surface), Section 22.1
// (events). Implemented in wave 0.
package api
