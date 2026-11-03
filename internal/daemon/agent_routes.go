package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

func agentError(err error) error {
	if err == nil {
		return nil
	}
	code := api.CodeInvalidParams
	switch {
	case errors.Is(err, agents.ErrNotFound):
		code = api.CodeUnknownTarget
	case errors.Is(err, agents.ErrArchived), errors.Is(err, agents.ErrLastActive), errors.Is(err, agents.ErrAmbiguous):
		code = api.CodeConflict
	}
	return &api.Error{Code: code, Message: err.Error()}
}
func decodeAgentParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") || json.Unmarshal(raw, out) != nil {
		return &api.Error{Code: api.CodeInvalidParams, Message: "expected agent parameters object"}
	}
	return nil
}
func (b *DaemonBackend) agentsOp(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	r := b.agentRegistry()
	if r == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "personal agent registry is unavailable"}
	}
	switch method {
	case api.MethodAgentsList:
		var p api.AgentsListParams
		if err := decodeAgentParams(raw, &p); err != nil {
			return nil, err
		}
		return api.AgentsListResult{Agents: r.List(p.IncludeArchived), FavoriteID: r.Favorite().ID}, nil
	case api.MethodAgentsCreate:
		var p api.AgentsCreateParams
		if err := decodeAgentParams(raw, &p); err != nil {
			return nil, err
		}
		a, err := r.Create(ctx, p)
		return a, agentError(err)
	case api.MethodAgentsUpdate:
		var p api.AgentsUpdateParams
		if err := decodeAgentParams(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "id is required"}
		}
		a, err := r.Update(ctx, p)
		return a, agentError(err)
	}
	var p api.AgentIDParams
	if err := decodeAgentParams(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "id is required"}
	}
	switch method {
	case api.MethodAgentsGet:
		a, err := r.Get(p.ID)
		return a, agentError(err)
	case api.MethodAgentsArchive:
		id, err := r.Archive(ctx, p.ID)
		return api.AgentsFavoriteResult{FavoriteID: id}, agentError(err)
	case api.MethodAgentsRestore:
		a, err := r.Restore(ctx, p.ID)
		return a, agentError(err)
	case api.MethodAgentsSetFavorite:
		err := r.SetFavorite(ctx, p.ID)
		return api.AgentsFavoriteResult{FavoriteID: p.ID}, agentError(err)
	case api.MethodAgentsMarkRead:
		err := r.MarkRead(ctx, p.ID)
		return api.AgentsMarkReadResult{}, agentError(err)
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown method " + method}
	}
}
