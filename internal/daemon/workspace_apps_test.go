package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func TestWorkspaceServerReceiptFollowsScreenshotChild(t *testing.T) {
	apps := &workspaceApps{}
	parent := &assistantToolRunner{policy: &deliveryPolicy{Autonomous: true, apps: apps}}
	parent.policy.apps.observe("exec", nil, tools.Result{Status: tools.StatusOK, Output: "Ready: http://127.0.0.1:8123", Sessions: []tools.SessionReceipt{{ID: "server", Alive: true}}})
	// Nested children receive the same policy, even with a different workspace.
	childPolicy := *parent.policy
	ctx := withDeliveryPolicy(context.Background(), childPolicy)
	if !ownWorkspaceOrigin(ctx, "192.168.1.8", "8123") || !ownWorkspaceApp(ctx, "192.168.1.8") {
		t.Fatal("screenshot child lost its parent's server port or hostname alias")
	}
	if ownWorkspaceOrigin(ctx, "accounts.example.com", "8123") || ownWorkspaceApp(ctx, "192.168.1.9") {
		t.Fatal("server receipt authorized an unrelated site")
	}
	other := withDeliveryPolicy(context.Background(), deliveryPolicy{apps: &workspaceApps{}})
	if ownWorkspaceOrigin(other, "192.168.1.8", "8123") {
		t.Fatal("server ownership leaked to another conversation")
	}
	// Ordinary non-delivery work also recognizes ports it started.
	ctx = withDeliveryPolicy(context.Background(), deliveryPolicy{apps: apps})
	if !ownWorkspaceApp(ctx, "192.168.1.8") {
		t.Fatal("interactive own-app login would open a credential form")
	}
}

func TestWorkspaceServerReceiptsRequireOwnedRunningSession(t *testing.T) {
	for _, tc := range []struct {
		name, tool, session, url string
		status                   tools.ResultStatus
		alive, own               bool
	}{
		{"running", "exec", "", "http://10.0.0.8:8123", tools.StatusOK, true, true},
		{"stopped", "exec", "", "http://10.0.0.8:8123", tools.StatusOK, false, false},
		{"failed", "exec", "", "http://10.0.0.8:8123", tools.StatusToolError, true, false},
		{"file text", "read_file", "", "http://10.0.0.8:8123", tools.StatusOK, true, false},
		{"external", "exec", "", "https://example.com:8123", tools.StatusOK, true, false},
		{"other session", "exec_io", "other", "http://10.0.0.8:8123", tools.StatusOK, true, false},
		{"owned session", "exec_io", "server", "http://10.0.0.8:8123", tools.StatusOK, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apps := &workspaceApps{}
			apps.observe("exec", nil, tools.Result{Status: tools.StatusOK, Sessions: []tools.SessionReceipt{{ID: "server", Alive: true}}})
			args, _ := json.Marshal(map[string]string{"session": tc.session})
			apps.observe(tc.tool, args, tools.Result{Status: tc.status, Output: tc.url, Sessions: []tools.SessionReceipt{{ID: "server", Alive: tc.alive}}})
			if apps.owns("10.0.0.8", "8123") != tc.own {
				t.Fatal("incorrect server ownership")
			}
		})
	}
}
