package gateway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// ErrVoiceUnavailable requests the client's local voice fallback.
var ErrVoiceUnavailable = errors.New("voice unavailable")

// VoiceListenSession is an ephemeral transcription session.
type VoiceListenSession interface {
	Append([]byte) error
	Commit() error
	Clear() error
	Events() <-chan VoiceListenEvent
	Close() error
}

type VoiceListenEvent struct{ Type, Item, Text, Code string }

// VoiceLiveBackend optionally adds streaming voice independently of the event bus.
type VoiceLiveBackend interface {
	VoiceListen(context.Context, string) (VoiceListenSession, error)
	VoiceSpeak(context.Context, string, func([]byte) error) error
}

type voiceLiveHandler struct {
	server *Server
	mu     sync.Mutex
	active int
}

func newVoiceLiveHandler(s *Server) *voiceLiveHandler { return &voiceLiveHandler{server: s} }

func (h *voiceLiveHandler) accept(w http.ResponseWriter, r *http.Request) (*voiceSocket, VoiceLiveBackend) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, nil
	}
	backend, ok := h.server.backend.(VoiceLiveBackend)
	if !ok {
		http.NotFound(w, r)
		return nil, nil
	}
	h.mu.Lock()
	if h.active >= 2 {
		h.mu.Unlock()
		http.Error(w, "too many voice sessions", http.StatusTooManyRequests)
		return nil, nil
	}
	h.active++
	h.mu.Unlock()
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.release()
		return nil, nil
	}
	conn.SetReadLimit(64 << 10)
	return newVoiceSocket(r.Context(), h.server.done, conn), backend
}

func (h *voiceLiveHandler) release() { h.mu.Lock(); h.active--; h.mu.Unlock() }

type voiceMessage struct {
	typ  websocket.MessageType
	data []byte
}
type voicePacket struct {
	voiceMessage
	job   *voiceSynthesis
	close bool
	ack   chan error
}

// voiceSocket has exactly one writer. Reads also run during upstream startup so
// disconnect and daemon shutdown cancel a pending handshake immediately.
type voiceSocket struct {
	ctx                    context.Context
	cancel                 context.CancelFunc
	conn                   *websocket.Conn
	incoming               chan voiceMessage
	outgoing               chan voicePacket
	readerDone, writerDone chan struct{}
}

func newVoiceSocket(parent context.Context, serverDone <-chan struct{}, conn *websocket.Conn) *voiceSocket {
	ctx, cancel := context.WithCancel(parent)
	s := &voiceSocket{ctx: ctx, cancel: cancel, conn: conn, incoming: make(chan voiceMessage, 1), outgoing: make(chan voicePacket, 8), readerDone: make(chan struct{}), writerDone: make(chan struct{})}
	safego.Go("gateway-voice-reader", nil, func() {
		defer close(s.readerDone)
		defer cancel()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			select {
			case s.incoming <- voiceMessage{typ, data}:
			case <-ctx.Done():
				return
			}
		}
	})
	safego.Go("gateway-voice-writer", nil, func() {
		defer close(s.writerDone)
		defer cancel()
		defer conn.CloseNow()
		for {
			select {
			case <-ctx.Done():
				return
			case <-serverDone:
				return
			case p := <-s.outgoing:
				if p.job != nil {
					p.job.mu.Lock()
					if p.job.ctx.Err() != nil {
						p.job.mu.Unlock()
						p.ack <- context.Canceled
						continue
					}
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
				err := conn.Write(writeCtx, p.typ, p.data)
				writeCancel()
				if p.job != nil {
					p.job.mu.Unlock()
				}
				if p.close && err == nil {
					err = conn.Close(websocket.StatusNormalClosure, "")
				}
				p.ack <- err
				if err != nil || p.close {
					return
				}
			}
		}
	})
	return s
}

func (s *voiceSocket) shutdown() {
	s.cancel()
	_ = s.conn.CloseNow()
	<-s.readerDone
	<-s.writerDone
}

func (s *voiceSocket) send(ctx context.Context, p voicePacket) error {
	p.ack = make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	case s.outgoing <- p:
	}
	select {
	case err := <-p.ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

func (s *voiceSocket) text(value any, end bool) error {
	raw, _ := json.Marshal(value)
	return s.send(s.ctx, voicePacket{voiceMessage: voiceMessage{websocket.MessageText, raw}, close: end})
}

func voiceErrorCode(err error) string {
	if errors.Is(err, ErrVoiceUnavailable) {
		return "unavailable"
	}
	return "failed"
}

func (h *voiceLiveHandler) serveListen(w http.ResponseWriter, r *http.Request) {
	socket, backend := h.accept(w, r)
	if socket == nil {
		return
	}
	defer h.release()
	defer socket.shutdown()
	language := r.URL.Query().Get("language")
	if len(language) != 2 || language[0] < 'a' || language[0] > 'z' || language[1] < 'a' || language[1] > 'z' {
		language = "es"
	}
	fail := func(code string) { _ = socket.text(map[string]string{"type": "error", "code": code}, true) }
	session, err := backend.VoiceListen(socket.ctx, language)
	if err != nil {
		fail(voiceErrorCode(err))
		return
	}
	defer session.Close()
	if socket.text(map[string]string{"type": "ready"}, false) != nil {
		return
	}
	for {
		select {
		case <-socket.ctx.Done():
			return
		case e, ok := <-session.Events():
			if !ok {
				fail("failed")
				return
			}
			switch e.Type {
			case "delta", "final":
				if socket.text(map[string]string{"type": e.Type, "item": e.Item, "text": e.Text}, false) != nil {
					return
				}
			case "error", "closed":
				code := "failed"
				if e.Code == "unavailable" {
					code = "unavailable"
				}
				fail(code)
				return
			}
		case message := <-socket.incoming:
			switch message.typ {
			case websocket.MessageBinary:
				err = session.Append(message.data)
			case websocket.MessageText:
				var command struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(message.data, &command) != nil {
					fail("failed")
					return
				}
				switch command.Type {
				case "commit":
					err = session.Commit()
				case "clear":
					err = session.Clear()
				default:
					fail("failed")
					return
				}
			default:
				fail("failed")
				return
			}
			if err != nil {
				fail(voiceErrorCode(err))
				return
			}
		}
	}
}

type voiceSynthesis struct {
	ctx    context.Context
	cancel context.CancelFunc
	// The writer holds mu while sending PCM. Cancellation takes the same lock,
	// ensuring all writes for this synthesis have stopped before it returns.
	mu   sync.Mutex
	done chan struct{}
	id   uint32
}

func (j *voiceSynthesis) stop() {
	j.mu.Lock()
	j.cancel()
	j.mu.Unlock()
	<-j.done
}

type voiceSpeakReply struct {
	Type string `json:"type"`
	ID   uint32 `json:"id"`
	Code string `json:"code,omitempty"`
}

func (h *voiceLiveHandler) serveSpeak(w http.ResponseWriter, r *http.Request) {
	socket, backend := h.accept(w, r)
	if socket == nil {
		return
	}
	defer h.release()
	defer socket.shutdown()
	var current *voiceSynthesis
	defer func() {
		if current != nil {
			current.stop()
		}
	}()
	for {
		select {
		case <-socket.ctx.Done():
			return
		case message := <-socket.incoming:
			var command struct {
				Type string `json:"type"`
				ID   uint32 `json:"id"`
				Text string `json:"text"`
			}
			parseErr := json.Unmarshal(message.data, &command)
			invalid := message.typ != websocket.MessageText || parseErr != nil || command.ID == 0
			if command.Type == "speak" {
				invalid = invalid || !utf8.ValidString(command.Text) || strings.TrimSpace(command.Text) == "" || utf8.RuneCountInString(command.Text) > 4096
			}
			if command.Type != "speak" && command.Type != "cancel" {
				invalid = true
			}
			if invalid {
				if socket.text(voiceSpeakReply{Type: "error", ID: command.ID, Code: "failed"}, false) != nil {
					return
				}
				continue
			}
			if command.Type == "cancel" {
				if current != nil && current.id == command.ID {
					current.stop()
					current = nil
				}
				continue
			}
			if current != nil {
				current.stop()
			}
			ctx, cancel := context.WithCancel(socket.ctx)
			job := &voiceSynthesis{ctx: ctx, cancel: cancel, done: make(chan struct{}), id: command.ID}
			current = job
			safego.Go("gateway-voice-synthesis", nil, func() {
				defer close(job.done)
				defer cancel()
				err := backend.VoiceSpeak(ctx, command.Text, func(pcm []byte) error {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					frame := make([]byte, 4+len(pcm))
					binary.BigEndian.PutUint32(frame, job.id)
					copy(frame[4:], pcm)
					return socket.send(ctx, voicePacket{voiceMessage: voiceMessage{websocket.MessageBinary, frame}, job: job})
				})
				reply := voiceSpeakReply{Type: "done", ID: job.id}
				if err != nil && ctx.Err() == nil {
					reply.Type = "error"
					reply.Code = voiceErrorCode(err)
				}
				_ = socket.text(reply, false)
			})
		}
	}
}
