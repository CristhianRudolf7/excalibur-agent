package codexlink

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

const richResponse = `{"id":"resp-test","object":"response","status":"completed","model":"test-model","output":[{"id":"reason","type":"reasoning","summary":[{"type":"summary_text","text":"plan"}],"encrypted_content":"opaque-reason"},{"id":"message","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]},{"id":"function","type":"function_call","call_id":"call-test","name":"inspect","arguments":"{\"value\":1}"}],"usage":{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":2},"output_tokens_details":{"reasoning_tokens":1}}}`

func richSSE() string {
	return "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-test\",\"output\":[]}}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":" + richResponse + "}\n\ndata: [DONE]\n\n"
}
func requestRelay(t *testing.T, r *Relay, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "http://localhost/v1/responses", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Authorization", "Bearer "+r.Key())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestRelayTranslationAggregationAndPassthrough(t *testing.T) {
	s := isolatedStore(t)
	tok := testTokens()
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		for name, want := range map[string]string{"Authorization": "Bearer " + tok.AccessToken, "chatgpt-account-id": tok.AccountID, "originator": "codex_cli_rs", "OpenAI-Beta": "responses=experimental", "Accept": "text/event-stream"} {
			if r.Header.Get(name) != want {
				t.Errorf("wrong header %s", name)
			}
		}
		if r.Header.Get("session_id") == "" || r.Header.Get("session-id") == "" {
			t.Error("missing session")
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Error("bad request")
		}
		if b["instructions"] != "base\n\nsystem text\n\ndeveloper text" || b["stream"] != true || b["store"] != false || b["service_tier"] != "priority" {
			t.Errorf("incorrect mapping: %+v", b)
		}
		for _, key := range []string{"max_output_tokens", "temperature", "top_p", "stream_options", "unknown"} {
			if _, ok := b[key]; ok {
				t.Errorf("rejected field retained: %s", key)
			}
		}
		input := b["input"].([]any)
		if len(input) != 3 {
			t.Errorf("lost tool round-trip input: %d", len(input))
		}
		if _, ok := b["reasoning"]; !ok {
			t.Error("reasoning lost")
		}
		if _, ok := b["tools"]; !ok {
			t.Error("tools lost")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, richSSE())
	}))
	defer upstream.Close()
	relay, err := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	input := `{"model":"test-model","instructions":"base","input":[{"role":"system","content":[{"type":"input_text","text":"system text"}]},{"role":"developer","content":"developer text"},{"role":"user","content":"run"},{"type":"function_call","call_id":"old","name":"inspect","arguments":"{}"},{"type":"function_call_output","call_id":"old","output":"yes"}],"tools":[{"type":"function","name":"inspect","parameters":{"type":"object"}}],"reasoning":{"effort":"low"},"service_tier":"fast","temperature":1,"top_p":1,"max_output_tokens":100,"stream_options":{},"unknown":"discard","stream":%t}`
	w := requestRelay(t, relay, fmt.Sprintf(input, false))
	if w.Code != 200 {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var got, want any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	_ = json.Unmarshal([]byte(richResponse), &want)
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if !bytes.Equal(a, b) {
		t.Fatalf("response changed: %s", a)
	}
	w = requestRelay(t, relay, fmt.Sprintf(input, true))
	if w.Code != 200 || w.Body.String() != richSSE() || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal("SSE passthrough changed")
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected retries")
	}
}
func TestRelayAuthenticationAndInvalidInput(t *testing.T) {
	s := isolatedStore(t)
	relay, _ := NewRelay(s, RelayOptions{})
	for _, tc := range []struct{ addr, key string }{{"192.0.2.1:1234", relay.Key()}, {"127.0.0.1:1234", "wrong"}, {"[::1]:1234", ""}, {"broken", relay.Key()}} {
		req := httptest.NewRequest("POST", "http://localhost/responses", strings.NewReader(`{}`))
		req.RemoteAddr = tc.addr
		req.Header.Set("Authorization", "Bearer "+tc.key)
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		relay.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Errorf("accepted unauthorized caller: %s", tc.addr)
		}
	}
	for _, body := range []string{`not json`, `null`, `{"stream":"true"}`, `{"instructions":[]}`, `{"input":2}`, `{"input":[{"role":"system","content":[{"type":"input_image"}]}]}`} {
		if w := requestRelay(t, relay, body); w.Code != 400 {
			t.Errorf("accepted invalid body: %s", body)
		}
	}
	if w := requestRelay(t, relay, `{"input":"hello"}`); w.Code != 401 {
		t.Fatal("missing sign-in should fail")
	}
}
func TestRelay401RefreshAndRedaction(t *testing.T) {
	s := isolatedStore(t)
	tok := testTokens()
	tok.AccessToken = "test-rejected-access"
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	var refresh, calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refresh.Add(1)
			_ = json.NewEncoder(w).Encode(Tokens{AccessToken: "test-new-access"})
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") == "Bearer "+tok.AccessToken {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, tok.AccessToken)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-new-access" {
			t.Error("refreshed token missing")
		}
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"test-new-access test-refresh\"}]}]}}\n\n")
	}))
	defer upstream.Close()
	relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL, TokenURL: upstream.URL + "/token"})
	w := requestRelay(t, relay, `{"input":"hello"}`)
	if w.Code != 200 || strings.Contains(w.Body.String(), "test-new-access") || strings.Contains(w.Body.String(), tok.RefreshToken) {
		t.Fatalf("unsafe refresh response: %d", w.Code)
	}
	if refresh.Load() != 1 || calls.Load() != 2 {
		t.Fatal("401 must refresh and retry exactly once")
	}
	got, err := s.load()
	if err != nil || got.tokens.AccessToken != "test-new-access" || got.tokens.RefreshToken != tok.RefreshToken {
		t.Fatal("refresh rewrite lost fields")
	}
}
func TestRelayQuotaAndUpstreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		stream   bool
		want     int
		contains string
	}{
		{"limit", 429, `{"error":{"type":"usage_limit_reached","resets_at":1900000000,"message":"test-refresh"}}`, false, 429, "2030-03-17T17:46:40Z"},
		{"limit-status", 403, `{"error":{"code":"usage_limit_reached","reset_at":"2030-01-01T00:00:00Z"}}`, false, 429, "2030-01-01"},
		{"sse-limit", 200, "data: {\"type\":\"error\",\"error\":{\"code\":\"usage_limit_reached\",\"resets_at\":1900000000}}\n\n", false, 429, "resets at"},
		{"stream-limit", 200, "data: {\"type\":\"error\",\"code\":\"rate_limit_exceeded\"}\n\n", true, 429, "usage limit"},
		{"unavailable", 503, `test-refresh Bearer secret`, false, 503, "rejected"},
		{"truncated", 200, "data: {\"type\":\"response.created\"}\n\n", false, 502, "without a valid response"},
		{"malformed", 200, "data: invalid\n\n", false, 502, "without a valid response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := isolatedStore(t)
			if err := s.Save(testTokens()); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "12")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
			w := requestRelay(t, relay, fmt.Sprintf(`{"stream":%t}`, tc.stream))
			if w.Code != tc.want || !strings.Contains(w.Body.String(), tc.contains) || strings.Contains(w.Body.String(), "test-refresh") {
				t.Fatalf("unexpected response %d %s", w.Code, w.Body.String())
			}
			if calls.Load() != 1 {
				t.Fatal("relay retried usage limit")
			}
		})
	}
}
func TestAggregateItemDoneAndMultiline(t *testing.T) {
	stream := "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\n" + "data: \"item\":{\"type\":\"function_call\",\"call_id\":\"call\",\"name\":\"inspect\",\"arguments\":\"{}\"}}\n\n" + "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"usage\":{\"input_tokens\":3}}}\n\n"
	b, err := aggregate(strings.NewReader(stream))
	if err != nil || !strings.Contains(string(b), `"call_id":"call"`) || !strings.Contains(string(b), `"input_tokens":3`) {
		t.Fatal("lost output item or usage")
	}
}
func TestRelayServerLifecycle(t *testing.T) {
	s := isolatedStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := Listen(ctx, s, RelayOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	response, err := http.Post(server.BaseURL()+"/responses", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("listener is unprotected")
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		response, err = http.Get(server.BaseURL())
		if err != nil {
			return
		}
		_ = response.Body.Close()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("listener did not stop")
}

func TestQuotaResetHeadersAndStreamRedaction(t *testing.T) {
	for _, header := range []http.Header{{"X-Codex-Primary-Reset-At": []string{"1900000000"}}, {"Retry-After": []string{"Sun, 17 Mar 2030 17:46:40 GMT"}}} {
		if got := limitMessage([]byte(`{}`), header); !strings.Contains(got, "2030-03-17T17:46:40Z") {
			t.Fatalf("missing header reset time: %s", got)
		}
	}
	s := isolatedStore(t)
	tok := testTokens()
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "event: error\ndata: {\"type\":\"error\",\"message\":%q}\n\n", tok.AccessToken+" "+tok.RefreshToken)
	}))
	defer upstream.Close()
	relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
	w := requestRelay(t, relay, `{"stream":true}`)
	if strings.Contains(w.Body.String(), tok.AccessToken) || strings.Contains(w.Body.String(), tok.RefreshToken) || !strings.Contains(w.Body.String(), "[redacted]") {
		t.Fatal("stream leaked credentials")
	}
}
func TestRelayRejectsRedirects(t *testing.T) {
	s := isolatedStore(t)
	if err := s.Save(testTokens()); err != nil {
		t.Fatal(err)
	}
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()
	relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
	w := requestRelay(t, relay, `{}`)
	if w.Code != 307 || forwarded.Load() != 0 {
		t.Fatal("followed upstream credential redirect")
	}
}

type afterTerminalReader struct{}

func (afterTerminalReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("stream remained open after terminal event")
}
func TestAggregateStopsAtTerminalEvent(t *testing.T) {
	completed := "data: {\"type\":\"response.completed\",\"response\":" + richResponse + "}\n\n"
	if _, err := aggregate(io.MultiReader(strings.NewReader(completed), afterTerminalReader{})); err != nil {
		t.Fatal("terminal response waited for connection close")
	}
}

func TestRelayCancellationReachesUpstream(t *testing.T) {
	s := isolatedStore(t)
	if err := s.Save(testTokens()); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	stopped := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the request so the server can observe peer cancellation while idle.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer upstream.Close()
	relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest("POST", "http://localhost/responses", strings.NewReader(`{}`)).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+relay.Key())
	done := make(chan struct{})
	safego.Go("relay-cancel-test", nil, func() { defer close(done); relay.ServeHTTP(httptest.NewRecorder(), request) })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not start")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not return")
	}
}

func TestRelayInputErrorDetailsRedacted(t *testing.T) {
	s := isolatedStore(t)
	tok := testTokens()
	if err := s.Save(tok); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": "unsupported parameter; " + tok.AccessToken + " " + tok.RefreshToken}})
	}))
	defer upstream.Close()
	relay, _ := NewRelay(s, RelayOptions{UpstreamURL: upstream.URL})
	w := requestRelay(t, relay, `{}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "unsupported parameter") || strings.Contains(w.Body.String(), tok.AccessToken) || strings.Contains(w.Body.String(), tok.RefreshToken) {
		t.Fatal("input error details are missing or unsafe")
	}
}

func TestServiceTierWireValues(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"fast", "priority"}, {"priority", "priority"}, {"default", "default"}, {"flex", "flex"}} {
		b, _, err := mapRequest([]byte(fmt.Sprintf(`{"service_tier":%q,"reasoning":{"effort":"low"}}`, tc.input)))
		var got struct {
			Tier      string `json:"service_tier"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if err != nil || json.Unmarshal(b, &got) != nil || got.Tier != tc.want || got.Reasoning.Effort != "low" {
			t.Fatalf("incorrect tier mapping for %s", tc.input)
		}
	}
}
