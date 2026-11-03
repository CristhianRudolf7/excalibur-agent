package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Service is shared by client routes and the deferred robot_lab tool.
// Set VisionStep during composition before serving requests.
type Service struct {
	Sidecar     Sidecar
	Bus         *telemetry.Bus
	Installer   *Installer
	VisionStep  func(ctx context.Context, session, instruction string) (any, error)
	unsubscribe func()
	closeOnce   sync.Once
}

func NewService(sidecar Sidecar, bus *telemetry.Bus) *Service {
	s := &Service{Sidecar: sidecar, Bus: bus}
	if sidecar != nil && Enabled() {
		s.unsubscribe = sidecar.Subscribe(func(e Event) {
			switch e.Name {
			case "frame", "metrics", "experiment", "log", "teleop":
				s.Publish("robotlab."+e.Name, e.Data)
			}
		})
	}
	return s
}
func NewRuntimeService(dataDir string, bus *telemetry.Bus) *Service {
	s := NewService(NewSidecar(dataDir), bus)
	s.Installer = &Installer{DataDir: dataDir, Publish: s.Publish}
	return s
}

// Publish sends a client event. Frames use the live-only bus path: no replay or sink.
func (s *Service) Publish(event string, payload any) {
	if s == nil || s.Bus == nil || !Enabled() {
		return
	}
	if !strings.HasPrefix(event, "robotlab.") {
		return
	}
	if event == "robotlab.frame" {
		_ = s.Bus.PublishEphemeral(types.EventType(event), payload)
		return
	}
	_, _ = s.Bus.Publish(types.EventType(event), nil, nil, payload)
}
func (s *Service) Close() error {
	var err error
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.Installer != nil {
			s.Installer.Close()
		}
		if s.unsubscribe != nil {
			s.unsubscribe()
		}
		if c, ok := s.Sidecar.(io.Closer); ok {
			err = c.Close()
		}
	})
	return err
}

// Op validates every public Robot Lab method before invoking the runtime.
func (s *Service) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, mapError(err)
	}
	if method == "robotlab.status" {
		if _, err := decodeParams(method, raw); err != nil {
			return nil, err
		}
		if s == nil || s.Sidecar == nil {
			return Status{Enabled: Enabled(), Requirements: Requirements{Python: ">=3.10", OSMesa: detectOSMesa()}}, nil
		}
		return s.Sidecar.Status(ctx), nil
	}
	if !Enabled() || s == nil || s.Sidecar == nil {
		return nil, unavailable("Robot Lab is unavailable")
	}
	params, err := decodeParams(method, raw)
	if err != nil {
		return nil, err
	}
	if method == "robotlab.install" {
		if s.Installer == nil {
			return nil, unavailable("Robot Lab installer is unavailable")
		}
		if err := s.Installer.Start(); err != nil {
			return nil, mapError(err)
		}
		return map[string]bool{"started": true}, nil
	}
	if method == "robotlab.vision.step" {
		v := params.(*api.RobotLabVisionStepParams)
		if s.VisionStep == nil {
			return nil, unavailable("Robot Lab vision is unavailable")
		}
		result, err := s.VisionStep(ctx, v.Session, v.Instruction)
		return result, mapError(err)
	}
	target := strings.TrimPrefix(method, "robotlab.")
	switch target {
	case "scenarios":
		target = "scenarios.list"
	case "session.params":
		target = "session.set_params"
	case "session.policy":
		target = "session.load_policy"
	case "experiment.code":
		target = "experiment.write_code"
	case "teleop.get":
		target = "teleop.get_mapping"
	case "teleop.mapping":
		target = "teleop.set_mapping"
	}
	result, err := s.Sidecar.Call(ctx, target, params)
	if err != nil {
		return nil, mapError(err)
	}
	return result, nil
}
func unavailable(msg string) error {
	return &api.Error{Code: api.CodeRobotLabUnavailable, Message: msg}
}
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae
	}
	code := api.CodeInternal
	var re *RuntimeError
	if errors.As(err, &re) {
		switch re.Code {
		case "invalid_params":
			code = api.CodeInvalidParams
		case "not_found":
			code = api.CodeUnknownTarget
		case "conflict":
			code = api.CodeConflict
		case "unavailable":
			code = api.CodeRobotLabUnavailable
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errors.ErrUnsupported) {
		code = api.CodeRobotLabUnavailable
	}
	return &api.Error{Code: code, Message: err.Error()}
}
