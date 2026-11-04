package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/coder/websocket"
)

func TestWebSocketSubscriptionReadyWhenAcknowledged(t *testing.T) {
	// Reconnect repeatedly and publish immediately after the ACK, without the
	// extra RPC barrier that would conceal a registration-after-ACK race.
	for i := 0; i < 32; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			bus := telemetry.NewBus(nil)
			server := httptest.NewServer(New(DefaultConfig(), nil, nil, bus))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			if err := conn.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":"subscribe","method":"conversation.subscribe","params":{}}`)); err != nil {
				t.Fatal(err)
			}
			_, raw, err := conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var ack api.Response
			if err := json.Unmarshal(raw, &ack); err != nil || ack.Error != nil || string(ack.ID) != `"subscribe"` {
				t.Fatalf("expected subscription ACK first: %s, %v", raw, err)
			}
			event, err := bus.Publish("robotlab.focus", nil, nil, map[string]string{"session": "s"})
			if err != nil {
				t.Fatal(err)
			}
			_, raw, err = conn.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var push struct {
				Method string      `json:"method"`
				Params types.Event `json:"params"`
			}
			if err := json.Unmarshal(raw, &push); err != nil || push.Method != "event" || push.Params.Seq != event.Seq || push.Params.Type != event.Type {
				t.Fatalf("missing immediate live event: %s, %v", raw, err)
			}
		})
	}
}
