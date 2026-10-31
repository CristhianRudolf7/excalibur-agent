package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
)

// Delegation through the daemon (Section 3.6): the CLI and the MCP
// server call the daemon API when it is active, so tasks stay visible
// to the master and other clients. When the daemon is unreachable the
// caller falls back to embedded execution. This file is the shared
// seam both frontends use; it lives in the daemon package so gate and
// lifecycle stay behind one import.

// ErrDaemonDown reports that no daemon answered: run embedded.
var ErrDaemonDown = fmt.Errorf("daemon: not active")

// CallDaemon performs one JSON-RPC call against the daemon at addr.
// Unreachable daemons map to ErrDaemonDown for the embedded fallback.
func CallDaemon(ctx context.Context, addr, clientID, method string, params any) (json.RawMessage, error) {
	if !gateway.DaemonActive(addr) {
		return nil, ErrDaemonDown
	}
	c := gateway.Dial(addr, clientID)
	res, err := c.Call(ctx, "delegate-1", method, params)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// MustDaemon reports whether delegation must go through the daemon:
// the daemon answers and the task was created there. Embedded tasks
// (no daemon, or --ephemeral) return false.
func MustDaemon(addr string) bool {
	if addr == "" {
		return false
	}
	return gateway.DaemonActive(addr)
}
