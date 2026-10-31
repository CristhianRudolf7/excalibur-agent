package gemini

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// TestChatTLSFailureIsInput pins defect w8-bench-fixes/2: a TLS
// verification failure (task image without CA certificates reaching
// an HTTPS endpoint) is a deployment configuration error, so the
// adapter surfaces KindInput and fails fast without burning the
// retry budget.
func TestChatTLSFailureIsInput(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	p := New(Options{APIKey: "k", BaseURL: srv.URL, Model: "fallback", MaxAttempts: 3})
	_, err := p.Chat(context.Background(), testRequest())
	var perr *providers.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %v, want provider error", err)
	}
	if perr.Kind != providers.KindInput {
		t.Fatalf("kind = %s, want %s (%v)", perr.Kind, providers.KindInput, err)
	}
	if perr.Retryable() {
		t.Fatalf("TLS failure must not be retryable: %v", err)
	}
}
