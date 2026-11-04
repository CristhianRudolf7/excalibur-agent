package robot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

// Options permits fake HTTP bridges and process launchers in ordinary tests.
type Options struct {
	URL    string
	Python string
	Script string
	Blobs  gateway.BlobStore
	Bus    *telemetry.Bus
	// External skips process management for an explicitly injected test bridge.
	External       bool
	PollInterval   time.Duration
	RestartBackoff time.Duration
	Command        func(context.Context, string, ...string) *exec.Cmd
}

// Service is daemon-owned. Start/Close bound every subprocess and goroutine.
type Service struct {
	opts        Options
	client      *http.Client
	mu          sync.Mutex
	startMu     sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	cmd         *exec.Cmd
	processDone chan struct{}
	wanted      bool
	closed      bool
	bridge      string
	lastErr     string
	closeErr    error
	state       *State
	frame       string
	camera      *Camera
	nextStart   time.Time
	backoff     time.Duration
	calMu       sync.Mutex
	calPath     string
	calibration calibrationData
}

// New allocates no goroutines and never starts Python.
func New(opts Options) *Service {
	if opts.URL == "" {
		port := os.Getenv("EXCALIBUR_ROBOT_BRIDGE_PORT")
		if port == "" {
			port = "7811"
		}
		opts.URL = "http://127.0.0.1:" + port
	}
	if opts.Python == "" {
		opts.Python = os.Getenv("EXCALIBUR_ROBOT_PYTHON")
		if opts.Python == "" {
			opts.Python = defaultRobotPython()
		}
	}
	if opts.Script == "" {
		opts.Script = os.Getenv("EXCALIBUR_ROBOT_BRIDGE_SCRIPT")
		if opts.Script == "" {
			opts.Script = defaultRobotScript()
			if _, err := os.Stat(opts.Script); err != nil {
				opts.Script = filepath.Join("extensions", "robot-ur", "bridge", "server.py")
			}
		}
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}
	if opts.RestartBackoff <= 0 {
		opts.RestartBackoff = time.Second
	}
	if opts.Command == nil {
		opts.Command = exec.CommandContext
	}
	return &Service{opts: opts, client: &http.Client{Timeout: 75 * time.Second}, bridge: "stopped", backoff: opts.RestartBackoff}
}

// Start launches only the demand/health loop, never the bridge itself.
func (s *Service) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil || s.closed {
		return
	}
	s.ctx, s.cancel = context.WithCancel(parent)
	s.done = make(chan struct{})
	ctx, done := s.ctx, s.done
	safego.Go("robot.supervisor", nil, func() {
		defer close(done)
		defer s.stopOwned()
		ticker := time.NewTicker(s.opts.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				wanted := s.wanted
				s.mu.Unlock()
				if !wanted {
					continue
				}
				healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				err := s.ensure(healthCtx)
				cancel()
				if err == nil {
					pollCtx, stop := context.WithTimeout(ctx, 2*time.Second)
					_, _ = s.State(pollCtx)
					stop()
				}
			}
		}
	})
}

// Done closes after the supervisor has stopped and joined its bridge process.
func (s *Service) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// Close stops robot activity before killing and joining the owned bridge.
func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func (s *Service) stopOwned() {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.mu.Lock()
	cmd, processDone := s.cmd, s.processDone
	s.mu.Unlock()
	var err error
	s.mu.Lock()
	wanted := s.wanted
	s.mu.Unlock()
	if wanted {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = s.request(ctx, http.MethodPost, "/stop", map[string]any{})
		cancel()
	}
	if cmd != nil {
		_ = stopProcess(cmd)
		if processDone != nil {
			<-processDone
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cmd = nil
	s.processDone = nil
	s.bridge = "stopped"
	s.state = &State{Connected: false}
	if err != nil {
		s.lastErr = err.Error()
	}
	s.closeErr = err
}

func (s *Service) ensure(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("robot service closed")
	}
	if s.ctx == nil {
		s.mu.Unlock()
		return errors.New("robot supervisor not started")
	}
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return errors.New("robot supervisor stopped")
	}
	s.wanted = true
	cmd, processDone, next := s.cmd, s.processDone, s.nextStart
	s.mu.Unlock()
	healthCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, err := s.request(healthCtx, http.MethodGet, "/health", nil)
	cancel()
	if err == nil {
		s.mu.Lock()
		s.bridge = "running"
		s.lastErr = ""
		s.mu.Unlock()
		return nil
	}
	if s.opts.External {
		return s.fail(err)
	}
	if err := supported(); err != nil {
		return s.fail(err)
	}
	if time.Now().Before(next) {
		return s.fail(fmt.Errorf("bridge restart backoff: %w", err))
	}
	if cmd != nil {
		stopCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
		_, _ = s.request(stopCtx, http.MethodPost, "/stop", map[string]any{})
		stop()
		_ = stopProcess(cmd)
		if processDone != nil {
			<-processDone
		}
		s.retryLater()
		s.mu.Lock()
		s.cmd = nil
		s.processDone = nil
		s.state = &State{Connected: false}
		s.mu.Unlock()
		return s.fail(errors.New("robot bridge unhealthy; reconnect after restart backoff"))
	}
	s.mu.Lock()
	processCtx := s.ctx
	s.mu.Unlock()
	command := s.opts.Command(context.WithoutCancel(processCtx), s.opts.Python, s.opts.Script)
	configureProcess(command)
	// No stdout/stderr forwarding: errors are returned through HTTP and status.
	if err = startRobotProcess(command); err != nil {
		s.retryLater()
		return s.fail(fmt.Errorf("start robot bridge: %w", err))
	}
	done := make(chan struct{})
	s.mu.Lock()
	s.cmd = command
	s.processDone = done
	s.mu.Unlock()
	safego.Go("robot.bridge", nil, func() { defer close(done); defer cleanupRobotProcess(command); _ = command.Wait() })
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return s.fail(ctx.Err())
		case <-done:
			s.retryLater()
			return s.fail(errors.New("robot bridge exited before health check"))
		case <-deadline.C:
			s.retryLater()
			return s.fail(errors.New("robot bridge health check timed out"))
		case <-ticker.C:
			_, err = s.request(ctx, http.MethodGet, "/health", nil)
			if err == nil {
				s.mu.Lock()
				s.bridge = "running"
				s.lastErr = ""
				s.backoff = s.opts.RestartBackoff
				s.nextStart = time.Time{}
				s.mu.Unlock()
				return nil
			}
		}
	}
}

func (s *Service) retryLater() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextStart = time.Now().Add(s.backoff)
	s.backoff = min(30*time.Second, s.backoff*2)
}
func (s *Service) fail(err error) error {
	s.mu.Lock()
	s.bridge = "error"
	s.lastErr = err.Error()
	var state *State
	if s.state != nil {
		copy := *s.state
		copy.Connected = false
		state = &copy
	}
	s.mu.Unlock()
	if state != nil {
		s.publish(*state)
	}
	return err
}

func (s *Service) request(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, s.opts.URL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("invalid bridge JSON: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (envelope.OK != nil && !*envelope.OK) {
		if envelope.Error == "" {
			envelope.Error = resp.Status
		}
		return nil, errors.New(envelope.Error)
	}
	return raw, nil
}

// Call proxies one validated action. A canceled move triggers an independent STOP.
func (s *Service) Call(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	if err := s.ensure(ctx); err != nil {
		return nil, err
	}
	raw, err := s.request(ctx, method, path, body)
	if err != nil && (path == "/move" || path == "/gripper" || path == "/freedrive") {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, stopErr := s.request(stopCtx, http.MethodPost, "/stop", map[string]any{})
		cancel()
		err = errors.Join(err, stopErr)
	}
	if method != http.MethodGet {
		pollCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_, _ = s.State(pollCtx)
		cancel()
	}
	return raw, err
}

// Stop bypasses ensure, restart backoff and motion serialization.
func (s *Service) Stop(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	raw, err := s.request(ctx, http.MethodPost, "/stop", map[string]any{})
	if err == nil {
		s.publish(State{Connected: false})
	}
	return raw, err
}

// State reads without starting a process.
func (s *Service) State(ctx context.Context) (*State, error) {
	raw, err := s.request(ctx, http.MethodGet, "/state", nil)
	if err != nil {
		return nil, s.fail(err)
	}
	var state State
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, s.fail(err)
	}
	s.publish(state)
	return &state, nil
}
func (s *Service) publish(state State) {
	s.mu.Lock()
	changed := !reflect.DeepEqual(s.state, &state)
	s.state = &state
	s.mu.Unlock()
	if s.opts.Bus != nil && (state.Connected || changed) {
		_, _ = s.opts.Bus.Publish("robot.state", nil, nil, StateEvent{State: state})
	}
}

// Status observes a stopped service without starting it.
func (s *Service) Status(ctx context.Context) Status {
	s.mu.Lock()
	active := s.bridge == "running"
	s.mu.Unlock()
	if active {
		_, _ = s.State(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Bridge: s.bridge, Error: s.lastErr}
	if s.state != nil {
		state := *s.state
		out.State = &state
	}
	if s.camera != nil {
		camera := *s.camera
		out.Camera = &camera
	}
	return out
}

// Connect validates literal IPs before contacting the loopback bridge.
func (s *Service) Connect(ctx context.Context, host string) (Status, error) {
	if net.ParseIP(host) == nil {
		return Status{}, errors.New("host must be a robot IP address")
	}
	_, err := s.Call(ctx, http.MethodPost, "/connect", map[string]string{"host": host})
	return s.Status(ctx), err
}

// Frame retains a verified image ref, with bounded encoded and decoded size.
func (s *Service) Frame(blob string, width, height int) error {
	if s.opts.Blobs == nil {
		return errors.New("robot frame store unavailable")
	}
	digest, err := hex.DecodeString(blob)
	if err != nil || len(digest) != 32 || len(blob) != 64 {
		return errors.New("blob must be a SHA-256 digest")
	}
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 || width*height > 12_000_000 {
		return errors.New("invalid camera dimensions")
	}
	reader, size, err := s.opts.Blobs.Get(blob)
	if err != nil {
		return fmt.Errorf("camera blob: %w", err)
	}
	defer reader.Close()
	if size > 4<<20 {
		return errors.New("camera frame exceeds 4 MiB")
	}
	cfg, _, err := image.DecodeConfig(io.LimitReader(reader, 4<<20))
	if err != nil {
		return errors.New("camera blob must be PNG or JPEG")
	}
	if cfg.Width != width || cfg.Height != height {
		return errors.New("camera dimensions do not match blob")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frame = strings.ToLower(blob)
	s.camera = &Camera{UpdatedAt: time.Now().UTC(), Width: width, Height: height}
	return nil
}

type LookResult struct {
	Status
	Calibration api.RobotCalibrationStatus `json:"calibration"`
	Image       api.RobotImageSize         `json:"image"`
	Instruction string                     `json:"instruction"`
}

// Look overlays pixel coordinates for the current language model directly.
func (s *Service) Look(ctx context.Context) (LookResult, []providers.ImageInput, error) {
	status := LookResult{Status: s.Status(ctx), Calibration: s.CalibrationStatus(), Instruction: "Estimate the pixel center of objects in this image and call locate with pixel:[u,v]. Origin is top-left; u increases right, v increases down. Grid spacing is 100 px. locate returns table-plane coordinates; account for object height and gripper offset. Follow state.approval_required for confirmations. STOP is always available."}
	s.mu.Lock()
	ref := s.frame
	if s.camera != nil {
		camera := *s.camera
		status.Camera = &camera
		status.Image = api.RobotImageSize{Width: camera.Width, Height: camera.Height}
	}
	s.mu.Unlock()
	if ref == "" {
		return status, nil, errors.New("no camera frame; share a camera first")
	}
	r, size, err := s.opts.Blobs.Get(ref)
	if err != nil {
		return status, nil, err
	}
	defer r.Close()
	if size > 4<<20 {
		return status, nil, errors.New("camera frame exceeds 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(r, (4<<20)+1))
	if err != nil {
		return status, nil, err
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return status, nil, fmt.Errorf("decode camera frame: %w", err)
	}
	var overlay bytes.Buffer
	if err = png.Encode(&overlay, pixelGrid(src)); err != nil {
		return status, nil, err
	}
	return status, []providers.ImageInput{{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(overlay.Bytes())}}, nil
}

func strictDecode(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("expected JSON object")
	}
	return nil
}
