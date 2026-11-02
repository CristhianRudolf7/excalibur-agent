package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities/fake"
)

func TestBrowserBlocksTellModelToNotifyUser(t *testing.T) {
	for _, block := range []string{capabilities.BlockedCaptcha, capabilities.BlockedLogin, capabilities.Blocked2FA, capabilities.BlockedBySite} {
		f := fake.New(capabilities.BrowserManifest())
		f.Results["browser_navigate"] = []capabilities.TaskResult{{State: capabilities.StateInputRequired, Need: block + ": resolve in headed mode, then continue"}}
		adapted, err := ToolsFromCapability(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range adapted {
			if tool.Definition().Name != "browser_navigate" {
				continue
			}
			res, err := tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"input_required", block, "human is required", "no takeover in this version", "Tell the user", "web_read"} {
				if !strings.Contains(res.Output, want) {
					t.Fatalf("missing %s: %s", want, res.Output)
				}
			}
		}
	}
}
