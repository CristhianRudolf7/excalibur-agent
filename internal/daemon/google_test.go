package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/googleapi"
)

type fakeGoogle struct {
	connected bool
	fail      bool
}

func (f *fakeGoogle) AccessToken(context.Context) (string, error) { return "fake-private-access", nil }
func (f *fakeGoogle) Status() googleapi.Status {
	return googleapi.Status{Connected: f.connected, Email: "person@example.test"}
}
func (f *fakeGoogle) ConnectionStatus(context.Context) googleapi.ConnectionStatus {
	return googleapi.ConnectionStatus{Status: f.Status(), Configured: true}
}
func (f *fakeGoogle) Start(context.Context) (string, string, error) {
	return "https://accounts.google.test/auth?state=fake-state", "fake-state", nil
}
func (f *fakeGoogle) Complete(_ context.Context, code, state string) (googleapi.ConnectionStatus, error) {
	if f.fail || code != "fake-code" || state != "fake-state" {
		return googleapi.ConnectionStatus{}, errors.New("googleapi: invalid OAuth state")
	}
	f.connected = true
	return f.ConnectionStatus(context.Background()), nil
}
func (f *fakeGoogle) Disconnect(context.Context) error { f.connected = false; return nil }
func TestGoogleRPCThroughGateway(t *testing.T) {
	p := &fakeGoogle{}
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Google: p})
	s := httptest.NewServer(gateway.New(gateway.DefaultConfig(), b, nil, nil))
	defer s.Close()
	for _, tc := range []struct {
		method, params string
		connected      bool
		bad            bool
	}{
		{api.MethodGoogleStatus, `{}`, false, false},
		{api.MethodGoogleConnectStart, `{}`, false, false},
		{api.MethodGoogleConnectComplete, `{"code":"fake-code","state":"fake-state"}`, true, false},
		{api.MethodGoogleStatus, `{}`, true, false},
		{api.MethodGoogleDisconnect, `{}`, false, false},
		{api.MethodGoogleConnectComplete, `{}`, false, true},
		{api.MethodGoogleConnectComplete, `{"code":"fake-code","state":"wrong"}`, false, true},
	} {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "google-test", "method": tc.method, "params": json.RawMessage(tc.params)})
		res, err := http.Post(s.URL+"/rpc", "application/json", strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		var out api.Response
		err = json.NewDecoder(res.Body).Decode(&out)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if tc.bad {
			if out.Error == nil {
				t.Fatal("bad callback accepted")
			}
			continue
		}
		if out.Error != nil {
			t.Fatal(out.Error)
		}
		if strings.Contains(string(out.Result), "fake-private-access") {
			t.Fatal("credential leaked")
		}
		if tc.method == api.MethodGoogleConnectStart {
			var start api.GoogleConnectStartResult
			_ = json.Unmarshal(out.Result, &start)
			if start.AuthURL == "" {
				t.Fatal("missing authorization URL")
			}
		} else {
			var status api.GoogleStatusResult
			if err := json.Unmarshal(out.Result, &status); err != nil || status.Connected != tc.connected || !status.Configured {
				t.Fatalf("bad status: %s", out.Result)
			}
		}
	}
}
func TestGoogleUnconfiguredRPC(t *testing.T) {
	out, err := googleOp(context.Background(), nil, api.MethodGoogleStatus, nil)
	if err != nil || out.(api.GoogleStatusResult).Configured {
		t.Fatal("unconfigured status missing")
	}
	if _, err := googleOp(context.Background(), nil, api.MethodGoogleConnectStart, nil); err == nil {
		t.Fatal("unavailable connection accepted")
	}
}
