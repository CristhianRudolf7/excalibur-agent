package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/coder/websocket"
)

func TestEphemeralFramesRequireExplicitSubscription(t *testing.T) {
	s, addr := testServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connect := func(filter []string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws://"+strings.TrimPrefix(addr, "http://")+"/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		q, _ := api.NewRequest("subscribe", api.MethodConversationSubscribe, api.ConversationSubscribeParams{Types: filter})
		b, _ := json.Marshal(q)
		if err = conn.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
		if _, _, err = conn.Read(ctx); err != nil {
			t.Fatal(err)
		}
		// The second request is a barrier: the subscription tail is now attached.
		q, _ = api.NewRequest("barrier", api.MethodConversationHistory, api.ConversationHistoryParams{})
		b, _ = json.Marshal(q)
		if err = conn.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
		if _, _, err = conn.Read(ctx); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	frames := connect([]string{"robotlab.frame"})
	all := connect(nil)
	// Frames are deliberately lossy: an RPC writer may still hold its lock
	// after the client reads the barrier response. Stream at the live cadence
	// until a frame arrives instead of requiring that particular frame to win.
	frameCtx, stopFrames := context.WithCancel(ctx)
	defer stopFrames()
	framesStopped := make(chan struct{})
	safego.Go("gateway-test-frames", nil, func() {
		defer close(framesStopped)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			_ = s.bus.PublishEphemeral("robotlab.frame", map[string]any{"session": "s"})
			select {
			case <-frameCtx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	_, raw, err := frames.Read(ctx)
	stopFrames()
	<-framesStopped
	if err != nil {
		t.Fatal(err)
	}
	var push struct {
		Method string      `json:"method"`
		Params types.Event `json:"params"`
	}
	if err = json.Unmarshal(raw, &push); err != nil || push.Params.Type != "robotlab.frame" {
		t.Fatalf("frame: %s %v", raw, err)
	}
	_, err = s.bus.Publish("robotlab.focus", nil, nil, map[string]any{"session": "s", "robot": "dog", "environment": "room"})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err = all.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &push); err != nil || push.Params.Type != "robotlab.focus" {
		t.Fatalf("unrequested frame forwarded: %s %v", raw, err)
	}
	if s.BusLen() != 1 {
		t.Fatal("frame retained")
	}
}
func TestEphemeralBusyWriterDropsWithoutQueue(t *testing.T) {
	peer := &wsPeer{}
	peer.mu.Lock()
	defer peer.mu.Unlock()
	// A busy writer must return without touching the (deliberately nil) socket.
	for i := 0; i < 1000; i++ {
		peer.tryWriteEphemeral(context.Background(), []byte(`{}`))
	}
}
