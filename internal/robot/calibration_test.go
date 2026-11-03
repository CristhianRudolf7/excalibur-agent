package robot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func smallCalibration() api.RobotCalibrationSetParams {
	z := .12
	return api.RobotCalibrationSetParams{Image: api.RobotImageSize{Width: 2, Height: 3}, TableZ: &z, Points: []api.RobotCalibrationPoint{
		{Pixel: []float64{0, 0}, Base: []float64{.1, .2}},
		{Pixel: []float64{1, 0}, Base: []float64{.2, .2}},
		{Pixel: []float64{1, 2}, Base: []float64{.2, .4}},
		{Pixel: []float64{0, 2}, Base: []float64{.1, .4}},
	}}
}
func calibrationRPC(t *testing.T, s *Service, method string, p any) api.RobotCalibrationStatus {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Op(context.Background(), method, raw)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return out.(api.RobotCalibrationStatus)
}
func TestCalibrationRPCPersistenceAndEvents(t *testing.T) {
	s, _ := fixture(t)
	data := t.TempDir()
	if err := s.BindDataDir(data); err != nil {
		t.Fatal(err)
	}
	if got := calibrationRPC(t, s, api.MethodRobotCalibrationStatus, struct{}{}); got.Calibrated || len(got.Points) != 0 {
		t.Fatal(got)
	}
	got := calibrationRPC(t, s, api.MethodRobotCalibrationSet, smallCalibration())
	if !got.Calibrated || got.ReprojectionErrorPX > 1e-8 {
		t.Fatal(got)
	}
	file := filepath.Join(data, "robot", "calibration.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	restored := New(Options{})
	if err := restored.BindDataDir(data); err != nil {
		t.Fatal(err)
	}
	if got := restored.CalibrationStatus(); !got.Calibrated || got.TableZ != .12 {
		t.Fatal(got)
	}
	// Returned observations do not alias service storage.
	got.Points[0].Pixel[0] = 999
	if s.CalibrationStatus().Points[0].Pixel[0] != 0 {
		t.Fatal("mutable status")
	}
	got = calibrationRPC(t, s, api.MethodRobotCalibrationSetTableZ, map[string]any{"z": .18})
	if got.TableZ != .18 {
		t.Fatal(got)
	}
	events := s.opts.Bus.Replay(types.Cursor{})
	count := 0
	for _, e := range events {
		if e.Type != api.EventRobotState {
			continue
		}
		var event api.RobotStateEvent
		if err := json.Unmarshal(e.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Calibration != nil {
			if !event.Calibration.Calibrated {
				t.Fatalf("%s", e.Payload)
			}
			count++
		}
	}
	if count != 2 {
		t.Fatalf("calibration events %d", count)
	}
	got = calibrationRPC(t, s, api.MethodRobotCalibrationClear, struct{}{})
	if got.Calibrated || len(got.Points) != 0 || got.Image.Width != 0 {
		t.Fatal(got)
	}
	restored = New(Options{})
	if err := restored.BindDataDir(data); err != nil || restored.CalibrationStatus().Calibrated {
		t.Fatalf("clear not durable: %v", err)
	}
}
func TestCalibrationCurrentTCPAndCollection(t *testing.T) {
	var current atomic.Pointer[State]
	current.Store(&State{Connected: true, TCPPose: []float64{.1, .2, .3, .4, .5, .6}})
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/state" {
			t.Errorf("unexpected bridge effect %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(current.Load())
	}))
	defer bridge.Close()
	s := New(Options{URL: bridge.URL})
	s.camera = &Camera{Width: 640, Height: 480}
	got := calibrationRPC(t, s, api.MethodRobotCalibrationSetTableZ, map[string]bool{"use_current": true})
	if got.TableZ != .3 {
		t.Fatal(got)
	}
	for i, p := range syntheticPoints(t, homography{{.001, 0, 0}, {0, .001, 0}, {0, 0, 1}}, 4, 0) {
		current.Store(&State{Connected: true, TCPPose: []float64{p.Base[0], p.Base[1], .3, .4, .5, .6}})
		got = calibrationRPC(t, s, api.MethodRobotCalibrationAddPoint, map[string]any{"pixel": p.Pixel})
		if got.Calibrated != (i == 3) || got.Points[i].Base[0] != p.Base[0] || got.Points[i].Base[1] != p.Base[1] {
			t.Fatal(got)
		}
	}
	s.camera = &Camera{Width: 320, Height: 240}
	if _, err := s.Op(context.Background(), api.MethodRobotCalibrationAddPoint, json.RawMessage(`{"pixel":[100,100]}`)); err == nil || !strings.Contains(err.Error(), "size differs") {
		t.Fatalf("accepted changed camera geometry: %v", err)
	}
	if len(s.CalibrationStatus().Points) != 4 {
		t.Fatal("failed point collection changed observations")
	}
	s.camera = &Camera{Width: 640, Height: 480}
	current.Store(&State{Connected: false, TCPPose: []float64{1, 2, 3, 4, 5, 6}})
	if _, err := s.Op(context.Background(), api.MethodRobotCalibrationSetTableZ, json.RawMessage(`{"use_current":true}`)); err == nil {
		t.Fatal("disconnected TCP accepted")
	}
	current.Store(&State{Connected: true, TCPPose: []float64{1, 2, 3}})
	if _, err := s.currentTCP(context.Background()); err == nil {
		t.Fatal("short TCP accepted")
	}
	current.Store(&State{Connected: true, TCPPose: []float64{1, 2, 3, 4, 5, math.NaN()}})
	if _, err := s.currentTCP(context.Background()); err == nil {
		t.Fatal("nonfinite TCP accepted")
	}
}
func TestCalibrationInvalidParamsAndAtomicFailure(t *testing.T) {
	s, _ := fixture(t)
	for _, test := range []struct{ method, raw string }{
		{api.MethodRobotCalibrationStatus, `{"extra":true}`},
		{api.MethodRobotCalibrationClear, `null`},
		{api.MethodRobotCalibrationAddPoint, `{"pixel":[1,2]}`}, // No frame.
		{api.MethodRobotCalibrationSetTableZ, `{}`},
		{api.MethodRobotCalibrationSetTableZ, `{"z":1,"use_current":true}`},
		{api.MethodRobotCalibrationSet, `{"points":[],"table_z":0,"image":{"width":2,"height":3}}`},
		{api.MethodRobotCalibrationSet, `{"points":[],"image":{"width":2,"height":3}}`},
	} {
		if _, err := s.Op(context.Background(), test.method, json.RawMessage(test.raw)); err == nil {
			t.Fatalf("accepted %s %s", test.method, test.raw)
		}
	}
	calibrationRPC(t, s, api.MethodRobotCalibrationSet, smallCalibration())
	for _, mutate := range []func(*api.RobotCalibrationSetParams){
		func(p *api.RobotCalibrationSetParams) { p.Image.Width = 0 },
		func(p *api.RobotCalibrationSetParams) { p.Points[0].Pixel = []float64{0, 1, 2} },
		func(p *api.RobotCalibrationSetParams) { p.Points[0].Pixel = []float64{-1, 0} },
		func(p *api.RobotCalibrationSetParams) { p.Points[0].Pixel = []float64{2, 0} },
		func(p *api.RobotCalibrationSetParams) { p.Points[0].Base = []float64{1} },
		func(p *api.RobotCalibrationSetParams) { p.Points[3] = p.Points[0] },
	} {
		p := smallCalibration()
		mutate(&p)
		raw, _ := json.Marshal(p)
		if _, err := s.Op(context.Background(), api.MethodRobotCalibrationSet, raw); err == nil {
			t.Fatalf("accepted invalid %s", raw)
		}
		if !s.CalibrationStatus().Calibrated {
			t.Fatal("invalid write destroyed calibration")
		}
	}
	// A file where a directory is needed forces a deterministic storage failure.
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.calPath = filepath.Join(blocker, "calibration.json")
	if _, err := s.Op(context.Background(), api.MethodRobotCalibrationClear, nil); err == nil {
		t.Fatal("storage failure hidden")
	}
	if !s.CalibrationStatus().Calibrated {
		t.Fatal("failed write changed calibration")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "robot"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "robot", "calibration.json"), []byte(`{"points":[`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := New(Options{}).BindDataDir(dir); err == nil {
		t.Fatal("corrupt calibration loaded")
	}
}
func TestLocateAndLookTool(t *testing.T) {
	s, f := fixture(t)
	tool := &Tool{Service: s}
	ctx := context.Background()
	invoke := func(raw string) (tools.Result, error) {
		return tool.Invoke(ctx, tools.Invocation{Arguments: json.RawMessage(raw)})
	}
	if _, err := invoke(`{"action":"locate","pixel":[0.5,1]}`); err == nil || !strings.Contains(err.Error(), "uncalibrated") {
		t.Fatal(err)
	}
	calibrationRPC(t, s, api.MethodRobotCalibrationSet, smallCalibration())
	if _, err := invoke(`{"action":"locate","pixel":[0.5,1]}`); err == nil || !strings.Contains(err.Error(), "no camera frame") {
		t.Fatal(err)
	}
	if err := s.Frame(uploadFrame(t, s), 2, 3); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.connected = true
	f.mu.Unlock()
	result, err := invoke(`{"action":"locate","pixel":[0.5,1]}`)
	if err != nil {
		t.Fatal(err)
	}
	var got LocateResult
	if err = json.Unmarshal([]byte(result.Output), &got); err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.Base[0]-.15) > 1e-9 || math.Abs(got.Base[1]-.3) > 1e-9 || got.Base[2] != .12 || got.ApproachPose[2] != .22 || got.Orientation != [3]float64{} {
		t.Fatal(got)
	}
	f.mu.Lock()
	for _, c := range f.calls {
		if c != "GET /state" {
			t.Errorf("locate caused effect: %s", c)
		}
	}
	f.mu.Unlock()
	for _, raw := range []string{`{"action":"locate"}`, `{"action":"locate","pixel":[1]}`, `{"action":"locate","pixel":[2,1]}`, `{"action":"locate","pixel":[-1,1]}`} {
		if _, err := invoke(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	s.mu.Lock()
	s.camera = &Camera{Width: 3, Height: 2}
	s.mu.Unlock()
	if _, err := invoke(`{"action":"locate","pixel":[0.5,1]}`); err == nil || !strings.Contains(err.Error(), "size differs") {
		t.Fatal(err)
	}
	if err := s.Frame(uploadFrame(t, s), 2, 3); err != nil {
		t.Fatal(err)
	}
	result, err = invoke(`{"action":"look"}`)
	if err != nil {
		t.Fatal(err)
	}
	var look LookResult
	if err = json.Unmarshal([]byte(result.Output), &look); err != nil {
		t.Fatal(err)
	}
	if !look.Calibration.Calibrated || look.Image.Width != 2 || look.Image.Height != 3 || !strings.Contains(look.Instruction, "pixel center") || len(result.Images) != 1 {
		t.Fatal(result)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(result.Images[0].URL, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil || image.Bounds().Dx() != 2 || image.Bounds().Dy() != 3 {
		t.Fatalf("%v %v", image, err)
	}
}
func TestPixelGridPreservesDimensionsAndObjectColors(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 640, 480))
	drawColor := color.RGBA{R: 240, G: 20, B: 10, A: 255}
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			src.SetRGBA(x, y, drawColor)
		}
	}
	grid := pixelGrid(src)
	if grid.Bounds() != src.Bounds() || grid.RGBAAt(150, 150) != drawColor {
		t.Fatal("grid changed dimensions or object interior")
	}
	if grid.RGBAAt(100, 150) == drawColor {
		t.Fatal("missing 100px grid line")
	}
	// Labels on all four edges alter multiple pixels beyond a grid line.
	for _, r := range []image.Rectangle{image.Rect(103, 0, 124, 14), image.Rect(103, 466, 124, 480), image.Rect(0, 103, 23, 117), image.Rect(616, 103, 640, 117)} {
		changed := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if grid.RGBAAt(x, y) != drawColor {
					changed++
				}
			}
		}
		if changed < 20 {
			t.Fatalf("missing edge label at %v", r)
		}
	}
}

func TestLocatePreservesCurrentOrientationAndRequiresTableHeight(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/state" || r.Method != http.MethodGet {
			t.Errorf("unexpected effect %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(State{Connected: true, TCPPose: []float64{.1, .2, .3, .4, -.5, 1.6}})
	}))
	defer bridge.Close()
	s := New(Options{URL: bridge.URL})
	s.camera = &Camera{Width: 2, Height: 3}
	p := smallCalibration()
	_, err := s.changeCalibration(func(c *calibrationData) error { *c = calibrationData{Points: p.Points, Image: p.Image}; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if status := s.CalibrationStatus(); status.Calibrated || !strings.Contains(status.Error, "table_z") {
		t.Fatal(status)
	}
	if _, err = s.Locate(context.Background(), []float64{.5, 1}); err == nil {
		t.Fatal("locate accepted missing table height")
	}
	calibrationRPC(t, s, api.MethodRobotCalibrationSetTableZ, map[string]bool{"use_current": true})
	located, err := s.Locate(context.Background(), []float64{.5, 1})
	if err != nil {
		t.Fatal(err)
	}
	want := [3]float64{.4, -.5, 1.6}
	if located.Orientation != want || located.Pose[3] != want[0] || located.Pose[4] != want[1] || located.Pose[5] != want[2] || located.ApproachPose[3] != want[0] || located.ApproachPose[4] != want[1] || located.ApproachPose[5] != want[2] || math.Abs(located.ApproachPose[2]-.4) > 1e-12 {
		t.Fatal(located)
	}
}

func TestCalibrationMethodRegistry(t *testing.T) {
	for _, name := range []string{api.MethodRobotCalibrationStatus, api.MethodRobotCalibrationAddPoint, api.MethodRobotCalibrationSetTableZ, api.MethodRobotCalibrationClear, api.MethodRobotCalibrationSet} {
		found := false
		for _, method := range api.Methods {
			if method.Name == name {
				found = method.Family == "robot" && method.Summary != ""
			}
		}
		if !found {
			t.Fatalf("missing calibration method %s", name)
		}
	}
}
