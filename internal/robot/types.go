// Package robot owns the supervised UR bridge, camera frame and assistant tool.
package robot

import (
	"encoding/json"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// State, Camera and Status are aliases of the public RPC contract.
type State = api.RobotState
type Camera = api.RobotCamera
type Status = api.RobotStatusResult
type StateEvent = api.RobotStateEvent

func decode(raw json.RawMessage, target any) error { return strictDecode(raw, target) }
