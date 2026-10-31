package openaiwire

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// TestTransportTLSFailureIsInput pins defect w8-bench-fixes/2 for the
// shared Chat Completions wire (used by the OpenAI, DeepSeek,
// OpenRouter, Ollama and generic adapters): a TLS verification
// failure is a deployment configuration error (KindInput), never a
// transient, so the gateway fails fast instead of retrying a call
// that cannot succeed.
func TestTransportTLSFailureIsInput(t *testing.T) {
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()
	c := testClient(t, srv, ReasonEffort)
	_, err := c.Chat(context.Background(), providers.ChatRequest{
		Model:    "m",
		Messages: []providers.Message{{Role: providers.RoleUser, Content: "x"}},
	})
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

func TestNetErrorKinds(t *testing.T) {
	certErr := errors.New(`Post "https://api.openai.com/v1/responses": tls: failed to verify certificate: x509: certificate signed by unknown authority`)
	if got := netError("p", "m", certErr, nil); got.(*providers.ProviderError).Kind != providers.KindInput {
		t.Fatalf("cert error kind = %v, want input", got)
	}
	if got := netError("p", "m", errors.New("dial tcp: connection refused"), nil); got.(*providers.ProviderError).Kind != providers.KindTransient {
		t.Fatalf("refused kind = %v, want transient", got)
	}
}
