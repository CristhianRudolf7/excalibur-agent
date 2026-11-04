package robotlab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Service is shared by client routes and the deferred robot_lab tool.
// VisionStep is an inert compatibility hook for existing composition callers.
type Service struct {
	DataDir     string
	Sidecar     Sidecar
	Bus         *telemetry.Bus
	Installer   *Installer
	VisionStep  func(ctx context.Context, session, instruction string) (any, error)
	unsubscribe func()
	closeOnce   sync.Once
	turnMu      sync.Mutex
	turnStarts  map[string]turnStart
}

type turnStart struct {
	done <-chan struct{}
	at   time.Time
}

// RecordTurnStart marks the lab turn before execution. Repeated inputs drained
// under the same parent turn retain the first start, so newer user input wins.
func (s *Service) RecordTurnStart(ctx context.Context, conversation string) {
	if s == nil || ctx.Done() == nil {
		return
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.turnStarts == nil {
		s.turnStarts = map[string]turnStart{}
	}
	if previous, ok := s.turnStarts[conversation]; !ok || previous.done != ctx.Done() {
		s.turnStarts[conversation] = turnStart{done: ctx.Done(), at: time.Now()}
	}
}

// TurnStartedAt returns the matching parent turn's start, or zero for callers
// outside the daemon's turn-budget hook. Only the latest turn per conversation
// is retained.
func (s *Service) TurnStartedAt(ctx context.Context, conversation string) time.Time {
	if s == nil {
		return time.Time{}
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if start, ok := s.turnStarts[conversation]; ok && start.done == ctx.Done() {
		return start.at
	}
	return time.Time{}
}

func NewService(sidecar Sidecar, bus *telemetry.Bus) *Service {
	s := &Service{Sidecar: sidecar, Bus: bus, VisionStep: (&VisionLoop{}).VisionStep}
	if sidecar != nil && Enabled() {
		s.unsubscribe = sidecar.Subscribe(func(e Event) {
			switch e.Name {
			case "frame", "session", "log":
				s.Publish("robotlab."+e.Name, e.Data)
			}
		})
	}
	return s
}
func NewRuntimeService(dataDir string, bus *telemetry.Bus) *Service {
	if absolute, err := filepath.Abs(dataDir); err == nil {
		dataDir = absolute
	}
	s := NewService(NewSidecar(dataDir), bus)
	s.DataDir = dataDir
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

// Op validates every public Robot Lab method against the v3 contract.
func (s *Service) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, mapError(err)
	}
	if method != "robotlab.status" && (!Enabled() || s == nil || s.Sidecar == nil) {
		return nil, unavailable("Robot Lab is unavailable")
	}
	params, err := decodeParams(method, raw)
	if err != nil {
		return nil, err
	}
	if method == "robotlab.status" {
		status := Status{Enabled: Enabled(), Requirements: Requirements{Python: ">=3.10", OSMesa: detectOSMesa()}}
		if s == nil || s.Sidecar == nil {
			return status, nil
		}
		status = s.Sidecar.Status(ctx)
		status.Enabled = Enabled()
		if status.Enabled && status.Installed {
			result, callErr := s.Sidecar.Call(ctx, "status", params)
			if callErr != nil {
				status.Error = callErr.Error()
				return status, nil
			}
			var runtimeStatus struct {
				Version      string  `json:"version"`
				LiveSession  *string `json:"live_session"`
				Robots       int     `json:"robots"`
				Environments int     `json:"environments"`
			}
			if err := json.Unmarshal(result, &runtimeStatus); err != nil {
				return nil, mapError(err)
			}
			status.Version, status.LiveSession = runtimeStatus.Version, runtimeStatus.LiveSession
			status.Robots, status.Environments = runtimeStatus.Robots, runtimeStatus.Environments
			status.Running = true
		}
		return status, nil
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
	result, err := s.Sidecar.Call(ctx, strings.TrimPrefix(method, "robotlab."), params)
	return result, mapError(err)
}

// SessionState is the minimal training state needed by daemon continuation.
type SessionState struct {
	ID           string `json:"id"`
	Conversation string `json:"conversation"`
	Status       string `json:"status"`
	Attempt      int    `json:"attempt"`
	MaxAttempts  int    `json:"max_attempts"`
}

// LiveSession reads current runtime truth; stopped sessions never cause relays.
func (s *Service) LiveSession(ctx context.Context, conversation string) (*SessionState, error) {
	if s == nil || s.Sidecar == nil || !Enabled() || conversation == "" || !s.Sidecar.Status(ctx).Installed {
		return nil, nil
	}
	raw, err := s.Sidecar.Call(ctx, "status", map[string]any{})
	if err != nil {
		return nil, err
	}
	var status struct {
		LiveSession *string `json:"live_session"`
	}
	if err = json.Unmarshal(raw, &status); err != nil || status.LiveSession == nil {
		return nil, err
	}
	raw, err = s.Sidecar.Call(ctx, "session.get", map[string]any{"session": *status.LiveSession})
	if err != nil {
		return nil, err
	}
	var result struct {
		Session SessionState `json:"session"`
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	state := &result.Session
	if state.ID != *status.LiveSession || state.Status != "live" || state.Conversation != conversation {
		return nil, nil
	}
	return state, nil
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
