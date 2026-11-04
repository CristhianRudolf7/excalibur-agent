package codexlink

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

const responsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"
const maxBody = 32 << 20

// RelayOptions provides transport seams for simulated upstreams.
type RelayOptions struct {
	UpstreamURL string
	TokenURL    string
	HTTPClient  *http.Client
}

// Relay accepts authenticated loopback Responses requests.
type Relay struct {
	store                  *Store
	client                 *http.Client
	upstream, key, session string
}

func (r *Relay) String() string   { return "[local account relay]" }
func (r *Relay) GoString() string { return r.String() }
func (r *Relay) Key() string      { return r.key }

func NewRelay(store *Store, opts RelayOptions) (*Relay, error) {
	if store == nil {
		return nil, errors.New("sign-in store is required")
	}
	entropy := make([]byte, 48)
	if _, err := rand.Read(entropy); err != nil {
		return nil, errors.New("local relay key unavailable")
	}
	upstream := opts.UpstreamURL
	if upstream == "" {
		upstream = responsesEndpoint
	}
	client := accountClient(opts.HTTPClient)
	store.mu.Lock()
	store.client = client
	if opts.TokenURL != "" {
		store.tokenURL = opts.TokenURL
	}
	store.mu.Unlock()
	return &Relay{store: store, client: client, upstream: upstream, key: hex.EncodeToString(entropy[:32]), session: fmt.Sprintf("%x-%x-%x-%x-%x", entropy[32:36], entropy[36:38], entropy[38:40], entropy[40:42], entropy[42:])}, nil
}

// Server owns the daemon's private listener and shuts down with its context.
type Server struct {
	relay   *Relay
	server  *http.Server
	baseURL string
}

func (s *Server) BaseURL() string  { return s.baseURL }
func (s *Server) Key() string      { return s.relay.Key() }
func (s *Server) String() string   { return "[local relay server]" }
func (s *Server) GoString() string { return s.String() }
func (s *Server) Close() error     { return s.server.Close() }

func Listen(ctx context.Context, store *Store, opts RelayOptions) (*Server, error) {
	relay, err := NewRelay(store, opts)
	if err != nil {
		return nil, err
	}
	// Use the worker port range, reserving the shared daemon's port.
	var listener net.Listener
	seed := make([]byte, 1)
	if _, err := rand.Read(seed); err != nil {
		return nil, errors.New("local relay listener unavailable")
	}
	for i := range 100 {
		port := 7800 + (int(seed[0])+i)%100
		if port == 7841 {
			continue
		}
		listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			break
		}
	}
	if listener == nil {
		return nil, errors.New("local relay listener unavailable")
	}
	server := &http.Server{Handler: relay, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	s := &Server{relay: relay, server: server, baseURL: "http://" + listener.Addr().String() + "/v1"}
	done := make(chan struct{})
	safego.Go("account-relay", nil, func() { defer close(done); _ = server.Serve(listener) })
	safego.Go("account-relay-lifetime", nil, func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-done:
		}
	})
	return s, nil
}

func responseError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": message, "type": "relay_error"}})
}
func (r *Relay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	ip := net.ParseIP(host)
	expected := "Bearer " + r.key
	if err != nil || ip == nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte(expected)) != 1 {
		responseError(w, http.StatusUnauthorized, "local relay authentication required")
		return
	}
	if req.URL.Path != "/v1/responses" && req.URL.Path != "/responses" {
		responseError(w, 404, "relay route unavailable")
		return
	}
	if req.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		responseError(w, 405, "POST required")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxBody))
	if err != nil {
		responseError(w, 413, "request body exceeds relay limit")
		return
	}
	payload, stream, err := mapRequest(body)
	if err != nil {
		responseError(w, 400, err.Error())
		return
	}
	tokens, err := r.store.credentials(req.Context(), "")
	if err != nil {
		responseError(w, 401, err.Error())
		return
	}
	redact := redactor(tokens, r.key)
	resp, err := r.call(req.Context(), payload, tokens)
	if err != nil {
		responseError(w, 502, "account service unavailable")
		return
	}
	if resp.StatusCode == 401 {
		_ = resp.Body.Close()
		tokens, err = r.store.credentials(req.Context(), tokens.AccessToken)
		if err != nil {
			responseError(w, 401, err.Error())
			return
		}
		previousRedactor := redact
		currentRedactor := redactor(tokens, r.key)
		redact = func(b []byte) []byte { return previousRedactor(currentRedactor(b)) }
		resp, err = r.call(req.Context(), payload, tokens)
		if err != nil {
			responseError(w, 502, "account service unavailable")
			return
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		status := resp.StatusCode
		if status == 429 || quotaError(b) {
			status = 429
		}
		msg := "account service rejected the request"
		if status == 400 {
			var detail struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Detail string `json:"detail"`
			}
			if json.Unmarshal(b, &detail) == nil {
				text := detail.Error.Message
				if text == "" {
					text = detail.Detail
				}
				if text != "" && len(text) <= 1024 {
					msg = "account request rejected: " + string(redact([]byte(text)))
				}
			}
		}
		if status == 401 {
			msg = "account sign-in rejected; sign in again"
		}
		if status == 429 {
			msg = limitMessage(b, resp.Header)
		}
		// Preserve only a numeric retry interval; upstream headers may contain credentials.
		if delay, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && delay >= 0 {
			w.Header().Set("Retry-After", strconv.Itoa(delay))
		}
		responseError(w, status, msg)
		return
	}
	if stream {
		r.passthrough(w, resp.Body, redact)
		return
	}
	result, err := aggregate(resp.Body)
	if err != nil {
		var limit *usageLimit
		if errors.As(err, &limit) {
			responseError(w, 429, limit.message)
		} else {
			responseError(w, 502, "account response stream ended without a valid response")
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(redact(result))
}

func (r *Relay) call(ctx context.Context, payload []byte, tokens Tokens) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.upstream, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("account request unavailable")
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("chatgpt-account-id", tokens.AccountID)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", r.session)
	req.Header.Set("session-id", r.session)
	req.Header.Set("User-Agent", "codex_cli_rs/0.166.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	return r.client.Do(req)
}

func mapRequest(body []byte) ([]byte, bool, error) {
	var src map[string]json.RawMessage
	if json.Unmarshal(body, &src) != nil || src == nil {
		return nil, false, errors.New("invalid Responses request")
	}
	var stream bool
	if b, ok := src["stream"]; ok && json.Unmarshal(b, &stream) != nil {
		return nil, false, errors.New("stream must be a boolean")
	}
	var instructions string
	if b, ok := src["instructions"]; ok && json.Unmarshal(b, &instructions) != nil {
		return nil, false, errors.New("instructions must be text")
	}
	var input []json.RawMessage
	if b, ok := src["input"]; ok {
		if json.Unmarshal(b, &input) != nil {
			var text string
			if json.Unmarshal(b, &text) != nil {
				return nil, false, errors.New("input must be text or an item list")
			}
			item, _ := json.Marshal(map[string]string{"role": "user", "content": text})
			input = []json.RawMessage{item}
		}
	}
	kept := make([]json.RawMessage, 0, len(input))
	for _, item := range input {
		var msg struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(item, &msg) != nil {
			return nil, false, errors.New("invalid input item")
		}
		if msg.Role != "system" && msg.Role != "developer" {
			kept = append(kept, item)
			continue
		}
		var text string
		if json.Unmarshal(msg.Content, &text) != nil {
			var parts []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(msg.Content, &parts) != nil {
				return nil, false, errors.New("instruction input must be text")
			}
			var texts []string
			for _, part := range parts {
				if part.Type != "input_text" && part.Type != "text" {
					return nil, false, errors.New("instruction input must be text")
				}
				texts = append(texts, part.Text)
			}
			text = strings.Join(texts, "\n")
		}
		if instructions != "" && text != "" {
			instructions += "\n\n"
		}
		instructions += text
	}
	// Explicit allowlist excludes sampling, output limits and other rejected fields.
	out := map[string]json.RawMessage{}
	for _, key := range []string{"model", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "service_tier", "text", "prompt_cache_key", "include"} {
		if value, ok := src[key]; ok {
			out[key] = value
		}
	}
	// The CLI's fast mode uses the upstream priority wire value.
	var tier string
	if json.Unmarshal(out["service_tier"], &tier) == nil && tier == "fast" {
		out["service_tier"] = json.RawMessage(`"priority"`)
	}
	out["instructions"], _ = json.Marshal(instructions)
	out["input"], _ = json.Marshal(kept)
	out["store"] = json.RawMessage(`false`)
	out["stream"] = json.RawMessage(`true`)
	encoded, err := json.Marshal(out)
	return encoded, stream, err
}

func redactor(t Tokens, key string) func([]byte) []byte {
	secrets := []string{t.AccessToken, t.RefreshToken, t.IDToken, key}
	return func(b []byte) []byte {
		for _, s := range secrets {
			if s != "" {
				b = bytes.ReplaceAll(b, []byte(s), []byte("[redacted]"))
				encoded, _ := json.Marshal(s)
				if len(encoded) > 2 {
					b = bytes.ReplaceAll(b, encoded[1:len(encoded)-1], []byte("[redacted]"))
				}
			}
		}
		return b
	}
}

func quotaError(b []byte) bool {
	var obj any
	if json.Unmarshal(b, &obj) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch value := v.(type) {
		case map[string]any:
			for k, field := range value {
				if k == "code" || k == "type" {
					s, _ := field.(string)
					switch s {
					case "usage_limit_reached", "usage_limit", "rate_limit_exceeded", "insufficient_quota":
						return true
					}
				}
				if walk(field) {
					return true
				}
			}
		case []any:
			for _, field := range value {
				if walk(field) {
					return true
				}
			}
		}
		return false
	}
	return walk(obj)
}
func limitMessage(b []byte, h http.Header) string {
	msg := "account usage limit reached"
	var obj any
	_ = json.Unmarshal(b, &obj)
	var reset string
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for _, k := range []string{"resets_at", "reset_at", "reset_time"} {
				switch val := value[k].(type) {
				case float64:
					reset = time.Unix(int64(val), 0).UTC().Format(time.RFC3339)
				case string:
					if parsed, err := time.Parse(time.RFC3339, val); err == nil {
						reset = parsed.UTC().Format(time.RFC3339)
					} else if n, err := strconv.ParseInt(val, 10, 64); err == nil {
						reset = time.Unix(n, 0).UTC().Format(time.RFC3339)
					}
				}
			}
			for _, field := range value {
				walk(field)
			}
		}
	}
	walk(obj)
	if reset == "" {
		for _, name := range []string{"X-Codex-Primary-Reset-At", "X-Codex-Secondary-Reset-At"} {
			if n, err := strconv.ParseInt(h.Get(name), 10, 64); err == nil {
				reset = time.Unix(n, 0).UTC().Format(time.RFC3339)
				break
			}
		}
	}
	if reset == "" {
		if at, err := http.ParseTime(h.Get("Retry-After")); err == nil {
			reset = at.UTC().Format(time.RFC3339)
		}
	}
	if reset != "" {
		msg += "; resets at " + reset
	} else if delay, err := strconv.Atoi(h.Get("Retry-After")); err == nil && delay >= 0 {
		msg += "; retry after " + strconv.Itoa(delay) + " seconds"
	}
	return msg
}

type usageLimit struct{ message string }

func (e *usageLimit) Error() string { return e.message }

// readSSE retains each frame's bytes and joins multiline data per the SSE format.
func readSSE(reader io.Reader, visit func([]byte, []byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxBody)
	var frame, data []byte
	dispatch := func() error {
		if len(frame) == 0 {
			return nil
		}
		err := visit(frame, bytes.TrimSuffix(data, []byte("\n")))
		frame = nil
		data = nil
		return err
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(frame)+len(line) > maxBody {
			return errors.New("SSE frame too large")
		}
		frame = append(frame, line...)
		frame = append(frame, '\n')
		if len(line) == 0 {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			part := line[5:]
			part = bytes.TrimPrefix(part, []byte(" "))
			data = append(data, part...)
			data = append(data, '\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return dispatch()
}
func aggregate(reader io.Reader) ([]byte, error) {
	terminal := errors.New("terminal response received")
	var result json.RawMessage
	items := map[int]json.RawMessage{}
	err := readSSE(io.LimitReader(reader, maxBody+1), func(_ []byte, data []byte) error {
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			return nil
		}
		if quotaError(data) {
			return &usageLimit{limitMessage(data, nil)}
		}
		var e struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Item     json.RawMessage `json:"item"`
			Index    int             `json:"output_index"`
		}
		if json.Unmarshal(data, &e) != nil {
			return errors.New("invalid stream event")
		}
		switch e.Type {
		case "error", "response.failed":
			return errors.New("account stream failed")
		case "response.output_item.done":
			items[e.Index] = append(json.RawMessage(nil), e.Item...)
		case "response.completed", "response.incomplete":
			result = append(json.RawMessage(nil), e.Response...)
			return terminal
		}
		return nil
	})
	if err != nil && !errors.Is(err, terminal) {
		return nil, err
	}
	if len(result) == 0 {
		return nil, errors.New("missing terminal event")
	}
	var final map[string]json.RawMessage
	if json.Unmarshal(result, &final) != nil || final == nil {
		return nil, errors.New("invalid terminal response")
	}
	var output []json.RawMessage
	if json.Unmarshal(final["output"], &output) != nil || len(output) == 0 {
		for i := 0; i < len(items); i++ {
			item, ok := items[i]
			if !ok {
				return nil, errors.New("incomplete output items")
			}
			output = append(output, item)
		}
		if output == nil {
			output = []json.RawMessage{}
		}
		final["output"], _ = json.Marshal(output)
	}
	return json.Marshal(final)
}
func (r *Relay) passthrough(w http.ResponseWriter, reader io.Reader, redact func([]byte) []byte) {
	started := false
	err := readSSE(reader, func(frame, data []byte) error {
		if !started && quotaError(data) {
			return &usageLimit{limitMessage(data, nil)}
		}
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			started = true
		}
		if _, err := w.Write(redact(frame)); err != nil {
			return err
		}
		return http.NewResponseController(w).Flush()
	})
	if err != nil && !started {
		var limit *usageLimit
		if errors.As(err, &limit) {
			responseError(w, 429, limit.message)
		} else {
			responseError(w, 502, "account response stream failed")
		}
	} else if err != nil {
		_, _ = io.WriteString(w, "event: error\ndata: {\"type\":\"error\",\"message\":\"account response stream failed\"}\n\n")
	} else if !started {
		responseError(w, 502, "account response stream is empty")
	}
}
