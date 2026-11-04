package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/coder/websocket"
)

type rpcLimitBackend struct {
	Backend
	calls atomic.Int32
}

func (b *rpcLimitBackend) Passthrough(_ context.Context, method string, params json.RawMessage) (any, error) {
	b.calls.Add(1)
	return map[string]any{"method": method, "bytes": len(params)}, nil
}

// Payload lengths include the envelope, so the tests exercise exact wire limits.
func sizedRPC(method string, size int) []byte {
	prefix := fmt.Sprintf(`{"jsonrpc":"2.0","id":"limit","method":%q,"params":{"data":"`, method)
	suffix := `"}}`
	raw := bytes.Repeat([]byte{'x'}, size)
	copy(raw, prefix)
	copy(raw[size-len(suffix):], suffix)
	return raw
}

func assertRPCSizeResult(t *testing.T, response api.Response, allowed bool, method string) {
	t.Helper()
	if !allowed {
		if response.Error == nil || response.Error.Code != api.CodeParseError || response.Error.Message != "parse error" || (len(response.ID) != 0 && string(response.ID) != "null") {
			t.Fatalf("expected original parse error without request id, got %+v", response)
		}
		return
	}
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	var result struct {
		Method string `json:"method"`
		Bytes  int    `json:"bytes"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil || result.Method != method || result.Bytes < maxRPCBodyBytes-1024 {
		t.Fatalf("missing unchanged large parameters: %+v, %v", result, err)
	}
}

var rpcSizeCases = []struct {
	name    string
	method  string
	size    int
	allowed bool
}{
	{"ordinary at 4 MiB", api.MethodPrincipalGet, maxRPCBodyBytes, true},
	{"ordinary above 4 MiB", api.MethodPrincipalGet, maxRPCBodyBytes + 1, false},
	{"other lab method above 4 MiB", api.MethodRobotLabEnvGet, maxRPCBodyBytes + 1, false},
	{"unknown method above 4 MiB", "unknown", maxRPCBodyBytes + 1, false},
	{"upload above 4 MiB", api.MethodRobotLabMediaUpload, maxRPCBodyBytes + 1, true},
	{"32 MiB base64 upload envelope", api.MethodRobotLabMediaUpload, (32<<20+2)/3*4 + 256, true},
	{"upload at 48 MiB", api.MethodRobotLabMediaUpload, maxMediaUploadRPCBodyBytes, true},
	{"upload above 48 MiB", api.MethodRobotLabMediaUpload, maxMediaUploadRPCBodyBytes + 1, false},
}

func TestHTTPRPCBodyLimits(t *testing.T) {
	backend := &rpcLimitBackend{Backend: NewMemoryBackend(nil)}
	gate := New(DefaultConfig(), backend, nil, nil)
	for _, tc := range rpcSizeCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := sizedRPC(tc.method, tc.size)
			req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(raw))
			req.RemoteAddr = "127.0.0.1:12345"
			req.ContentLength = -1 // The limit must work without a trusted Content-Length.
			response := httptest.NewRecorder()
			before := backend.calls.Load()
			gate.ServeHTTP(response, req)
			var result api.Response
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			assertRPCSizeResult(t, result, tc.allowed, tc.method)
			if got := backend.calls.Load() - before; (got == 1) != tc.allowed || got > 1 {
				t.Fatalf("incorrect backend calls: %d", got)
			}
		})
	}
}

func TestWebSocketRPCBodyLimits(t *testing.T) {
	backend := &rpcLimitBackend{Backend: NewMemoryBackend(nil)}
	server := httptest.NewServer(New(DefaultConfig(), backend, nil, nil))
	defer server.Close()
	for _, tc := range rpcSizeCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			before := backend.calls.Load()
			err = conn.Write(ctx, websocket.MessageText, sizedRPC(tc.method, tc.size))
			// The peer may close while an oversized message is still being sent.
			if err != nil && tc.size <= maxMediaUploadRPCBodyBytes {
				t.Fatal(err)
			}
			_, raw, err := conn.Read(ctx)
			if tc.size > maxMediaUploadRPCBodyBytes {
				if websocket.CloseStatus(err) != websocket.StatusMessageTooBig {
					t.Fatalf("expected message-too-big close, got %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var response api.Response
				if err := json.Unmarshal(raw, &response); err != nil {
					t.Fatal(err)
				}
				assertRPCSizeResult(t, response, tc.allowed, tc.method)
				// A method-specific rejection must leave the socket usable.
				if !tc.allowed {
					if err := conn.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":"next","method":"principal.get","params":{}}`)); err != nil {
						t.Fatal(err)
					}
					if _, _, err := conn.Read(ctx); err != nil {
						t.Fatal(err)
					}
					before++
				}
			}
			if got := backend.calls.Load() - before; (got == 1) != tc.allowed || got > 1 {
				t.Fatalf("incorrect backend calls: %d", got)
			}
		})
	}
}

func TestHTTPRPCBodyLimitCannotBeBypassed(t *testing.T) {
	backend := &rpcLimitBackend{Backend: NewMemoryBackend(nil)}
	gate := New(DefaultConfig(), backend, nil, nil)
	for name, raw := range map[string][]byte{
		"upload batch":        append(append([]byte{'['}, sizedRPC(api.MethodRobotLabMediaUpload, maxRPCBodyBytes)...), ']'),
		"trailing whitespace": append([]byte(`{"jsonrpc":"2.0","id":"limit","method":"principal.get","params":{}}`), bytes.Repeat([]byte{' '}, maxRPCBodyBytes)...),
		"nested method":       append([]byte(`{"jsonrpc":"2.0","id":"limit","method":"principal.get","params":{"method":"robotlab.media.upload","data":"`), append(bytes.Repeat([]byte{'x'}, maxRPCBodyBytes), []byte(`"}}`)...)...),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(raw))
			req.RemoteAddr = "127.0.0.1:12345"
			gate.ServeHTTP(response, req)
			var result api.Response
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			assertRPCSizeResult(t, result, false, "")
		})
	}
	if backend.calls.Load() != 0 {
		t.Fatal("oversized request reached backend")
	}
}
