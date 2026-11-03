package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/coder/websocket"
)

// This backend deliberately retains the borrowed params until response encoding
// so the test verifies the transport's own clearing boundary.
type credentialBufferBackend struct{ Backend }

func (*credentialBufferBackend) Passthrough(_ context.Context, _ string, params json.RawMessage) (any, error) {
	return credentialBufferProbe{params}, nil
}

type credentialBufferProbe struct{ params json.RawMessage }

func (p credentialBufferProbe) MarshalJSON() ([]byte, error) {
	cleared := bytes.Equal(p.params, make([]byte, len(p.params)))
	return json.Marshal(map[string]bool{"ok": true, "cleared": cleared})
}
func credentialRPCFrame(t *testing.T) []byte {
	t.Helper()
	req, err := api.NewRequest("credential-test", api.MethodCredentialsSubmit, api.CredentialsSubmitParams{RequestID: "request", Values: map[string]string{"password": "transport-private-sentinel"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func assertCredentialBufferResult(t *testing.T, response *api.Response) {
	t.Helper()
	var result struct {
		OK      bool `json:"ok"`
		Cleared bool `json:"cleared"`
	}
	if response.Error != nil || json.Unmarshal(response.Result, &result) != nil || !result.OK || !result.Cleared {
		t.Fatal("credential transport retained params after backend use")
	}
}

func TestCredentialRPCTransportBuffers(t *testing.T) {
	backend := &credentialBufferBackend{NewMemoryBackend(nil)}
	gate := New(DefaultConfig(), backend, nil, nil)
	raw := credentialRPCFrame(t)
	response := gate.dispatch(context.Background(), raw)
	assertCredentialBufferResult(t, response)
	if !bytes.Equal(raw, make([]byte, len(raw))) {
		t.Fatal("dispatch retained credential envelope")
	}

	server := httptest.NewServer(gate)
	defer server.Close()
	t.Run("http batch", func(t *testing.T) {
		raw := append([]byte{'['}, credentialRPCFrame(t)...)
		raw = append(raw, ']')
		resp, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var responses []api.Response
		if err := json.NewDecoder(resp.Body).Decode(&responses); err != nil || len(responses) != 1 {
			t.Fatal("missing batch response")
		}
		assertCredentialBufferResult(t, &responses[0])
	})
	t.Run("websocket", func(t *testing.T) {
		ctx := context.Background()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		if err := conn.Write(ctx, websocket.MessageText, credentialRPCFrame(t)); err != nil {
			t.Fatal(err)
		}
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var response api.Response
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		assertCredentialBufferResult(t, &response)
	})
}
