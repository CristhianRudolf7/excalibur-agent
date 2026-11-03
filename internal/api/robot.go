package api

import "time"

// RobotConnectParams selects an arm reachable through the local network or VPN.
type RobotConnectParams struct {
	Host string `json:"host"`
}

// RobotFrameParams references a camera image uploaded through POST /blobs.
type RobotFrameParams struct {
	Blob   string `json:"blob"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// RobotOK acknowledges a completed operation.
type RobotOK struct {
	OK bool `json:"ok"`
}

// State mirrors the loopback bridge contract. Unknown modes fail closed there.
type RobotState struct {
	Connected        bool      `json:"connected"`
	Host             string    `json:"host,omitempty"`
	RobotMode        string    `json:"robot_mode,omitempty"`
	SafetyMode       string    `json:"safety_mode,omitempty"`
	ProgramRunning   bool      `json:"program_running"`
	TCPPose          []float64 `json:"tcp_pose,omitempty"`
	Joints           []float64 `json:"joints,omitempty"`
	DigitalOut       []bool    `json:"digital_out,omitempty"`
	DigitalIn        []bool    `json:"digital_in,omitempty"`
	SpeedScaling     float64   `json:"speed_scaling"`
	Gripper          string    `json:"gripper,omitempty"`
	UpdatedAt        string    `json:"updated_at,omitempty"`
	PolyscopeVersion string    `json:"polyscope_version,omitempty"`
	RemoteControl    *bool     `json:"remote_control,omitempty"`
}

// Camera is the metadata exposed to clients; the content-addressed ref stays private.
type RobotCamera struct {
	UpdatedAt time.Time `json:"updated_at"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
}

// Status is the stable robot.status/connect/disconnect result.
type RobotStatusResult struct {
	Bridge string       `json:"bridge"`
	State  *RobotState  `json:"state,omitempty"`
	Camera *RobotCamera `json:"camera,omitempty"`
	Error  string       `json:"error,omitempty"`
}

// StateEvent is published on changes and every second while connected.
type RobotStateEvent struct {
	State RobotState `json:"state"`
}
