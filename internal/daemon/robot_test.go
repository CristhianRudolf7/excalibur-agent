package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robot"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func TestRobotGatewayAndAssistantImageRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/state" || r.URL.Path == "/connect" {
			fmt.Fprint(w, `{"connected":true,"host":"127.0.0.1","robot_mode":"RUNNING","safety_mode":"NORMAL","tcp_pose":[0.1,0.2,0.3,0.4,0.5,0.6]}`)
		} else {
			fmt.Fprint(w, `{"ok":true,"version":"test"}`)
		}
	}))
	defer bridge.Close()
	blobs := gateway.NewMemoryBlobs()
	bus := telemetry.NewBus(nil)
	service := robot.New(robot.Options{URL: bridge.URL, External: true, Blobs: blobs, Bus: bus})
	service.Start(ctx)
	defer service.Close()
	calls := 0
	g := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "robot-test", Model: "same-model"}}}})
	g.Register(&turnProvider{FakeProvider: fake.New("robot-test"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		calls++
		if calls == 1 {
			found := false
			for _, def := range req.Tools {
				if def.Name == "robot" {
					found = true
				}
			}
			if !found {
				t.Error("robot absent from conversational assistant allowlist")
			}
			return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: "robot-look", Name: "robot", Arguments: json.RawMessage(`{"action":"look"}`)}}}}, nil
		}
		found := false
		for _, message := range req.Messages {
			if message.ToolResult != nil && message.ToolResult.CallID == "robot-look" {
				found = len(message.ToolResult.Images) == 1
			}
		}
		if !found {
			t.Error("camera image lost between tool, engine history and compiled model request")
		}
		return textReply("I can see the gripper. STOP is available."), nil
	}})
	defer g.Close()
	s, err := Assemble(AssembleOptions{Robot: service, Bus: bus, Models: g})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Catalog.IsDeferred("robot") {
		t.Fatal("robot must be deferred")
	}
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), &OrchControl{Store: orchestrator.NewMemoryStore()}, bus)
	b.AttachServices(BackendServices{Robot: service, FileBlobs: blobs})
	WireAssistantTurns(b, s, g, "same-model")
	defer s.delegations.stop()
	defer func() { _ = b.Master.Turns().Stop(context.Background()) }()
	gate := gateway.New(gateway.DefaultConfig(), b, blobs, bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Stop(context.Background())
	rpc := func(method string, params any) api.Response {
		t.Helper()
		request, err := api.NewRequest("robot-rpc", method, params)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(request)
		response, err := http.Post("http://"+addr+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var out api.Response
		if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, method := range []string{api.MethodRobotStatus, api.MethodRobotStop, api.MethodRobotDisconnect} {
		if out := rpc(method, map[string]any{}); out.Error != nil {
			t.Fatalf("%s: %+v", method, out.Error)
		}
	}
	if out := rpc(api.MethodRobotConnect, api.RobotConnectParams{Host: "127.0.0.1"}); out.Error != nil {
		t.Fatal(out.Error)
	}
	var frame bytes.Buffer
	if err = png.Encode(&frame, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	// Use the actual browser blob-upload endpoint before robot.frame.
	upload, err := http.Post("http://"+addr+"/blobs", "image/png", bytes.NewReader(frame.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var uploaded map[string]string
	err = json.NewDecoder(upload.Body).Decode(&uploaded)
	upload.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(frame.Bytes())
	ref := hex.EncodeToString(sum[:])
	if uploaded["blob_digest"] != ref {
		t.Fatal(uploaded)
	}
	if out := rpc(api.MethodRobotFrame, api.RobotFrameParams{Blob: ref, Width: 2, Height: 3}); out.Error != nil {
		t.Fatal(out.Error)
	}
	for _, call := range []struct {
		method string
		params any
	}{
		{api.MethodRobotCalibrationStatus, struct{}{}},
		{api.MethodRobotCalibrationSetTableZ, map[string]bool{"use_current": true}},
		{api.MethodRobotCalibrationAddPoint, api.RobotCalibrationAddPointParams{Pixel: []float64{0, 0}}},
		{api.MethodRobotCalibrationClear, struct{}{}},
		{api.MethodRobotCalibrationSet, api.RobotCalibrationSetParams{
			TableZ: func() *float64 { z := .12; return &z }(), Image: api.RobotImageSize{Width: 2, Height: 3},
			Points: []api.RobotCalibrationPoint{
				{Pixel: []float64{0, 0}, Base: []float64{.1, .2}},
				{Pixel: []float64{1, 0}, Base: []float64{.2, .2}},
				{Pixel: []float64{1, 2}, Base: []float64{.2, .4}},
				{Pixel: []float64{0, 2}, Base: []float64{.1, .4}},
			},
		}},
	} {
		if out := rpc(call.method, call.params); out.Error != nil {
			t.Fatalf("%s: %+v", call.method, out.Error)
		}
	}
	out := rpc(api.MethodRobotCalibrationStatus, struct{}{})
	var calibration api.RobotCalibrationStatus
	if err := json.Unmarshal(out.Result, &calibration); err != nil || !calibration.Calibrated {
		t.Fatalf("calibration gateway round trip: %+v %v", out, err)
	}
	if out := rpc(api.MethodRobotConnect, map[string]string{"host": "invalid"}); out.Error == nil || out.Error.Code != api.CodeInvalidParams {
		t.Fatalf("invalid connect: %+v", out)
	}
	_, _, err = b.ConversationSend(ctx, "client", "robot-msg", "cnv_robot", "yes")
	if err != nil {
		t.Fatal(err)
	}
	history := awaitAssistant(t, b, "cnv_robot", 1)
	if history[len(history)-1].Text != "I can see the gripper. STOP is available." || calls != 2 {
		t.Fatalf("history=%+v calls=%d", history, calls)
	}
}

func TestRobotNotExposedWhenDisabled(t *testing.T) {
	s, err := Assemble(AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Tools.Get("robot"); err == nil {
		t.Fatal("disabled robot registered")
	}
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{})
	if _, err = b.Passthrough(context.Background(), api.MethodRobotStatus, json.RawMessage(`{}`)); err == nil {
		t.Fatal("disabled RPC hid missing service")
	}
}

func TestRobotSupervisorJoinsDaemonLifecycle(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true,"version":"test"}`) }))
	defer bridge.Close()
	service := robot.New(robot.Options{URL: bridge.URL, External: true})
	data := t.TempDir()
	d := New(Config{DataDir: data}, Deps{Services: &Services{Robot: service}})
	if _, err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Op(context.Background(), api.MethodRobotCalibrationSetTableZ, json.RawMessage(`{"z":0.12}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(data, "robot", "calibration.json")); err != nil {
		t.Fatalf("daemon did not bind calibration storage: %v", err)
	}
	select {
	case <-service.Done():
		t.Fatal("supervisor exited before shutdown")
	default:
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-service.Done():
	default:
		t.Fatal("daemon leaked robot supervisor")
	}
}
