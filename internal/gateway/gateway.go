package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Config carries the gate surface of Section 3.5.
type Config struct {
	// WebUI optionally serves static files and SPA routes after API routing.
	WebUI http.Handler

	// DaemonBlobs overrides the transport store with the daemon database store.
	DaemonBlobs BlobStore
	// BlobMIME optionally resolves a document MIME by original digest.
	BlobMIME func(context.Context, string) (string, error)

	// Listen is the bind address. The gate listens on loopback by
	// default; non-loopback binds are rejected unless AllowRemote is
	// set (tests may set it explicitly).
	Listen string
	// AllowRemote permits an explicit non-loopback container bind. Publish
	// container host ports on loopback unless remote access is intended.
	AllowRemote bool
	// ClientID identifies this daemon to idempotency keys.
	ClientID string
	// ReadTimeout caps one request; WS pings keep streams alive.
	ReadTimeout time.Duration
}

// DefaultConfig binds the loopback gate on an ephemeral port.
func DefaultConfig() Config {
	return Config{Listen: "127.0.0.1:0", ReadTimeout: 30 * time.Second}
}

// Server is the Section 3.5 gate: JSON-RPC 2.0 over HTTP POST /rpc
// and WebSocket /ws, plus blobs over HTTP. It enforces the loopback
// bind, routes every api.Methods entry, and fans bus events out to
// cursor subscriptions. Long-lived serve loops use safego.Go.
type Server struct {
	cfg     Config
	backend Backend
	blobs   BlobStore
	bus     *telemetry.Bus

	mu       sync.Mutex
	srv      *http.Server
	listener net.Listener
	mux      *http.ServeMux
	subs     map[string]*subscription
	done     chan struct{}
}

// New returns a gate over backend and blobs. Bus may be nil (then
// subscriptions replay history only and observe no live events).
func New(cfg Config, backend Backend, blobs BlobStore, bus *telemetry.Bus) *Server {
	if backend == nil {
		backend = NewMemoryBackend(bus)
	}
	if cfg.DaemonBlobs != nil {
		blobs = cfg.DaemonBlobs
	}
	if blobs == nil {
		blobs = NewMemoryBlobs()
	}
	s := &Server{
		cfg:     cfg,
		backend: backend,
		blobs:   blobs,
		bus:     bus,
		subs:    map[string]*subscription{},
		done:    make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", s.serveRPC)
	mux.HandleFunc("/ws", s.serveWS)
	mux.HandleFunc("/ws/browser", newBrowserLiveHandler(s).serveBrowserLive)
	mux.HandleFunc("/ws/voice/listen", newVoiceLiveHandler(s).serveListen)
	mux.HandleFunc("/ws/voice/speak", newVoiceLiveHandler(s).serveSpeak)
	mux.HandleFunc("/blobs/", s.serveBlobs)
	mux.HandleFunc("/blobs", s.uploadBlob)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if cfg.WebUI != nil {
		mux.Handle("/", cfg.WebUI)
	}
	s.mux = mux
	return s
}

// checkLoopback rejects non-loopback binds unless AllowRemote.
func checkLoopback(listen string, allowRemote bool) error {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("gateway: bad listen %q: %w", listen, err)
	}
	if host == "" {
		host = "0.0.0.0"
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if allowRemote {
		return nil
	}
	return fmt.Errorf("gateway: refusing non-loopback bind %q without AllowRemote", listen)
}

// Start binds and serves; it returns once the socket is listening.
// The accept loop runs on a safego.Go goroutine so a panic there
// cannot take the daemon down silently.
func (s *Server) Start() (string, error) {
	if err := checkLoopback(s.cfg.Listen, s.cfg.AllowRemote); err != nil {
		return "", err
	}
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return "", fmt.Errorf("gateway: listen: %w", err)
	}
	if !s.cfg.AllowRemote {
		addr, ok := ln.Addr().(*net.TCPAddr)
		if !ok || !addr.IP.IsLoopback() {
			_ = ln.Close()
			return "", fmt.Errorf("gateway: resolved bind %q is not loopback", ln.Addr())
		}
	}
	s.mu.Lock()
	s.listener = ln
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second}
	s.mu.Unlock()
	safego.Go("gateway-serve", nil, func() {
		_ = s.srv.Serve(ln)
	})
	return ln.Addr().String(), nil
}

// Stop shuts the gate down, closing every subscription.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	srv := s.srv
	select {
	case <-s.done:
	default:
		close(s.done)
	}
	s.mu.Unlock()
	s.closeAllSubs()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// ServeHTTP enforces the loopback-only peer check on top of the mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.AllowRemote {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				http.Error(w, "loopback only", http.StatusForbidden)
				return
			}
		}
	}
	// Browsers must use the same origin. CLI clients need no browser token.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) {
			http.Error(w, "same origin required", http.StatusForbidden)
			return
		}
	}
	if !s.cfg.AllowRemote && s.cfg.WebUI != nil {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "localhost" {
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				http.Error(w, "loopback host required", http.StatusForbidden)
				return
			}
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	s.mux.ServeHTTP(w, r)
}

// Addr returns the bound address after Start.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// serveRPC handles JSON-RPC 2.0 over HTTP: single requests and batch
// arrays. Notifications (no id) are applied with an empty body.
func (s *Server) serveRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRPCBodyBytes)
	body, err := io.ReadAll(r.Body)
	defer clear(body)
	if err != nil {
		writeResponse(w, api.NewError(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"}))
		return
	}
	var raw json.RawMessage
	defer func() { clear(raw) }()
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		writeResponse(w, api.NewError(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"}))
		return
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(raw, &batch); err != nil {
			writeResponse(w, api.NewError(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"}))
			return
		}
		out := make([]api.Response, 0, len(batch))
		for _, item := range batch {
			if resp := s.dispatch(r.Context(), item); resp != nil {
				out = append(out, *resp)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	resp := s.dispatch(r.Context(), raw)
	if resp == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeResponse(w, resp)
}

const maxRPCBodyBytes = 4 << 20

func writeResponse(w http.ResponseWriter, resp *api.Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// dispatch decodes one envelope, validates it and routes the method.
// Unknown methods answer CodeMethodNotFound; a nil-ID notification
// applies the call and returns nil (no response).
func (s *Server) dispatch(ctx context.Context, raw json.RawMessage) *api.Response {
	var req api.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return api.NewError(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"})
	}
	if req.Method == api.MethodCredentialsSubmit || req.Method == api.MethodCredentialsSaveLogin {
		defer clear(raw)
		defer clear(req.Params)
	}
	if err := req.Validate(); err != nil {
		if apiErr, ok := err.(*api.Error); ok {
			return api.NewError(req.ID, apiErr)
		}
		return api.NewError(req.ID, &api.Error{Code: api.CodeInvalidRequest, Message: err.Error()})
	}
	notification := len(req.ID) == 0
	result, apiErr := s.route(ctx, req.Method, req.Params, clientIDOf(ctx))
	if req.Method == api.MethodCredentialsSubmit || req.Method == api.MethodCredentialsSaveLogin {
		clear(req.Params)
	}
	if apiErr != nil {
		if notification {
			return nil
		}
		return api.NewError(req.ID, apiErr)
	}
	if notification {
		return nil
	}
	resp, err := api.NewResult(req.ID, result)
	if err != nil {
		return api.NewError(req.ID, &api.Error{Code: api.CodeInternal, Message: err.Error()})
	}
	return resp
}

// route maps every api.Methods entry to its backend call. Fast-lane
// reads (run.status, run.list, conversation history) never touch a
// model; unknown names answer CodeMethodNotFound (Section 3.5).
func (s *Server) route(ctx context.Context, method string, params json.RawMessage, clientID string) (any, *api.Error) {
	decode := func(v any) *api.Error {
		if len(params) == 0 {
			return nil
		}
		if err := json.Unmarshal(params, v); err != nil {
			return &api.Error{Code: api.CodeInvalidParams, Message: "bad params: " + err.Error()}
		}
		return nil
	}
	switch method {
	case api.MethodConversationSend:
		var p api.ConversationSendParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.ClientMsgID == "" || p.ConversationID == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "conversation.send needs conversation_id and client_msg_id"}
		}
		if err := p.Context.Validate(); err != nil {
			return nil, asAPIError(err)
		}
		var msgID types.MessageID
		var err error
		if contextual, ok := s.backend.(ConversationContextBackend); ok {
			msgID, _, err = contextual.ConversationSendContext(ctx, clientID, p.ClientMsgID, p.ConversationID, p.Text, p.Attachments, p.Context)
		} else if p.Context != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "app context is unavailable on this backend"}
		} else {
			msgID, _, err = s.backend.ConversationSend(ctx, clientID, p.ClientMsgID, p.ConversationID, p.Text)
		}
		if err != nil {
			return nil, asAPIError(err)
		}
		return api.ConversationSendResult{MsgID: string(msgID)}, nil
	case api.MethodConversationHistory:
		var p api.ConversationHistoryParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		msgs, err := s.backend.ConversationHistory(ctx, p.ConversationID, p.Before, p.Limit)
		if err != nil {
			return nil, asAPIError(err)
		}
		return map[string]any{"messages": msgs}, nil
	case api.MethodConversationSubscribe:
		var p api.ConversationSubscribeParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		cursor, err := types.ParseCursor(p.Cursor)
		if err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
		}
		return map[string]any{"events": s.replay(cursor, p.Types), "cursor": cursor.String()}, nil
	case api.MethodRunList:
		rows, err := s.backend.RunList(ctx)
		if err != nil {
			return nil, asAPIError(err)
		}
		return map[string]any{"runs": rows}, nil
	case api.MethodRunStatus, api.MethodRunPause, api.MethodRunResume,
		api.MethodRunInterrupt, api.MethodRunCancel, api.MethodRunRedirect:
		var p struct {
			RunID     string `json:"run_id"`
			Interrupt bool   `json:"interrupt"`
			Text      string `json:"text"`
		}
		if err := decode(&p); err != nil {
			return nil, err
		}
		if p.RunID == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "run control needs run_id"}
		}
		snap, err := s.backend.RunControl(ctx, method, p.RunID, p.Interrupt, p.Text)
		if err != nil {
			return nil, asAPIError(err)
		}
		return snap, nil
	case api.MethodTaskStart, api.MethodTaskStatus, api.MethodTaskWait,
		api.MethodTaskResult, api.MethodTaskReply, api.MethodTaskCancel,
		api.MethodTaskPause, api.MethodTaskResume, api.MethodTaskList,
		api.MethodTaskLogs:
		res, err := s.backend.TaskOp(ctx, method, params)
		if err != nil {
			return nil, asAPIError(err)
		}
		return res, nil
	case api.MethodRobotStatus, api.MethodRobotConnect, api.MethodRobotDisconnect, api.MethodRobotStop, api.MethodRobotFrame,
		api.MethodVoiceStatus, api.MethodVoiceTranscribe, api.MethodVoiceSpeak,
		api.MethodAutomationsList, api.MethodAutomationsGet, api.MethodAutomationsRuns, api.MethodAutomationsRun, api.MethodAutomationsPause, api.MethodAutomationsResume, api.MethodAutomationsDelete, api.MethodTasksQuery, api.MethodTasksGet, api.MethodRemindersList, api.MethodRemindersCreate, api.MethodRemindersUpdate, api.MethodRemindersDelete, api.MethodSkillsGroups, api.MethodSkillsList, api.MethodSkillsGet, api.MethodSkillsUpdate, api.MethodSkillsSetState, api.MethodSkillsDelete, api.MethodUsageSummary:
		res, err := s.backend.Passthrough(ctx, method, params)
		if err != nil {
			return nil, asAPIError(err)
		}
		return res, nil
	case "codex.status", "codex.login.start", "codex.login.complete", "codex.logout",
		api.MethodCollectionsList, api.MethodCollectionsGet, api.MethodCollectionsCreate, api.MethodCollectionsUpdate, api.MethodCollectionsFields, api.MethodCollectionsDelete, api.MethodCollectionsSummary, api.MethodCollectionsImport, api.MethodCollectionsExport, api.MethodRecordsList, api.MethodRecordsGet, api.MethodRecordsCreate, api.MethodRecordsUpdate, api.MethodRecordsDelete,
		api.MethodGoogleStatus, api.MethodGoogleConnectStart, api.MethodGoogleConnectComplete, api.MethodGoogleDisconnect,
		api.MethodPrincipalGet, api.MethodPrincipalUpdate:
		if len(params) > 0 && !json.Valid(params) {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "bad " + method + " params"}
		}
		res, err := s.backend.Passthrough(ctx, method, params)
		if err != nil {
			return nil, asAPIError(err)
		}
		return res, nil
	case api.MethodFilesList:
		var p api.FilesListParams
		if err := decode(&p); err != nil {
			return nil, err
		}
		res, err := s.backend.FilesList(ctx, p)
		if err != nil {
			return nil, asAPIError(err)
		}
		return res, nil
	default:
		// Project, plan, memory, skills, capability, vault, question,
		// metrics and config families route to the passthrough seam so
		// the daemon owns their stores; unknown names are rejected.
		known := false
		for _, m := range api.Methods {
			if m.Name == method {
				known = true
				break
			}
		}
		if !known {
			return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
		}
		res, err := s.backend.Passthrough(ctx, method, params)
		if err != nil {
			return nil, asAPIError(err)
		}
		return res, nil
	}
}

func asAPIError(err error) *api.Error {
	if apiErr, ok := err.(*api.Error); ok {
		return apiErr
	}
	return &api.Error{Code: api.CodeInternal, Message: err.Error()}
}

// serveWS upgrades to WebSocket JSON-RPC: each text/binary message is
// one envelope, each response carries the request id. A
// conversation.subscribe request additionally attaches the socket to
// the live feed from its cursor (Section 3.5).
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	conn.SetReadLimit(maxRPCBodyBytes)
	ctx := r.Context()
	peer := &wsPeer{conn: conn}
	sub := s.newSub(nil)
	defer s.dropSub(sub.id)
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var req api.Request
		decodeErr := json.Unmarshal(raw, &req)
		clear(raw)
		if decodeErr != nil {
			resp := api.NewError(nil, &api.Error{Code: api.CodeParseError, Message: "parse error"})
			_ = peer.write(ctx, resp)
			continue
		}
		if err := req.Validate(); err != nil {
			if req.Method == api.MethodCredentialsSubmit || req.Method == api.MethodCredentialsSaveLogin {
				clear(req.Params)
			}
			_ = peer.write(ctx, api.NewError(req.ID, &api.Error{Code: api.CodeInvalidRequest, Message: err.Error()}))
			continue
		}
		if req.Method == api.MethodConversationSubscribe {
			var p api.ConversationSubscribeParams
			if len(req.Params) > 0 {
				if err := json.Unmarshal(req.Params, &p); err != nil {
					_ = peer.write(ctx, api.NewError(req.ID, &api.Error{Code: api.CodeInvalidParams, Message: "bad params"}))
					continue
				}
			}
			cursor, err := types.ParseCursor(p.Cursor)
			if err != nil {
				_ = peer.write(ctx, api.NewError(req.ID, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}))
				continue
			}
			sub.mu.Lock()
			sub.cursor = cursor
			sub.types = p.Types
			sub.mu.Unlock()
			past := s.replay(cursor, p.Types)
			ack, _ := api.NewResult(req.ID, map[string]any{"events": past, "cursor": cursor.String()})
			ackRaw, err := json.Marshal(ack)
			if err != nil {
				return
			}
			// Register the live tail before acknowledging readiness. Hold the
			// writer so a live push cannot overtake the subscribe response.
			peer.mu.Lock()
			sub.attach(ctx, s, peer)
			err = peer.writeRawLocked(ctx, ackRaw)
			peer.mu.Unlock()
			if err != nil {
				return
			}
			continue
		}
		result, apiErr := s.route(ctx, req.Method, req.Params, clientIDOf(ctx))
		if req.Method == api.MethodCredentialsSubmit || req.Method == api.MethodCredentialsSaveLogin {
			clear(req.Params)
		}
		if len(req.ID) == 0 {
			continue
		}
		if apiErr != nil {
			if err := peer.write(ctx, api.NewError(req.ID, apiErr)); err != nil {
				return
			}
			continue
		}
		resp, err := api.NewResult(req.ID, result)
		if err != nil {
			_ = peer.write(ctx, api.NewError(req.ID, &api.Error{Code: api.CodeInternal, Message: err.Error()}))
			continue
		}
		if err := peer.write(ctx, resp); err != nil {
			return
		}
	}
}

// wsPeer serializes every write to one socket: the read loop serves
// RPC responses while a live tail pushes events beside it.
type wsPeer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (p *wsPeer) write(ctx context.Context, v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return p.writeRaw(ctx, buf)
}

func (p *wsPeer) writeRaw(ctx context.Context, buf []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writeRawLocked(ctx, buf)
}

func (p *wsPeer) writeRawLocked(ctx context.Context, buf []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.conn.Write(ctx, websocket.MessageText, buf)
}

// tryWriteEphemeral reserves a free socket writer or discards the frame. There
// is no send queue, outbox or cursor update for live-only visualization data.
func (p *wsPeer) tryWriteEphemeral(ctx context.Context, buf []byte) {
	if !p.mu.TryLock() {
		return
	}
	safego.Go("gateway-ephemeral", nil, func() {
		defer p.mu.Unlock()
		writeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_ = p.conn.Write(writeCtx, websocket.MessageText, buf)
	})
}
