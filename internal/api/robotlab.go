package api

// CodeRobotLabUnavailable reports a disabled, uninstalled or unavailable runtime.
const CodeRobotLabUnavailable = -32020

// RobotLabEmptyParams carries Robot Lab client parameters.
type RobotLabEmptyParams struct {
}

// RobotLabSessionOpenParams carries Robot Lab client parameters.
type RobotLabSessionOpenParams struct {
	Scenario string         `json:"scenario"`
	Params   map[string]any `json:"params,omitempty"`
	Seed     *int64         `json:"seed,omitempty"`
}

// RobotLabSessionParams carries Robot Lab client parameters.
type RobotLabSessionParams struct {
	Session string `json:"session"`
}

// RobotLabSessionResetParams carries Robot Lab client parameters.
type RobotLabSessionResetParams struct {
	Session string `json:"session"`
	Seed    *int64 `json:"seed,omitempty"`
}

// RobotLabSessionSetParams carries Robot Lab client parameters.
type RobotLabSessionSetParams struct {
	Session string         `json:"session"`
	Params  map[string]any `json:"params"`
}

// RobotLabSessionControlParams carries Robot Lab client parameters.
type RobotLabSessionControlParams struct {
	Session string             `json:"session"`
	Mode    string             `json:"mode"`
	Inputs  map[string]float64 `json:"inputs,omitempty"`
	Pose    *RobotLabPose      `json:"pose,omitempty"`
}

// RobotLabSessionPolicyParams carries Robot Lab client parameters.
type RobotLabSessionPolicyParams struct {
	Session    string `json:"session"`
	Experiment string `json:"experiment,omitempty"`
	Run        string `json:"run,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
	Pretrained bool   `json:"pretrained,omitempty"`
}

// RobotLabSessionSnapshotParams carries Robot Lab client parameters.
type RobotLabSessionSnapshotParams struct {
	Session string `json:"session"`
	Camera  string `json:"camera,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	Marks   bool   `json:"marks,omitempty"`
}

// RobotLabExperimentParams carries Robot Lab client parameters.
type RobotLabExperimentParams struct {
	Experiment string `json:"experiment"`
}

// RobotLabExperimentCreateParams carries Robot Lab client parameters.
type RobotLabExperimentCreateParams struct {
	Name          string             `json:"name"`
	Scenario      string             `json:"scenario"`
	Params        map[string]any     `json:"params,omitempty"`
	Runs          []RobotLabRunSpec  `json:"runs"`
	TotalSteps    *int64             `json:"total_steps,omitempty"`
	RewardWeights map[string]float64 `json:"reward_weights,omitempty"`
	Code          *string            `json:"code,omitempty"`
}

// RobotLabExperimentCodeParams carries Robot Lab client parameters.
type RobotLabExperimentCodeParams struct {
	Experiment string `json:"experiment"`
	Code       string `json:"code"`
}

// RobotLabExperimentMetricsParams carries Robot Lab client parameters.
type RobotLabExperimentMetricsParams struct {
	Experiment string `json:"experiment"`
	Run        string `json:"run,omitempty"`
	Since      *int64 `json:"since,omitempty"`
}

// RobotLabTeleopMappingParams carries Robot Lab client parameters.
type RobotLabTeleopMappingParams struct {
	Session string  `json:"session"`
	Code    *string `json:"code,omitempty"`
}

// RobotLabVisionStepParams carries Robot Lab client parameters.
type RobotLabVisionStepParams struct {
	Session     string `json:"session"`
	Instruction string `json:"instruction"`
}

// RobotLabRunSpec describes one algorithm in an experiment.
type RobotLabRunSpec struct {
	Label       string         `json:"label"`
	Algorithm   string         `json:"algorithm"`
	Hyperparams map[string]any `json:"hyperparams,omitempty"`
}

// RobotLabPose contains landmarks only; webcam video stays in the browser.
type RobotLabPose struct {
	Landmarks   [][]float64          `json:"landmarks"`
	World       [][]float64          `json:"world,omitempty"`
	Hands       []RobotLabHand       `json:"hands,omitempty"`
	T           float64              `json:"t"`
	Arm         string               `json:"arm,omitempty"`
	Calibration *RobotLabCalibration `json:"calibration,omitempty"`
}
type RobotLabHand struct {
	Handedness string      `json:"handedness"`
	Landmarks  [][]float64 `json:"landmarks"`
}
type RobotLabCalibration struct {
	Landmarks [][]float64 `json:"landmarks"`
	World     [][]float64 `json:"world,omitempty"`
}

func init() {
	EventTypes = append(EventTypes, []EventTypeInfo{
		{"robotlab.frame", "RobotLabFrame", "Robot Lab frame."},
		{"robotlab.metrics", "RobotLabMetricsEvent", "Robot Lab metrics."},
		{"robotlab.experiment", "RobotLabExperiment", "Robot Lab experiment."},
		{"robotlab.log", "RobotLabLogEvent", "Robot Lab log."},
		{"robotlab.install", "RobotLabInstallEvent", "Robot Lab install."},
		{"robotlab.vision", "RobotLabVisionEvent", "Robot Lab vision."},
		{"robotlab.focus", "RobotLabFocusEvent", "Robot Lab focus."},
		{"robotlab.teleop", "RobotLabTeleopEvent", "Robot Lab teleop."},
	}...)
}

// Robot Lab client methods use the registered-method gateway passthrough.
const (
	MethodRobotLabStatus            = "robotlab.status"
	MethodRobotLabInstall           = "robotlab.install"
	MethodRobotLabScenarios         = "robotlab.scenarios"
	MethodRobotLabSessionOpen       = "robotlab.session.open"
	MethodRobotLabSessionClose      = "robotlab.session.close"
	MethodRobotLabSessionReset      = "robotlab.session.reset"
	MethodRobotLabSessionParams     = "robotlab.session.params"
	MethodRobotLabSessionControl    = "robotlab.session.control"
	MethodRobotLabSessionPolicy     = "robotlab.session.policy"
	MethodRobotLabSessionSnapshot   = "robotlab.session.snapshot"
	MethodRobotLabExperimentList    = "robotlab.experiment.list"
	MethodRobotLabExperimentGet     = "robotlab.experiment.get"
	MethodRobotLabExperimentCreate  = "robotlab.experiment.create"
	MethodRobotLabExperimentCode    = "robotlab.experiment.code"
	MethodRobotLabExperimentStart   = "robotlab.experiment.start"
	MethodRobotLabExperimentStop    = "robotlab.experiment.stop"
	MethodRobotLabExperimentMetrics = "robotlab.experiment.metrics"
	MethodRobotLabTeleopGet         = "robotlab.teleop.get"
	MethodRobotLabTeleopMapping     = "robotlab.teleop.mapping"
	MethodRobotLabVisionStep        = "robotlab.vision.step"
)
