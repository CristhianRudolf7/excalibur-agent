package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// BrowserFrame is an in-memory image delivered only to a browser viewer.
type BrowserFrame struct {
	Seq           uint64
	Tab           string
	JPEG          []byte
	Width, Height int
	AtMillis      int64
}

// BrowserLiveBackend optionally adds watch-only browser streaming to a backend.
type BrowserLiveBackend interface {
	WatchBrowser(conversationID, sessionID string, fn func(BrowserFrame)) (stop func(), done <-chan struct{}, err error)
}

type browserLiveHandler struct {
	server  *Server
	mu      sync.Mutex
	viewers map[string]int
}

func newBrowserLiveHandler(s *Server) *browserLiveHandler {
	return &browserLiveHandler{server: s, viewers: make(map[string]int)}
}

func (h *browserLiveHandler) acquire(session string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.viewers[session] >= 4 {
		return false
	}
	h.viewers[session]++
	return true
}

func (h *browserLiveHandler) release(session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.viewers[session]--
	if h.viewers[session] == 0 {
		delete(h.viewers, session)
	}
}

// browserFrameMailbox bounds queued video to one frame, replacing stale frames.
// The producer lock also makes concurrent engine callbacks safe.
type browserFrameMailbox struct {
	mu sync.Mutex
	ch chan BrowserFrame
}

func newBrowserFrameMailbox() *browserFrameMailbox {
	return &browserFrameMailbox{ch: make(chan BrowserFrame, 1)}
}

func (m *browserFrameMailbox) offer(f BrowserFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.ch:
	default:
	}
	m.ch <- f
}

func encodeBrowserFrame(f BrowserFrame) []byte {
	header, _ := json.Marshal(struct {
		Seq    uint64 `json:"seq"`
		Tab    string `json:"tab_id"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		At     int64  `json:"at"`
	}{f.Seq, f.Tab, f.Width, f.Height, f.AtMillis})
	buf := make([]byte, 4+len(header)+len(f.JPEG))
	binary.BigEndian.PutUint32(buf, uint32(len(header)))
	copy(buf[4:], header)
	copy(buf[4+len(header):], f.JPEG)
	return buf
}

func (h *browserLiveHandler) serveBrowserLive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	backend, ok := h.server.backend.(BrowserLiveBackend)
	conversation, session := r.URL.Query().Get("conversation_id"), r.URL.Query().Get("session_id")
	if !ok || conversation == "" || session == "" {
		http.NotFound(w, r)
		return
	}
	if !h.acquire(session) {
		http.Error(w, "too many browser viewers", http.StatusTooManyRequests)
		return
	}
	defer h.release(session)
	mailbox := newBrowserFrameMailbox()
	stop, done, err := backend.WatchBrowser(conversation, session, mailbox.offer)
	if err != nil {
		if stop != nil {
			stop()
		}
		http.NotFound(w, r)
		return
	}
	defer stop()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1024)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	writerDone := make(chan struct{})
	safego.Go("gateway-browser-live", nil, func() {
		defer close(writerDone)
		defer conn.CloseNow()
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.server.done:
				return
			case <-done:
				_ = conn.Close(websocket.StatusNormalClosure, "ended")
				return
			case frame := <-mailbox.ch:
				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, websocket.MessageBinary, encodeBrowserFrame(frame))
				writeCancel()
				if err != nil {
					return
				}
			}
		}
	})
	// Reading detects disconnects promptly and handles control frames. This
	// watch-only endpoint deliberately discards all client application messages.
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	cancel()
	_ = conn.CloseNow()
	<-writerDone
}
