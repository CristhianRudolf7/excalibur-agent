package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func deliveryCheckDefinition() tools.Definition {
	return tools.Definition{Name: "delivery_check", Description: "Check the full client-spec delivery evidence. Resolve every pending requirement, executed check and desktop/mobile screenshot before finishing. An incomplete final response is returned to this check so work can continue.", InputSchema: json.RawMessage(`{"type":"object","properties":{"attempt":{"type":"integer"}},"additionalProperties":false}`), Profiles: []string{"default"}, Effect: types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelCooperative, Timeout: time.Second, OutputBudget: 16000}
}

// Incomplete replies become a local evidence check, preserving the engine's
// normal step, journal and budget accounting while letting the worker continue.
// This is composed only into autonomous daemon builds, never evaluation runs.
type deliveryEndpoint struct {
	engine.ModelEndpoint
	check   func() tasks.GateReport
	attempt int
}

func (e *deliveryEndpoint) Chat(ctx context.Context, role types.ModelRole, req providers.ChatRequest) (providers.ChatResponse, models.CallMeta, error) {
	response, meta, err := e.ModelEndpoint.Chat(ctx, role, req)
	if err != nil {
		return response, meta, err
	}
	finishing := len(response.Message.ToolCalls) == 0
	for _, call := range response.Message.ToolCalls {
		if call.Name == "finish" {
			finishing = true
		}
	}
	if finishing && !e.check().OK {
		e.attempt++
		args, _ := json.Marshal(map[string]int{"attempt": e.attempt})
		response.Message.Content = ""
		calls := make([]providers.ToolCall, 0, len(response.Message.ToolCalls)+1)
		for _, call := range response.Message.ToolCalls {
			if call.Name != "finish" {
				calls = append(calls, call)
			}
		}
		response.Message.ToolCalls = append(calls, providers.ToolCall{ID: fmt.Sprintf("delivery-check-%d", e.attempt), Name: "delivery_check", Arguments: args})
	}
	return response, meta, nil
}
