package ctxcompiler

import (
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// History without sequence numbers must keep chronological order; a
// content tie-break once sorted conversations alphabetically.
func TestActivityWithoutSeqKeepsInputOrder(t *testing.T) {
	events := []ActivityEvent{
		{Role: providers.RoleUser, Content: "zeta first"},
		{Role: providers.RoleAssistant, Content: "✅ automation done"},
		{Role: providers.RoleUser, Content: "alpha last"},
	}
	var req providers.ChatRequest
	Compiled{}.AppendActivityMessages(&req, events)
	if len(req.Messages) != 4 || req.Messages[2].Content != "alpha last" {
		t.Fatalf("order changed: %+v", req.Messages)
	}
}
