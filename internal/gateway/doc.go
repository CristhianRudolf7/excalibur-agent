// Package gateway implements the HTTP and WebSocket gate, the API, idempotent
// submission with client_msg_id, the outbox and channel adapters.
//
// Architecture reference: Section 3.5 (client surface), Section 6.1
// (intake). Implemented in milestone H9.
package gateway
