package robot

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// Op dispatches the five additive daemon RPCs.
func (s *Service) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if s == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "robot disabled or unavailable"}
	}
	invalid := func(err error) (any, error) {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	switch method {
	case api.MethodRobotConnect:
		var p api.RobotConnectParams
		if err := decode(raw, &p); err != nil {
			return invalid(err)
		}
		if net.ParseIP(p.Host) == nil {
			return invalid(errors.New("host must be a robot IP address"))
		}
		return s.Connect(ctx, p.Host)
	case api.MethodRobotFrame:
		var p api.RobotFrameParams
		if err := decode(raw, &p); err != nil {
			return invalid(err)
		}
		if err := s.Frame(p.Blob, p.Width, p.Height); err != nil {
			return invalid(err)
		}
		return api.RobotOK{OK: true}, nil
	case api.MethodRobotStatus, api.MethodRobotDisconnect, api.MethodRobotStop:
		if err := decode(raw, &struct{}{}); err != nil {
			return invalid(err)
		}
		if method == api.MethodRobotStatus {
			return s.Status(ctx), nil
		}
		if method == api.MethodRobotStop {
			result, err := s.Stop(ctx)
			return result, err
		}
		_, err := s.Call(ctx, http.MethodPost, "/disconnect", map[string]any{})
		return s.Status(ctx), err
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown robot method"}
	}
}
