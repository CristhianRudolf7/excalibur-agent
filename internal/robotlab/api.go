package robotlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"io"
	"strings"
)

func invalid(message string) error { return &api.Error{Code: api.CodeInvalidParams, Message: message} }
func decodeParams(method string, raw json.RawMessage) (any, error) {
	var p any
	var required []string
	switch method {
	case "robotlab.status":
		p = &api.RobotLabEmptyParams{}
		required = []string{}
	case "robotlab.install":
		p = &api.RobotLabEmptyParams{}
		required = []string{}
	case "robotlab.scenarios":
		p = &api.RobotLabEmptyParams{}
		required = []string{}
	case "robotlab.session.open":
		p = &api.RobotLabSessionOpenParams{}
		required = []string{"scenario"}
	case "robotlab.session.close":
		p = &api.RobotLabSessionParams{}
		required = []string{"session"}
	case "robotlab.session.reset":
		p = &api.RobotLabSessionResetParams{}
		required = []string{"session"}
	case "robotlab.session.params":
		p = &api.RobotLabSessionSetParams{}
		required = []string{"session", "params"}
	case "robotlab.session.control":
		p = &api.RobotLabSessionControlParams{}
		required = []string{"session", "mode"}
	case "robotlab.session.policy":
		p = &api.RobotLabSessionPolicyParams{}
		required = []string{"session"}
	case "robotlab.session.snapshot":
		p = &api.RobotLabSessionSnapshotParams{}
		required = []string{"session"}
	case "robotlab.experiment.list":
		p = &api.RobotLabEmptyParams{}
		required = []string{}
	case "robotlab.experiment.get":
		p = &api.RobotLabExperimentParams{}
		required = []string{"experiment"}
	case "robotlab.experiment.create":
		p = &api.RobotLabExperimentCreateParams{}
		required = []string{"name", "scenario", "runs"}
	case "robotlab.experiment.code":
		p = &api.RobotLabExperimentCodeParams{}
		required = []string{"experiment", "code"}
	case "robotlab.experiment.start":
		p = &api.RobotLabExperimentParams{}
		required = []string{"experiment"}
	case "robotlab.experiment.stop":
		p = &api.RobotLabExperimentParams{}
		required = []string{"experiment"}
	case "robotlab.experiment.metrics":
		p = &api.RobotLabExperimentMetricsParams{}
		required = []string{"experiment"}
	case "robotlab.teleop.get":
		p = &api.RobotLabSessionParams{}
		required = []string{"session"}
	case "robotlab.teleop.mapping":
		p = &api.RobotLabTeleopMappingParams{}
		required = []string{"session"}
	case "robotlab.vision.step":
		p = &api.RobotLabVisionStepParams{}
		required = []string{"session", "instruction"}
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown Robot Lab method"}
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 2<<20 {
		return nil, invalid("parameters exceed 2 MiB")
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return nil, invalid("parameters must be an object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(p); err != nil {
		return nil, invalid(err.Error())
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return nil, invalid("unexpected trailing JSON")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	if pose := fields["pose"]; len(pose) > 64<<10 {
		return nil, invalid("pose exceeds 64 KiB")
	}
	for _, key := range required {
		v, ok := fields[key]
		if !ok || string(v) == "null" {
			return nil, invalid(fmt.Sprintf("%s required", key))
		}
		if len(v) > 0 && v[0] == '"' {
			var value string
			_ = json.Unmarshal(v, &value)
			if strings.TrimSpace(value) == "" && key != "code" {
				return nil, invalid(key + " required")
			}
		}
	}
	if err := validateParams(p); err != nil {
		return nil, err
	}
	return p, nil
}

func validateParams(p any) error {
	checkCode := func(code string) error {
		if len(code) > 256<<10 {
			return invalid("code exceeds 256 KiB")
		}
		return nil
	}
	switch v := p.(type) {
	case *api.RobotLabSessionControlParams:
		switch v.Mode {
		case "idle", "manual", "policy", "pose", "paused":
		default:
			return invalid("invalid control mode")
		}
		for _, n := range v.Inputs {
			if n < -1 || n > 1 {
				return invalid("inputs must be in [-1,1]")
			}
		}
		if v.Pose != nil {
			b, _ := json.Marshal(v.Pose)
			if len(b) > 64<<10 {
				return invalid("pose exceeds 64 KiB")
			}
			if err := validatePose(v.Pose); err != nil {
				return err
			}
		}
	case *api.RobotLabSessionPolicyParams:
		if v.Pretrained {
			if v.Experiment != "" || v.Run != "" {
				return invalid("pretrained and experiment policy are mutually exclusive")
			}
		} else if v.Experiment == "" || v.Run == "" {
			return invalid("experiment and run required")
		}
		if v.Checkpoint != "" && v.Checkpoint != "best" && v.Checkpoint != "last" {
			return invalid("checkpoint must be best or last")
		}
	case *api.RobotLabSessionSnapshotParams:
		if v.Width < 0 || v.Height < 0 || v.Width > 4096 || v.Height > 4096 {
			return invalid("snapshot dimensions must be between 1 and 4096")
		}
	case *api.RobotLabSessionSetParams:
		if v.Params == nil {
			return invalid("params must be an object")
		}
	case *api.RobotLabExperimentCodeParams:
		return checkCode(v.Code)
	case *api.RobotLabTeleopMappingParams:
		if v.Code != nil {
			return checkCode(*v.Code)
		}
	case *api.RobotLabExperimentCreateParams:
		if len(v.Runs) == 0 || len(v.Runs) > 32 {
			return invalid("runs must contain 1 to 32 entries")
		}
		if v.TotalSteps != nil && *v.TotalSteps <= 0 {
			return invalid("total_steps must be positive")
		}
		for _, r := range v.Runs {
			if strings.TrimSpace(r.Label) == "" || strings.TrimSpace(r.Algorithm) == "" {
				return invalid("run label and algorithm required")
			}
		}
		if v.Code != nil {
			return checkCode(*v.Code)
		}
	case *api.RobotLabExperimentMetricsParams:
		if v.Since != nil && *v.Since < 0 {
			return invalid("since must be nonnegative")
		}
	}
	return nil
}
func validatePose(p *api.RobotLabPose) error {
	points := func(v [][]float64, count, width int) bool {
		if len(v) != count {
			return false
		}
		for _, row := range v {
			if len(row) != width {
				return false
			}
		}
		return true
	}
	if !points(p.Landmarks, 33, 4) {
		return invalid("pose requires 33 landmarks of four coordinates")
	}
	if p.World != nil && !points(p.World, 33, 3) {
		return invalid("pose world requires 33 landmarks of three coordinates")
	}
	if p.Arm != "" && p.Arm != "left" && p.Arm != "right" {
		return invalid("pose arm must be left or right")
	}
	if len(p.Hands) > 2 {
		return invalid("pose supports at most two hands")
	}
	for _, h := range p.Hands {
		if (h.Handedness != "Left" && h.Handedness != "Right") || !points(h.Landmarks, 21, 3) {
			return invalid("invalid hand landmarks")
		}
	}
	if p.Calibration != nil {
		if !points(p.Calibration.Landmarks, 33, 4) || (p.Calibration.World != nil && !points(p.Calibration.World, 33, 3)) {
			return invalid("invalid calibration landmarks")
		}
	}
	return nil
}
