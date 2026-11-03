package api

import "time"

const (
	MethodGoogleStatus          = "google.status"
	MethodGoogleConnectStart    = "google.connect.start"
	MethodGoogleConnectComplete = "google.connect.complete"
	MethodGoogleDisconnect      = "google.disconnect"
)

// GoogleStatusResult exposes account metadata only, never OAuth credentials.
type GoogleStatusResult struct {
	Connected     bool       `json:"connected"`
	Configured    bool       `json:"configured"`
	Email         string     `json:"email,omitempty"`
	Scopes        []string   `json:"scopes,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	Warning       string     `json:"warning,omitempty"`
	MissingScopes []string   `json:"missing_scopes,omitempty"`
}
type GoogleConnectStartResult struct {
	AuthURL string `json:"auth_url"`
}
type GoogleConnectCompleteParams struct {
	Code  string `json:"code"`
	State string `json:"state"`
}
