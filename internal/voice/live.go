package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// ErrUnavailable indicates disabled voice or unavailable credentials.
var ErrUnavailable = errors.New("voice: live service unavailable")

// ListenEvent carries transient transcription data; it must never be persisted.
type ListenEvent struct{ Type, Item, Text, Code string }

// ListenSession owns one upstream transcription socket and its bounded mailbox.
type ListenSession struct {
	conn        *websocket.Conn
	ctx         context.Context
	cancel      context.CancelFunc
	events      chan ListenEvent
	done        chan struct{}
	mu          sync.Mutex
	unavailable atomic.Bool
}

// Listen returns only after the upstream confirms the transcription configuration.
func (c *Client) Listen(ctx context.Context, language string) (*ListenSession, error) {
	return c.listen(ctx, language, 10*time.Second)
}

func (c *Client) listen(ctx context.Context, language string, startupTimeout time.Duration) (*ListenSession, error) {
	base, key, err := c.credentials(ctx)
	if err != nil {
		return nil, ErrUnavailable
	}
	conn, err := c.listenAttempt(ctx, base, key, language, startupTimeout)
	if errors.Is(err, ErrUnavailable) && c.FallbackCredentials != nil {
		if fbase, fkey, ferr := c.FallbackCredentials(ctx); ferr == nil && fkey != "" && fkey != key {
			conn, err = c.listenAttempt(ctx, strings.TrimRight(fbase, "/"), fkey, language, startupTimeout)
		}
	}
	if err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &ListenSession{conn: conn, ctx: sessionCtx, cancel: cancel, events: make(chan ListenEvent, 32), done: make(chan struct{})}
	safego.Go("voice-listen", nil, s.read)
	return s, nil
}

func (c *Client) liveHTTP(timeout time.Duration) *http.Client {
	client := &http.Client{}
	if c.HTTP != nil {
		*client = *c.HTTP
	}
	client.Timeout = timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

func (c *Client) listenAttempt(ctx context.Context, base, key, language string, timeout time.Duration) (*websocket.Conn, error) {
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, errors.New("voice: invalid live endpoint")
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/realtime"
	endpoint.RawPath = ""
	endpoint.RawQuery = "intent=transcription"
	endpoint.Fragment = ""
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	conn, resp, err := websocket.Dial(dialCtx, endpoint.String(), &websocket.DialOptions{HTTPClient: c.liveHTTP(0), HTTPHeader: http.Header{"Authorization": []string{"Bearer " + key}}})
	dialCancel()
	if err != nil {
		if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 402 || resp.StatusCode == 403 || resp.StatusCode == 429) {
			return nil, ErrUnavailable
		}
		return nil, errors.New("voice: live connection failed")
	}
	conn.SetReadLimit(1 << 20)
	success := false
	defer func() {
		if !success {
			_ = conn.CloseNow()
		}
	}()
	if len(language) != 2 || language[0] < 'a' || language[0] > 'z' || language[1] < 'a' || language[1] > 'z' {
		language = "es"
	}
	model := c.Config.LiveSTTModel
	if model == "" {
		model = "gpt-realtime-whisper"
	}
	update := map[string]any{"type": "session.update", "session": map[string]any{"type": "transcription", "audio": map[string]any{"input": map[string]any{"format": map[string]any{"type": "audio/pcm", "rate": 24000}, "transcription": map[string]any{"model": model, "language": language}, "turn_detection": nil}}}}
	raw, _ := json.Marshal(update)
	startupCtx, startupCancel := context.WithTimeout(ctx, timeout)
	defer startupCancel()
	if conn.Write(startupCtx, websocket.MessageText, raw) != nil {
		return nil, errors.New("voice: live configuration failed")
	}
	for {
		_, raw, err := conn.Read(startupCtx)
		if err != nil {
			return nil, errors.New("voice: live configuration failed")
		}
		var event upstreamListenEvent
		if json.Unmarshal(raw, &event) != nil {
			return nil, errors.New("voice: invalid live response")
		}
		switch event.Type {
		case "session.updated":
			success = true
			return conn, nil
		case "error":
			if event.unavailable() {
				return nil, ErrUnavailable
			}
			return nil, errors.New("voice: live configuration rejected")
		}
	}
}

type upstreamListenEvent struct {
	Type       string `json:"type"`
	Item       string `json:"item_id"`
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`
	Error      struct {
		Type string `json:"type"`
		Code string `json:"code"`
	} `json:"error"`
}

func (e upstreamListenEvent) unavailable() bool {
	if e.Error.Type == "insufficient_quota" {
		return true
	}
	switch e.Error.Code {
	case "insufficient_quota", "credit_balance_exhausted", "billing_hard_limit_reached", "invalid_api_key":
		return true
	}
	return false
}

func (s *ListenSession) emit(e ListenEvent) bool {
	select {
	case s.events <- e:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *ListenSession) read() {
	defer close(s.done)
	defer close(s.events)
	defer s.cancel()
	defer s.conn.CloseNow()
	for {
		_, raw, err := s.conn.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() == nil {
				s.emit(ListenEvent{Type: "error", Code: "failed"})
			}
			return
		}
		var e upstreamListenEvent
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		switch e.Type {
		case "conversation.item.input_audio_transcription.delta":
			if !s.emit(ListenEvent{Type: "delta", Item: e.Item, Text: e.Delta}) {
				return
			}
		case "conversation.item.input_audio_transcription.completed":
			if !s.emit(ListenEvent{Type: "final", Item: e.Item, Text: e.Transcript}) {
				return
			}
		case "error":
			if e.unavailable() {
				s.unavailable.Store(true)
				s.emit(ListenEvent{Type: "error", Code: "unavailable"})
				return
			}
			if e.Error.Code == "input_audio_buffer_commit_empty" {
				if !s.emit(ListenEvent{Type: "final"}) {
					return
				}
			}
		}
	}
}

func (s *ListenSession) send(value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable.Load() {
		return ErrUnavailable
	}
	raw, _ := json.Marshal(value)
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	if s.conn.Write(ctx, websocket.MessageText, raw) != nil {
		if s.unavailable.Load() {
			return ErrUnavailable
		}
		return errors.New("voice: live audio write failed")
	}
	return nil
}

// Append sends PCM16LE mono audio at 24 kHz.
func (s *ListenSession) Append(pcm []byte) error {
	return s.send(map[string]string{"type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm)})
}
func (s *ListenSession) Commit() error {
	return s.send(map[string]string{"type": "input_audio_buffer.commit"})
}
func (s *ListenSession) Clear() error {
	return s.send(map[string]string{"type": "input_audio_buffer.clear"})
}
func (s *ListenSession) Events() <-chan ListenEvent { return s.events }
func (s *ListenSession) Close() error               { s.cancel(); _ = s.conn.CloseNow(); <-s.done; return nil }

// SpeakStream delivers aligned PCM16LE mono 24 kHz chunks without buffering the body.
func (c *Client) SpeakStream(ctx context.Context, text string, write func([]byte) error) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > MaxTextCharacters {
		return invalid("voice: text must contain 1 to 4096 characters")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	base, key, err := c.credentials(ctx)
	if err != nil {
		return ErrUnavailable
	}
	liveVoice := c.Config.LiveVoice
	if liveVoice == "" {
		liveVoice = "marin"
	}
	body, _ := json.Marshal(map[string]string{"model": c.settings().TTSModel, "input": text, "voice": liveVoice, "response_format": "pcm", "instructions": "Speak naturally in the language of the text."})
	status, err := c.speechAttempt(ctx, base, key, body, write)
	if status == 401 || status == 402 || status == 429 {
		if c.FallbackCredentials != nil {
			if fbase, fkey, ferr := c.FallbackCredentials(ctx); ferr == nil && fkey != "" && fkey != key {
				_, err = c.speechAttempt(ctx, strings.TrimRight(fbase, "/"), fkey, body, write)
			}
		}
	}
	return err
}

func (c *Client) speechAttempt(ctx context.Context, base, key string, body []byte, write func([]byte) error) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return 0, errors.New("voice: invalid speech endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.liveHTTP(5 * time.Minute).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, errors.New("voice: speech request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 402 || resp.StatusCode == 429 {
		return resp.StatusCode, ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, errors.New("voice: speech request rejected")
	}
	buf := make([]byte, 4800)
	pending, total := 0, 0
	for {
		n, err := resp.Body.Read(buf[pending:])
		n += pending
		even := n &^ 1
		if even > 0 {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if werr := write(buf[:even]); werr != nil {
				return 0, werr
			}
			total += even
		}
		pending = n - even
		if pending != 0 {
			buf[0] = buf[even]
		}
		if err != nil {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			if err == io.EOF && pending == 0 && total > 0 {
				return 0, nil
			}
			return 0, errors.New("voice: speech stream failed")
		}
	}
}
