package api

import "github.com/CristhianRudolf7/excalibur-agent/internal/types"

const (
	MethodCredentialsPending                  = "credentials.pending"
	MethodCredentialsSubmit                   = "credentials.submit"
	MethodCredentialsCancel                   = "credentials.cancel"
	EventCredentialsRequested types.EventType = "credentials.requested"
	EventCredentialsResolved  types.EventType = "credentials.resolved"
)

// CredentialRequest contains only display metadata; values use a separate RPC.
type CredentialRequest struct {
	ID              string   `json:"id"`
	ConversationID  string   `json:"conversation_id"`
	Site            string   `json:"site"`
	Purpose         string   `json:"purpose"`
	Fields          []string `json:"fields"`
	RememberDefault bool     `json:"remember_default"`
	Status          string   `json:"status"`
	CreatedAt       string   `json:"created_at"`
}

type CredentialsPendingParams struct {
	ConversationID string `json:"conversation_id"`
}
type CredentialsPendingResult struct {
	Requests []CredentialRequest `json:"requests"`
}

// CredentialsSubmitParams must never pass through conversation intake or logging.
type CredentialsSubmitParams struct {
	RequestID string            `json:"request_id"`
	Values    map[string]string `json:"values"`
	Remember  bool              `json:"remember"`
}
type CredentialsCancelParams struct {
	RequestID string `json:"request_id"`
}
type CredentialsOKResult struct {
	OK bool `json:"ok"`
}

type CredentialsRequestedEvent struct {
	RequestID       string   `json:"request_id"`
	ConversationID  string   `json:"conversation_id"`
	RunID           string   `json:"run_id"`
	Site            string   `json:"site"`
	Purpose         string   `json:"purpose"`
	Fields          []string `json:"fields"`
	RememberDefault bool     `json:"remember_default"`
	CreatedAt       string   `json:"created_at"`
}
type CredentialsResolvedEvent struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}
