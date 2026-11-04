package daemon

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// Shared by descendants of one delegation, so a screenshot child recognizes
// the server started by its implementer. Only local literal origins are tracked.
type workspaceApps struct {
	mu       sync.Mutex
	hosts    map[string]bool
	ports    map[string]bool
	sessions map[string]bool
}

var workspaceServerURL = regexp.MustCompile(`https?://(?:\[[0-9a-fA-F:]+\]|[a-zA-Z0-9.-]+):[0-9]+`)

func (a *workspaceApps) observe(name string, args json.RawMessage, result tools.Result) {
	if a == nil || result.Status != tools.StatusOK || name != "exec" && name != "exec_io" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessions == nil {
		a.hosts, a.ports, a.sessions = map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	if name == "exec_io" {
		var input struct {
			Session string `json:"session"`
		}
		if json.Unmarshal(args, &input) != nil || !a.sessions[input.Session] {
			return
		}
	}
	alive := false
	for _, session := range result.Sessions {
		if session.Alive {
			alive = true
			a.sessions[session.ID] = true
		}
	}
	if !alive {
		return
	}
	for _, raw := range workspaceServerURL.FindAllString(result.Output, -1) {
		u, err := url.Parse(raw)
		if err == nil && localAppHost(u.Hostname()) && u.Port() != "" {
			a.hosts[strings.ToLower(u.Hostname())] = true
			a.ports[u.Port()] = true
		}
	}
}

func (a *workspaceApps) owns(host, port string) bool {
	if a == nil || !localAppHost(host) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	host = strings.ToLower(host)
	if a.hosts[host] || port != "" && a.ports[port] {
		// Remember aliases for request_credentials, which accepts only a host.
		a.hosts[host] = true
		return true
	}
	return false
}

func ownWorkspaceOrigin(ctx context.Context, host, port string) bool {
	policy, _ := ctx.Value(deliveryPolicyKey{}).(deliveryPolicy)
	if policy.apps.owns(host, port) {
		return true
	}
	workspace, _ := ctx.Value(workspaceAppKey{}).(string)
	// A local workspace's loopback preview is its demo app. Private LAN sites
	// need a server receipt; being on the same LAN alone does not prove ownership.
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	loopback := host == "localhost" || strings.HasSuffix(host, ".localhost")
	if ip := net.ParseIP(host); ip != nil {
		loopback = ip.IsLoopback() || ip.IsUnspecified()
	}
	return loopback && (workspace != "" || policy.Autonomous)
}
