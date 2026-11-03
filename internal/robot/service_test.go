package robot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

type fakeBridge struct {
	mu        sync.Mutex
	calls     []string
	bodies    []map[string]any
	connected bool
	failure   string
}

func (f *fakeBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	var body map[string]any
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodies = append(f.bodies, body)
	}
	w.Header().Set("Content-Type", "application/json")
	if f.failure != "" && r.URL.Path != "/stop" {
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": f.failure})
		return
	}
	switch r.URL.Path {
	case "/connect":
		f.connected = true
		_ = json.NewEncoder(w).Encode(map[string]any{"connected": true, "host": body["host"], "robot_mode": "RUNNING", "safety_mode": "NORMAL"})
	case "/disconnect", "/stop":
		f.connected = false
		fmt.Fprint(w, `{"ok":true,"connected":false}`)
	case "/state":
		_ = json.NewEncoder(w).Encode(State{Connected: f.connected, Host: "127.0.0.1", RobotMode: "RUNNING", SafetyMode: "NORMAL", TCPPose: []float64{.1, .2, .3, 0, 0, 0}})
	case "/poses":
		fmt.Fprint(w, `{"poses":{"home":[0.1,0.2,0.3,0,0,0]}}`)
	case "/health":
		fmt.Fprint(w, `{"ok":true,"version":"test"}`)
	default:
		fmt.Fprint(w, `{"ok":true,"final_pose":[0.1,0.2,0.3,0,0,0],"final_joints":[0,0,0,0,0,0]}`)
	}
}
func fixture(t *testing.T) (*Service, *fakeBridge) {
	t.Helper()
	f := &fakeBridge{}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	s := New(Options{URL: server.URL, External: true, Blobs: gateway.NewMemoryBlobs(), Bus: telemetry.NewBus(nil), PollInterval: 20 * time.Millisecond})
	s.Start(context.Background())
	t.Cleanup(func() { _ = s.Close() })
	return s, f
}
func uploadFrame(t *testing.T, s *Service) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b.Bytes())
	ref := hex.EncodeToString(sum[:])
	if err := s.opts.Blobs.Put(ref, bytes.NewReader(b.Bytes())); err != nil {
		t.Fatal(err)
	}
	return ref
}
func TestRPCFrameEventsAndErrors(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if got := s.Status(ctx); got.Bridge != "stopped" {
		t.Fatal(got)
	}
	for _, raw := range []string{`null`, `{"host":"bad"}`, `{"host":"127.0.0.1","unknown":1}`} {
		if _, err := s.Op(ctx, api.MethodRobotConnect, json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	out, err := s.Op(ctx, api.MethodRobotConnect, json.RawMessage(`{"host":"127.0.0.1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if status := out.(Status); status.Bridge != "running" || !status.State.Connected {
		t.Fatal(status)
	}
	ref := uploadFrame(t, s)
	raw := json.RawMessage(fmt.Sprintf(`{"blob":%q,"width":2,"height":3}`, ref))
	if _, err = s.Op(ctx, api.MethodRobotFrame, raw); err != nil {
		t.Fatal(err)
	}
	out, err = s.Op(ctx, api.MethodRobotStatus, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.(Status).Camera.Width != 2 {
		t.Fatal(out)
	}
	if _, err = s.Op(ctx, api.MethodRobotDisconnect, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Op(ctx, api.MethodRobotStop, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Op(ctx, api.MethodRobotStatus, json.RawMessage(`{"bad":1}`)); err == nil {
		t.Fatal("invalid status accepted")
	}
	if _, err = s.Op(ctx, "robot.unknown", nil); err == nil {
		t.Fatal("unknown accepted")
	}
	if _, err = (*Service)(nil).Op(ctx, api.MethodRobotStatus, nil); err == nil {
		t.Fatal("nil service accepted")
	}
	events := s.opts.Bus.Replay(types.Cursor{})
	if len(events) == 0 || events[0].Type != api.EventRobotState {
		t.Fatal(events)
	}
	var event StateEvent
	if err = json.Unmarshal(events[0].Payload, &event); err != nil || !event.State.Connected {
		t.Fatalf("event=%+v err=%v", event, err)
	}
}
func TestHeartbeatOnlyOnDemandAndClose(t *testing.T) {
	s, _ := fixture(t)
	time.Sleep(45 * time.Millisecond)
	if s.opts.Bus.Len() != 0 {
		t.Fatal("idle supervisor polled bridge")
	}
	if _, err := s.Connect(context.Background(), "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	before := s.opts.Bus.Len()
	deadline := time.Now().Add(time.Second)
	for s.opts.Bus.Len() <= before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.opts.Bus.Len() <= before {
		t.Fatal("missing connected heartbeat")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.Status(context.Background()).Bridge != "stopped" {
		t.Fatal("not stopped")
	}
	if _, err := s.Connect(context.Background(), "127.0.0.1"); err == nil {
		t.Fatal("closed service reactivated")
	}
}
func TestFrameValidationAndLook(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if _, _, err := s.Look(ctx); err == nil {
		t.Fatal("missing frame hidden")
	}
	ref := uploadFrame(t, s)
	for _, test := range []struct {
		ref  string
		w, h int
	}{{"../secret", 2, 3}, {strings.Repeat("0", 64), 2, 3}, {ref, 3, 2}, {ref, 0, 3}, {ref, 4097, 3}} {
		if err := s.Frame(test.ref, test.w, test.h); err == nil {
			t.Fatal(test)
		}
	}
	if err := s.Frame(ref, 2, 3); err != nil {
		t.Fatal(err)
	}
	status, images, err := s.Look(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Camera == nil || len(images) != 1 || !strings.HasPrefix(images[0].URL, "data:image/png;base64,") {
		t.Fatalf("%+v %+v", status, images)
	}
}
func TestEveryToolActionAndApproval(t *testing.T) {
	s, f := fixture(t)
	tool := &Tool{Service: s}
	ctx := context.Background()
	if err := tool.Definition().Validate(); err != nil {
		t.Fatal(err)
	}
	upload := uploadFrame(t, s)
	if err := s.Frame(upload, 2, 3); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"action":"status"}`, `{"action":"look"}`, `{"action":"stop"}`, `{"action":"list_poses"}`, `{"action":"save_pose","name":"home"}`, `{"action":"move_named","name":"home","approved":true}`, `{"action":"home","approved":true}`, `{"action":"move_pose","pose":[0.1,0.2,0.3,0,0,0],"speed":0.1,"accel":0.2,"approved":true}`, `{"action":"move_joints","joints":[0,0,0,0,0,0],"approved":true}`, `{"action":"move_relative","delta":{"dx":0.01},"frame":"tool","approved":true}`, `{"action":"gripper","gripper":"close","approved":true}`, `{"action":"freedrive","on":true,"approved":true}`, `{"action":"freedrive","on":false}`} {
		t.Run(raw, func(t *testing.T) {
			result, err := tool.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(raw)})
			if err != nil || result.Status != tools.StatusOK {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if strings.Contains(raw, `"look"`) && len(result.Images) != 1 {
				t.Fatal("look omitted model image")
			}
		})
	}
	f.mu.Lock()
	count := len(f.calls)
	f.mu.Unlock()
	for _, action := range []string{"move_named", "move_pose", "move_relative", "move_joints", "home", "gripper", "freedrive"} {
		result, err := tool.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(fmt.Sprintf(`{"action":%q}`, action))})
		if err == nil || result.Status != tools.StatusDenied {
			t.Fatalf("approval bypass %s: %+v %v", action, result, err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != count {
		t.Fatal("denied tool contacted bridge")
	}
	for _, body := range f.bodies {
		if body["kind"] != nil && body["approved"] != true {
			t.Fatal(body)
		}
	}
}
func TestToolInvalidAndBridgeFailure(t *testing.T) {
	s, f := fixture(t)
	tool := &Tool{Service: s}
	for _, raw := range []string{`null`, `{"action":"unknown"}`, `{"action":"status","bad":true}`, `{"action":"move_pose","approved":true}`, `{"action":"move_joints","approved":true}`, `{"action":"move_named","approved":true}`, `{"action":"move_relative","approved":true}`, `{"action":"move_relative","delta":{},"frame":"world","approved":true}`, `{"action":"gripper","approved":true}`, `{"action":"freedrive","approved":true}`, `{"action":"save_pose"}`} {
		result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(raw)})
		if err == nil || result.Status == tools.StatusOK {
			t.Fatalf("accepted %s", raw)
		}
	}
	f.mu.Lock()
	f.failure = "PROTECTIVE_STOP"
	f.mu.Unlock()
	result, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"home","approved":true}`)})
	if err == nil || !strings.Contains(result.Output, "PROTECTIVE_STOP") {
		t.Fatalf("%+v %v", result, err)
	}
	// STOP remains available even when health and all other endpoints fail.
	if result, err = tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"stop"}`)}); err != nil || result.Status != tools.StatusOK {
		t.Fatalf("stop: %+v %v", result, err)
	}
}
func TestCanceledMoveSendsIndependentStop(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})
	var stopOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true}`)
		case "/move":
			close(entered)
			select {
			case <-r.Context().Done():
			case <-stopped:
			case <-time.After(time.Second):
			}
		case "/stop":
			stopOnce.Do(func() { close(stopped) })
			fmt.Fprint(w, `{"ok":true}`)
		default:
			fmt.Fprint(w, `{"connected":false}`)
		}
	}))
	defer server.Close()
	s := New(Options{URL: server.URL, External: true})
	s.Start(context.Background())
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	safego.Go("test.robot.cancel", nil, func() { <-entered; cancel() })
	if _, err := s.Call(ctx, http.MethodPost, "/move", map[string]any{"approved": true}); err == nil {
		t.Fatal("canceled move succeeded")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("missing stop")
	}
}
func TestSupervisorStartsRestartsAndJoinsRealPythonProcess(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	script, err := filepath.Abs("../../extensions/robot-ur/bridge/server.py")
	if err != nil {
		t.Fatal(err)
	}
	var starts atomic.Int32
	s := New(Options{URL: fmt.Sprintf("http://127.0.0.1:%d", port), Python: python, Script: script, PollInterval: 20 * time.Millisecond, RestartBackoff: 20 * time.Millisecond, Command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
		starts.Add(1)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), fmt.Sprintf("EXCALIBUR_ROBOT_BRIDGE_PORT=%d", port), "PYTHONDONTWRITEBYTECODE=1")
		return cmd
	}})
	s.Start(context.Background())
	defer s.Close()
	if starts.Load() != 0 {
		t.Fatal("bridge started before demand")
	}
	if err = s.ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	first := s.cmd
	done := s.processDone
	s.mu.Unlock()
	_ = first.Process.Kill()
	<-done
	deadline := time.Now().Add(3 * time.Second)
	for starts.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if starts.Load() < 2 {
		t.Fatal("bridge was not restarted")
	}
	if err = s.ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil || s.bridge != "stopped" {
		t.Fatal("subprocess leaked")
	}
}
func TestSupervisorBackoffOnLaunchFailure(t *testing.T) {
	s := New(Options{URL: "http://127.0.0.1:1", Python: "/missing/robot-python", RestartBackoff: time.Hour})
	s.Start(context.Background())
	defer s.Close()
	if err := s.ensure(context.Background()); err == nil {
		t.Fatal("missing Python accepted")
	}
	if err := s.ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "backoff") {
		t.Fatalf("backoff missing: %v", err)
	}
}

func TestInstallableBuiltinExtensionUsesSameTool(t *testing.T) {
	s, _ := fixture(t)
	reg := tools.NewMapRegistry()
	executor := tools.NewExecutor(reg)
	cat := tools.NewDeferredCatalog(reg)
	loader := tools.NewLoader(reg, cat, executor)
	defer loader.Close()
	loader.ProvideBuiltin("robot-ur", []tools.Tool{&Tool{Service: s}})
	extension, err := loader.LoadDir("../../extensions/robot-ur")
	if err != nil {
		t.Fatal(err)
	}
	if err = loader.Activate(extension); err != nil {
		t.Fatal(err)
	}
	if !cat.IsDeferred("robot") {
		t.Fatal("installable tool is not deferred")
	}
	result, err := executor.Execute(context.Background(), "robot", json.RawMessage(`{"action":"status"}`), tools.ExecuteOptions{})
	if err != nil || result.Status != tools.StatusOK {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestStopWithCanceledContextStillReachesBridge(t *testing.T) {
	s, f := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 1 || f.calls[0] != "POST /stop" {
		t.Fatal(f.calls)
	}
}

func TestCanceledOwnerCannotRestartBridge(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(Options{URL: "http://127.0.0.1:1", External: true})
	s.Start(ctx)
	cancel()
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
	if err := s.ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "supervisor stopped") {
		t.Fatalf("stopped owner reactivated: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
