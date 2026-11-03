// Package googleapi connects the daemon's Google account and API consumers.
package googleapi

import (
	"context"
	"errors"
	"time"
)

// TokenProvider returns a valid OAuth access token for the connected Google account,
// refreshing it when needed. ErrNotConnected when no account is connected.
type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
	Status() Status
}
type Status struct {
	Connected bool      `json:"connected"`
	Email     string    `json:"email,omitempty"`
	Scopes    []string  `json:"scopes,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // refresh token expiry if known (testing-mode apps: ~7 days)
}

var ErrNotConnected = errors.New("googleapi: no Google account connected")
