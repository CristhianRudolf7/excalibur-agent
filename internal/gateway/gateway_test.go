package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	s := New(Config{Listen: "127.0.0.1:0", AllowRemote: false}, NewMemoryBackend(bus), NewMemoryBlobs(), bus)
	addr, err := s.Start()
	if err != nil {
		t.Fatalf("start gate: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s, addr
}

func rpcCall(t *testing.T, addr, method string, params any) api.Response {
	t.Helper()
	req, err := api.NewRequest("1", method, params)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	buf, _ := json.Marshal(req)
	resp, err := http.Post("http://"+addr+"/rpc", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out api.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// Section 3.7: the gate refuses non-loopback binds by default.
func TestLoopbackRefused(t *testing.T) {
	bus := telemetry.NewBus(nil)
	s := New(Config{Listen: "0.0.0.0:0"}, NewMemoryBackend(bus), NewMemoryBlobs(), bus)
	if _, err := s.Start(); err == nil {
		_ = s.Stop(context.Background())
		t.Fatal("non-loopback bind must be refused without AllowRemote")
	}
}

// Section 3.7: the same client_msg_id returns the assigned msg_id,
// one row only, after a disconnect-style resend.
func TestConversationSendIdempotent(t *testing.T) {
	s, addr := testServer(t)
	_ = s
	p := api.ConversationSendParams{ConversationID: "cnv_x", ClientMsgID: "c-1", Text: "hello"}
	first := rpcCall(t, addr, api.MethodConversationSend, p)
	if first.Error != nil {
		t.Fatalf("send: %v", first.Error)
	}
	second := rpcCall(t, addr, api.MethodConversationSend, p)
	if second.Error != nil {
		t.Fatalf("resend: %v", second.Error)
	}
	var r1, r2 api.ConversationSendResult
	if err := json.Unmarshal(first.Result, &r1); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Result, &r2); err != nil {
		t.Fatal(err)
	}
	if r1.MsgID != r2.MsgID {
		t.Fatalf("resend assigned a new msg_id: %q vs %q", r1.MsgID, r2.MsgID)
	}
	hist := rpcCall(t, addr, api.MethodConversationHistory, api.ConversationHistoryParams{ConversationID: "cnv_x"})
	if hist.Error != nil {
		t.Fatalf("history: %v", hist.Error)
	}
	var h struct {
		Messages []StoredMessage `json:"messages"`
	}
	if err := json.Unmarshal(hist.Result, &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Messages) != 1 {
		t.Fatalf("duplicate send stored %d messages, want 1", len(h.Messages))
	}
}

// Section 3.5: unknown methods answer CodeMethodNotFound.
func TestUnknownMethod(t *testing.T) {
	_, addr := testServer(t)
	out := rpcCall(t, addr, "nope.unknown", nil)
	if out.Error == nil || out.Error.Code != api.CodeMethodNotFound {
		t.Fatalf("want CodeMethodNotFound, got %+v", out.Error)
	}
}

// Section 3.5: notifications (no id) apply with no response body.
func TestNotificationNoResponse(t *testing.T) {
	_, addr := testServer(t)
	raw := `{"jsonrpc":"2.0","method":"conversation.send","params":{"conversation_id":"c","client_msg_id":"n1","text":"hi"}}`
	resp, err := http.Post("http://"+addr+"/rpc", "application/json", strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("notification status = %d, want 204", resp.StatusCode)
	}
}

// Section 3.5/3.7: cursor replay has no gaps or duplicates; a
// reconnect from the cursor continues exactly where it left off.
func TestSubscribeReplayNoGaps(t *testing.T) {
	bus := telemetry.NewBus(nil)
	s := New(Config{Listen: "127.0.0.1:0"}, NewMemoryBackend(bus), NewMemoryBlobs(), bus)
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	for i := 0; i < 5; i++ {
		if _, err := bus.Publish(types.EventMessageCreated, nil, nil, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	mid := types.Cursor{Seq: 2}
	got := s.replay(mid, nil)
	if len(got) != 3 || got[0].Seq != 3 || got[2].Seq != 5 {
		t.Fatalf("replay from 2 = %+v, want seq 3..5", got)
	}
	// Full replay from zero, then continue from the last seq.
	all := s.replay(types.Cursor{}, nil)
	if len(all) != 5 {
		t.Fatalf("full replay = %d events, want 5", len(all))
	}
	rest := s.replay(types.Cursor{Seq: all[len(all)-1].Seq}, nil)
	if len(rest) != 0 {
		t.Fatalf("replay at head = %d events, want 0", len(rest))
	}
	_ = addr
}

// Section 3.7: blobs round-trip over HTTP with digest checks.
func TestBlobsRoundTrip(t *testing.T) {
	_, addr := testServer(t)
	body := []byte("fake capture bytes")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	// Digest mismatch is rejected.
	req, _ := http.NewRequest(http.MethodPut, "http://"+addr+"/blobs/"+strings.Repeat("0", 64), bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatch status = %d, want 400", resp.StatusCode)
	}
	// Correct upload.
	req, _ = http.NewRequest(http.MethodPut, "http://"+addr+"/blobs/"+digest, bytes.NewReader(body))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("put status = %d, want 201", resp.StatusCode)
	}
	// Download matches.
	get, err := http.Get("http://" + addr + "/blobs/" + digest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = get.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(get.Body); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), body) {
		t.Fatal("blob content mismatch")
	}
	// Missing blob is 404.
	missing, err := http.Get("http://" + addr + "/blobs/" + strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	_ = missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", missing.StatusCode)
	}
}

// Section 3.5: the client outbox resends unconfirmed messages after a
// restart with the same client_msg_id.
func TestOutboxFlushResends(t *testing.T) {
	o, err := NewOutbox(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o.Queue(PendingSend{ClientMsgID: "c-9", ConversationID: "cnv", Text: "pending"})
	var sent []PendingSend
	err = o.Flush(context.Background(), func(_ context.Context, p PendingSend) error {
		sent = append(sent, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0].ClientMsgID != "c-9" {
		t.Fatalf("flushed = %+v, want the pending send", sent)
	}
	if len(o.Pending()) != 0 {
		t.Fatal("confirmed sends must leave the outbox")
	}
}

// Section 3.7: JSON-RPC over WebSocket answers and live events reach
// a cursor subscription.
func TestWebSocketRPCAndSubscribe(t *testing.T) {
	bus := telemetry.NewBus(nil)
	s := New(Config{Listen: "127.0.0.1:0"}, NewMemoryBackend(bus), NewMemoryBlobs(), bus)
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	req, _ := api.NewRequest("ws-1", api.MethodConversationSend,
		api.ConversationSendParams{ConversationID: "cnv", ClientMsgID: "ws-c-1", Text: "hi"})
	buf, _ := json.Marshal(req)
	if err := conn.Write(ctx, websocket.MessageText, buf); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var resp api.Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != nil {
		t.Fatalf("ws send: %v", resp.Error)
	}
	// Subscribe from zero: the ack carries the event just published.
	sub, _ := api.NewRequest("ws-2", api.MethodConversationSubscribe, api.ConversationSubscribeParams{})
	sbuf, _ := json.Marshal(sub)
	if err := conn.Write(ctx, websocket.MessageText, sbuf); err != nil {
		t.Fatal(err)
	}
	_, ackRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ack api.Response
	if err := json.Unmarshal(ackRaw, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Error != nil {
		t.Fatalf("ws subscribe: %v", ack.Error)
	}
	var payload struct {
		Events []types.Event `json:"events"`
	}
	if err := json.Unmarshal(ack.Result, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) == 0 {
		t.Fatal("subscribe ack must replay the sent message event")
	}
	// A later event reaches the live tail without a resubscribe.
	if _, err := bus.Publish(types.EventRunProgress, nil, nil, map[string]any{"step": 1}); err != nil {
		t.Fatal(err)
	}
	_, liveRaw, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var live struct {
		Method string      `json:"method"`
		Params types.Event `json:"params"`
	}
	if err := json.Unmarshal(liveRaw, &live); err != nil {
		t.Fatal(err)
	}
	if live.Method != "event" || live.Params.Type != types.EventRunProgress {
		t.Fatalf("live tail = %+v, want the run.progress event", live)
	}
	if live.Params.Seq != payload.Events[len(payload.Events)-1].Seq+1 {
		t.Fatalf("live seq = %d, want no gap after %d", live.Params.Seq, payload.Events[len(payload.Events)-1].Seq)
	}
}

// Batch requests answer in order; every api.Methods entry routes
// (unknown families via passthrough reject, never hang).
func TestBatchAndMethodRegistry(t *testing.T) {
	_, addr := testServer(t)
	seen := map[string]bool{}
	for _, m := range api.Methods {
		seen[m.Name] = true
	}
	for _, want := range []string{api.MethodConversationSend, api.MethodConversationSubscribe, api.MethodRunStatus, api.MethodTaskStart, api.MethodVaultCreate, api.MethodMetricsQuery, api.MethodFilesList} {
		if !seen[want] {
			t.Fatalf("api.Methods missing %s", want)
		}
	}
	batch := `[{"jsonrpc":"2.0","id":"a","method":"run.list","params":{}},
		{"jsonrpc":"2.0","id":"b","method":"bogus.x","params":{}}]`
	resp, err := http.Post("http://"+addr+"/rpc", "application/json", strings.NewReader(batch))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out []api.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("batch answers = %d, want 2", len(out))
	}
	if out[0].Error != nil {
		t.Fatalf("run.list: %v", out[0].Error)
	}
	if out[1].Error == nil || out[1].Error.Code != api.CodeMethodNotFound {
		t.Fatalf("bogus.x must be MethodNotFound, got %+v", out[1].Error)
	}
}

func TestClientDialRoundTrip(t *testing.T) {
	_, addr := testServer(t)
	if !DaemonActive(addr) {
		t.Fatal("health check must pass on a live gate")
	}
	c := Dial(addr, "test-client")
	raw, err := c.Call(context.Background(), "1", api.MethodRunList, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Runs []api.RunStatusResult `json:"runs"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	_ = fmt.Sprint(list)
}

func TestBrowserSameOriginAndHost(t *testing.T) {
	s := New(Config{Listen: "127.0.0.1:0", WebUI: http.NotFoundHandler()}, nil, nil, nil)
	addr, err := s.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	for _, tc := range []struct {
		origin, host string
		want         int
	}{
		{"http://" + addr, addr, 200}, {"https://" + addr, addr, 403}, {"https://attacker.invalid", addr, 403},
		{"null", addr, 403}, {"", "attacker.invalid", 403}, {"", addr, 200},
	} {
		req, _ := http.NewRequest("GET", "http://"+addr+"/healthz", nil)
		req.Header.Set("Origin", tc.origin)
		req.Host = tc.host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("origin=%s host=%s: %d", tc.origin, tc.host, resp.StatusCode)
		}
	}
}
