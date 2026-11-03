package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
)

type labRuntimeFake struct {
	calls  int
	closed bool
}

func (f *labRuntimeFake) Call(_ context.Context, m string, p any) (json.RawMessage, error) {
	f.calls++
	if m == "teleop.get_mapping" {
		return json.RawMessage(`{"source":"default","code":"def map_pose(pose): return {}"}`), nil
	}
	if m != "session.open" {
		return nil, &robotlab.RuntimeError{Code: "not_found", Message: "missing"}
	}
	return json.RawMessage(`{"session":"ses_test","scene":{}}`), nil
}
func (f *labRuntimeFake) Subscribe(func(robotlab.Event)) func() { return func() {} }
func (f *labRuntimeFake) Status(context.Context) robotlab.Status {
	return robotlab.Status{Enabled: robotlab.Enabled(), Requirements: robotlab.Requirements{Python: ">=3.10"}}
}
func (f *labRuntimeFake) Close() error { f.closed = true; return nil }
func TestRobotLabGatewayRoutingAndDisabled(t *testing.T) {
	bus := telemetry.NewBus(nil)
	runtime := &labRuntimeFake{}
	svc := robotlab.NewService(runtime, bus)
	defer svc.Close()
	backend := NewDaemonBackend(nil, nil, bus)
	backend.AttachServices(BackendServices{RobotLab: svc})
	server := httptest.NewServer(gateway.New(gateway.Config{}, backend, gateway.NewMemoryBlobs(), bus))
	defer server.Close()
	call := func(method, params string) api.Response {
		t.Helper()
		res, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"robotlab.`+method+`","params":`+params+`}`))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out api.Response
		if err = json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if res := call("status", "{}"); res.Error != nil {
		t.Fatal(res.Error)
	}
	if res := call("session.open", `{"scenario":"cartpole"}`); res.Error != nil {
		t.Fatal(res.Error)
	}
	if res := call("session.open", `{"scenario":"cartpole","unknown":1}`); res.Error == nil || res.Error.Code != api.CodeInvalidParams {
		t.Fatalf("invalid: %+v", res)
	}
	if res := call("teleop.get", `{"session":"ses_test"}`); res.Error != nil || !bytes.Contains(res.Result, []byte(`"source":"default"`)) || !bytes.Contains(res.Result, []byte(`"code":`)) {
		t.Fatalf("mapping: %+v", res)
	}
	if res := call("teleop.get", `{"session":"ses_test","unknown":1}`); res.Error == nil || res.Error.Code != api.CodeInvalidParams {
		t.Fatalf("mapping validation: %+v", res)
	}
	if res := call("experiment.get", `{"experiment":"missing"}`); res.Error == nil || res.Error.Code != api.CodeUnknownTarget {
		t.Fatalf("missing: %+v", res)
	}
	t.Setenv("EXCALIBUR_ROBOTLAB", "off")
	if res := call("scenarios", "{}"); res.Error == nil || res.Error.Code != api.CodeRobotLabUnavailable {
		t.Fatalf("disabled: %+v", res)
	}
	if runtime.calls != 3 {
		t.Fatalf("unexpected calls %d", runtime.calls)
	}
}
func TestRobotLabCompositionAndShutdown(t *testing.T) {
	bus := telemetry.NewBus(nil)
	svc, err := Assemble(AssembleOptions{DataDir: t.TempDir(), Bus: bus})
	if err != nil {
		t.Fatal(err)
	}
	if svc.RobotLab == nil {
		t.Fatal("service not composed")
	}
	st, err := svc.RobotLab.Op(context.Background(), "robotlab.status", nil)
	if err != nil || st.(robotlab.Status).Running {
		t.Fatalf("startup was not lazy: %v %v", st, err)
	}
	fake := &labRuntimeFake{}
	_ = svc.RobotLab.Close()
	svc.RobotLab = robotlab.NewService(fake, bus)
	d := New(DefaultConfig(), Deps{Services: svc})
	if err = d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.closed {
		t.Fatal("sidecar not stopped")
	}
}

func TestRobotLabParentContextStopsRuntime(t *testing.T) {
	runtime := &labRuntimeFake{}
	service := robotlab.NewService(runtime, nil)
	defer service.Close()
	d := New(DefaultConfig(), Deps{Services: &Services{RobotLab: service}})
	ctx, cancel := context.WithCancel(context.Background())
	done := d.startServices(ctx)
	if len(done) != 1 {
		t.Fatalf("runtime lifetime watcher missing: %d", len(done))
	}
	cancel()
	if !waitDone(done, time.Second) {
		t.Fatal("runtime shutdown did not join")
	}
	if !runtime.closed {
		t.Fatal("runtime survived parent cancellation")
	}
}
