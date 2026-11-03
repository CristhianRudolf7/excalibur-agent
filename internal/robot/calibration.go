package robot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// calibrationData persists observations only; fits are always recomputed on load.
type calibrationData struct {
	Points []api.RobotCalibrationPoint `json:"points"`
	TableZ *float64                    `json:"table_z"`
	Image  api.RobotImageSize          `json:"image"`
}

func cloneCalibration(c calibrationData) calibrationData {
	c.Points = append([]api.RobotCalibrationPoint{}, c.Points...)
	for i := range c.Points {
		c.Points[i].Pixel = append([]float64{}, c.Points[i].Pixel...)
		c.Points[i].Base = append([]float64{}, c.Points[i].Base...)
	}
	if c.TableZ != nil {
		z := *c.TableZ
		c.TableZ = &z
	}
	return c
}

// BindDataDir binds durable storage before serving robot RPCs. Empty keeps an
// isolated in-memory service for embedded tests. Corrupt files fail closed.
func (s *Service) BindDataDir(dataDir string) error {
	if dataDir == "" {
		return nil
	}
	s.calMu.Lock()
	defer s.calMu.Unlock()
	path := filepath.Join(dataDir, "robot", "calibration.json")
	if s.calPath == path {
		return nil
	}
	if s.calPath != "" {
		return errors.New("robot calibration data directory already bound")
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read robot calibration: %w", err)
	}
	c := calibrationData{}
	if err == nil {
		if err = decode(data, &c); err != nil {
			return fmt.Errorf("read robot calibration: %w", err)
		}
		if _, err = validateCalibration(c, false); err != nil {
			return fmt.Errorf("read robot calibration: %w", err)
		}
	}
	s.calPath, s.calibration = path, c
	return nil
}

func validateImage(size api.RobotImageSize) error {
	if size.Width <= 0 || size.Height <= 0 || size.Width > 4096 || size.Height > 4096 || size.Width*size.Height > 12_000_000 {
		return errors.New("invalid calibration image dimensions")
	}
	return nil
}
func validatePixel(pixel []float64, size api.RobotImageSize) error {
	if len(pixel) != 2 || !finite(pixel[0]) || !finite(pixel[1]) {
		return errors.New("pixel must contain two finite coordinates [u,v]")
	}
	if pixel[0] < 0 || pixel[1] < 0 || pixel[0] >= float64(size.Width) || pixel[1] >= float64(size.Height) {
		return errors.New("pixel is outside the calibration image")
	}
	return nil
}
func validateCalibration(c calibrationData, complete bool) (api.RobotCalibrationStatus, error) {
	c = cloneCalibration(c)
	status := api.RobotCalibrationStatus{Points: append([]api.RobotCalibrationPoint{}, c.Points...), Image: c.Image}
	if c.TableZ != nil {
		if !finite(*c.TableZ) {
			return status, errors.New("table_z must be finite")
		}
		status.TableZ = *c.TableZ
	}
	if len(c.Points) > 1024 {
		return status, errors.New("calibration supports at most 1024 points")
	}
	if len(c.Points) > 0 || c.Image != (api.RobotImageSize{}) {
		if err := validateImage(c.Image); err != nil {
			return status, err
		}
	}
	for _, p := range c.Points {
		if err := validatePixel(p.Pixel, c.Image); err != nil {
			return status, err
		}
		if len(p.Base) != 2 || !finite(p.Base[0]) || !finite(p.Base[1]) {
			return status, errors.New("base must contain two finite coordinates [x,y]")
		}
	}
	if complete && (len(c.Points) < 4 || c.TableZ == nil) {
		return status, errors.New("calibration requires at least four points and table_z")
	}
	if len(c.Points) >= 4 {
		_, rms, err := fitHomography(c.Points)
		if err != nil {
			return status, err
		}
		status.ReprojectionErrorPX = rms
		status.Calibrated = c.TableZ != nil
	}
	return status, nil
}

// CalibrationStatus never starts Python or contacts an arm.
func (s *Service) CalibrationStatus() api.RobotCalibrationStatus {
	s.calMu.Lock()
	defer s.calMu.Unlock()
	// Detach nested coordinate slices so callers cannot change stored calibration.
	c := cloneCalibration(s.calibration)
	status, err := validateCalibration(c, false)
	if err != nil {
		status.Error = err.Error()
	} else if len(c.Points) < 4 {
		status.Error = "calibration needs at least four correspondences"
	} else if c.TableZ == nil {
		status.Error = "set table_z before using locate"
	}
	return status
}

// changeCalibration serializes read/modify/write and atomically replaces the
// durable file before publishing. Failed writes leave the old fit untouched.
func (s *Service) changeCalibration(change func(*calibrationData) error) (api.RobotCalibrationStatus, error) {
	s.calMu.Lock()
	defer s.calMu.Unlock()
	c := cloneCalibration(s.calibration)
	if err := change(&c); err != nil {
		return api.RobotCalibrationStatus{}, err
	}
	status, err := validateCalibration(c, false)
	if err != nil {
		return status, err
	}
	if s.calPath != "" {
		dir := filepath.Dir(s.calPath)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return status, err
		}
		data, marshalErr := json.MarshalIndent(c, "", "  ")
		if marshalErr != nil {
			return status, marshalErr
		}
		file, createErr := os.CreateTemp(dir, ".calibration-*")
		if createErr != nil {
			return status, createErr
		}
		defer os.Remove(file.Name())
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		err = errors.Join(err, file.Close())
		if err != nil {
			return status, err
		}
		if err = os.Rename(file.Name(), s.calPath); err != nil {
			return status, err
		}
	}
	s.calibration = c
	if !status.Calibrated {
		if len(c.Points) < 4 {
			status.Error = "calibration needs at least four correspondences"
		} else {
			status.Error = "set table_z before using locate"
		}
	}
	if s.opts.Bus != nil {
		s.mu.Lock()
		var state State
		if s.state != nil {
			state = *s.state
		}
		s.mu.Unlock()
		_, _ = s.opts.Bus.Publish(api.EventRobotState, nil, nil, api.RobotStateEvent{State: state, Calibration: &status})
	}
	return status, nil
}

func (s *Service) currentTCP(ctx context.Context) ([]float64, error) {
	state, err := s.State(ctx)
	if err != nil {
		return nil, fmt.Errorf("read current TCP: %w", err)
	}
	if !state.Connected || len(state.TCPPose) != 6 {
		return nil, errors.New("current TCP requires a connected robot with a six-coordinate pose")
	}
	for _, x := range state.TCPPose {
		if !finite(x) {
			return nil, errors.New("current TCP pose must be finite")
		}
	}
	return state.TCPPose, nil
}

func (s *Service) calibrationOp(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	invalid := func(err error) (any, error) {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	switch method {
	case api.MethodRobotCalibrationStatus, api.MethodRobotCalibrationClear:
		if err := decode(raw, &struct{}{}); err != nil {
			return invalid(err)
		}
		if method == api.MethodRobotCalibrationStatus {
			return s.CalibrationStatus(), nil
		}
		return s.changeCalibration(func(c *calibrationData) error { *c = calibrationData{}; return nil })
	case api.MethodRobotCalibrationSet:
		var p api.RobotCalibrationSetParams
		if err := decode(raw, &p); err != nil {
			return invalid(err)
		}
		c := calibrationData{Points: p.Points, TableZ: p.TableZ, Image: p.Image}
		if _, err := validateCalibration(c, true); err != nil {
			return invalid(err)
		}
		return s.changeCalibration(func(out *calibrationData) error { *out = c; return nil })
	case api.MethodRobotCalibrationAddPoint:
		var p api.RobotCalibrationAddPointParams
		if err := decode(raw, &p); err != nil {
			return invalid(err)
		}
		s.mu.Lock()
		var size api.RobotImageSize
		if s.camera != nil {
			size = api.RobotImageSize{Width: s.camera.Width, Height: s.camera.Height}
		}
		s.mu.Unlock()
		if err := validateImage(size); err != nil {
			return invalid(errors.New("share a camera frame before adding calibration points"))
		}
		if err := validatePixel(p.Pixel, size); err != nil {
			return invalid(err)
		}
		pose, err := s.currentTCP(ctx)
		if err != nil {
			return nil, err
		}
		status, err := s.changeCalibration(func(c *calibrationData) error {
			if len(c.Points) > 0 && c.Image != size {
				return errors.New("camera frame size differs from calibration; clear and recalibrate")
			}
			c.Image = size
			c.Points = append(c.Points, api.RobotCalibrationPoint{Pixel: p.Pixel, Base: append([]float64{}, pose[:2]...)})
			return nil
		})
		if err != nil {
			return invalid(err)
		}
		return status, nil
	case api.MethodRobotCalibrationSetTableZ:
		var p api.RobotCalibrationTableZParams
		if err := decode(raw, &p); err != nil {
			return invalid(err)
		}
		if (p.Z == nil && !p.UseCurrent) || (p.Z != nil && p.UseCurrent) {
			return invalid(errors.New("provide z or use_current:true, exclusively"))
		}
		if p.UseCurrent {
			pose, err := s.currentTCP(ctx)
			if err != nil {
				return nil, err
			}
			p.Z = &pose[2]
		}
		if !finite(*p.Z) {
			return invalid(errors.New("z must be finite"))
		}
		return s.changeCalibration(func(c *calibrationData) error { c.TableZ = p.Z; return nil })
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown robot calibration method"}
	}
}

// LocateResult contains a base position plus copyable six-coordinate poses.
type LocateResult struct {
	Pixel        []float64  `json:"pixel"`
	Base         [3]float64 `json:"base"`
	Orientation  [3]float64 `json:"orientation"`
	Pose         [6]float64 `json:"pose"`
	ApproachPose [6]float64 `json:"approach_pose"`
}

func (s *Service) Locate(ctx context.Context, pixel []float64) (LocateResult, error) {
	var out LocateResult
	status := s.CalibrationStatus()
	if !status.Calibrated {
		return out, fmt.Errorf("robot camera uncalibrated: %s", status.Error)
	}
	s.mu.Lock()
	camera := s.camera
	s.mu.Unlock()
	if camera == nil {
		return out, errors.New("no camera frame; call look first")
	}
	if camera.Width != status.Image.Width || camera.Height != status.Image.Height {
		return out, errors.New("camera frame size differs from calibration; recalibrate for this image size")
	}
	if err := validatePixel(pixel, status.Image); err != nil {
		return out, err
	}
	h, _, err := fitHomography(status.Points)
	if err != nil {
		return out, err
	}
	xy, err := h.project(pixel)
	if err != nil {
		return out, err
	}
	pose, err := s.currentTCP(ctx)
	if err != nil {
		return out, err
	}
	out.Pixel = append([]float64{}, pixel...)
	out.Base = [3]float64{xy[0], xy[1], status.TableZ}
	out.Orientation = [3]float64{pose[3], pose[4], pose[5]}
	out.Pose = [6]float64{xy[0], xy[1], status.TableZ, pose[3], pose[4], pose[5]}
	out.ApproachPose = out.Pose
	out.ApproachPose[2] += .10
	return out, nil
}
