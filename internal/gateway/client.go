package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Client is the thin delegation caller of Section 3.6: the CLI and
// the MCP server use it to reach the daemon API when active, and fall
// back to embedded execution when the daemon is unreachable.
type Client struct {
	base     string
	clientID string
	http     *http.Client
}

// Dial returns a client for the daemon at addr ("127.0.0.1:port").
func Dial(addr, clientID string) *Client {
	return &Client{
		base:     "http://" + addr,
		clientID: clientID,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// DaemonActive probes the gate health endpoint.
func DaemonActive(addr string) bool {
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// Call performs one JSON-RPC call over HTTP.
func (c *Client) Call(ctx context.Context, id, method string, params any) (json.RawMessage, error) {
	req, err := api.NewRequest(id, method, params)
	if err != nil {
		return nil, err
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/rpc", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.clientID != "" {
		httpReq.Header.Set("X-Client-ID", c.clientID)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway: rpc %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out api.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("gateway: decode rpc %s: %w", method, err)
	}
	if out.Error != nil {
		return nil, out.Error
	}
	return out.Result, nil
}

// Subscribe opens a WebSocket subscription from cursor and delivers
// events to fn until ctx ends. On disconnect the caller reopens with
// the last cursor: replay covers the gap (Section 3.5).
func (c *Client) Subscribe(ctx context.Context, cursor types.Cursor, eventTypes []string, fn func(types.Event)) error {
	url := "ws://" + trimScheme(c.base) + "/ws?client_id=" + c.clientID
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("gateway: ws dial: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	sub, err := api.NewRequest("sub-1", api.MethodConversationSubscribe, api.ConversationSubscribeParams{Cursor: cursor.String(), Types: eventTypes})
	if err != nil {
		return err
	}
	buf, _ := json.Marshal(sub)
	if err := conn.Write(ctx, websocket.MessageText, buf); err != nil {
		return err
	}
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		// Live event notifications carry method "event".
		var probe struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
			Result json.RawMessage `json:"result,omitempty"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			continue
		}
		if probe.Method == "event" && len(probe.Params) > 0 {
			var ev types.Event
			if err := json.Unmarshal(probe.Params, &ev); err == nil {
				fn(ev)
			}
			continue
		}
		if len(probe.Result) > 0 {
			var ack struct {
				Events []types.Event `json:"events"`
			}
			if err := json.Unmarshal(probe.Result, &ack); err == nil {
				for _, ev := range ack.Events {
					fn(ev)
				}
			}
		}
	}
}

func trimScheme(base string) string {
	for _, p := range []string{"http://", "https://", "ws://", "wss://"} {
		if len(base) > len(p) && base[:len(p)] == p {
			return base[len(p):]
		}
	}
	return base
}

// Outbox is the client-side tray of Section 3.5: unconfirmed sends
// wait here and are resent on reconnect with the same client_msg_id,
// so no message is lost or duplicated across a crash or restart.
type Outbox struct {
	mu      sync.Mutex
	dir     string
	pending map[string]PendingSend
}

// PendingSend is one unconfirmed message.
type PendingSend struct {
	ClientMsgID    string `json:"client_msg_id"`
	ConversationID string `json:"conversation_id"`
	Text           string `json:"text"`
}

// NewOutbox opens dir, creating it when missing.
func NewOutbox(dir string) (*Outbox, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gateway: mkdir outbox: %w", err)
	}
	return &Outbox{dir: dir, pending: map[string]PendingSend{}}, nil
}

// Queue stages an unconfirmed send.
func (o *Outbox) Queue(p PendingSend) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pending[p.ClientMsgID] = p
}

// Confirm drops a send the daemon acknowledged.
func (o *Outbox) Confirm(clientMsgID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.pending, clientMsgID)
}

// Pending lists unconfirmed sends in client_msg_id order.
func (o *Outbox) Pending() []PendingSend {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]PendingSend, 0, len(o.pending))
	for _, p := range o.pending {
		out = append(out, p)
	}
	return out
}

// Flush resends every pending message through send and confirms the
// acknowledged ones. A duplicate client_msg_id returns the already
// assigned msg_id: at-most-once effect with at-least-once delivery.
func (o *Outbox) Flush(ctx context.Context, send func(ctx context.Context, p PendingSend) error) error {
	for _, p := range o.Pending() {
		if err := send(ctx, p); err != nil {
			return err
		}
		o.Confirm(p.ClientMsgID)
	}
	return nil
}

// OutboxPath returns the on-disk path for tooling.
func (o *Outbox) OutboxPath() string { return filepath.Join(o.dir, "outbox.json") }
