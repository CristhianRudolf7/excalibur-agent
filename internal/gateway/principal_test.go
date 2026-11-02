package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

type principalBackend struct {
	Backend
	calls int
}

func (b *principalBackend) Passthrough(_ context.Context, method string, _ json.RawMessage) (any, error) {
	b.calls++
	if method != api.MethodPrincipalGet {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown"}
	}
	return api.PrincipalGetResult{ID: "local", Name: "Ana Prueba", Timezone: "UTC", Language: "es"}, nil
}
func TestPrincipalDispatchHTTP(t *testing.T) {
	backend := &principalBackend{Backend: NewMemoryBackend(nil)}
	server := New(DefaultConfig(), backend, nil, nil)
	ts := httptest.NewServer(server)
	defer ts.Close()
	for _, tc := range []struct {
		method, params string
		code           int
	}{{api.MethodPrincipalGet, "", 0}, {api.MethodPrincipalGet, `,"params":{"anything":true}`, 0}, {"missing.method", "", api.CodeMethodNotFound}} {
		req := `{"jsonrpc":"2.0","id":"profile","method":"` + tc.method + `"` + tc.params + `}`
		resp, err := http.Post(ts.URL+"/rpc", "application/json", strings.NewReader(req))
		if err != nil {
			t.Fatal(err)
		}
		var result api.Response
		err = json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if tc.code != 0 {
			if result.Error == nil || result.Error.Code != tc.code {
				t.Fatalf("unknown: %+v", result)
			}
			continue
		}
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		var profile api.PrincipalGetResult
		if err := json.Unmarshal(result.Result, &profile); err != nil || profile.Name != "Ana Prueba" {
			t.Fatalf("result %s %v", result.Result, err)
		}
	}
	if _, err := server.route(context.Background(), api.MethodPrincipalGet, json.RawMessage(`{`), "test"); err == nil || err.Code != api.CodeInvalidParams {
		t.Fatalf("invalid params: %v", err)
	}
	if backend.calls != 2 {
		t.Fatalf("dispatch calls: %d", backend.calls)
	}
}
