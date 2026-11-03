package daemon

import (
	"context"
	"encoding/json"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/googleapi"
)

type googleConnection interface {
	googleapi.TokenProvider
	ConnectionStatus(context.Context) googleapi.ConnectionStatus
	Start(context.Context) (string, string, error)
	Complete(context.Context, string, string) (googleapi.ConnectionStatus, error)
	Disconnect(context.Context) error
}

func googleStatus(s googleapi.ConnectionStatus) api.GoogleStatusResult {
	out := api.GoogleStatusResult{Connected: s.Connected, Configured: s.Configured, Email: s.Email, Scopes: s.Scopes, Reason: s.Reason, Warning: s.Warning, MissingScopes: s.MissingScopes}
	if !s.ExpiresAt.IsZero() {
		out.ExpiresAt = &s.ExpiresAt
	}
	return out
}
func googleOp(ctx context.Context, p googleapi.TokenProvider, method string, raw json.RawMessage) (any, error) {
	c, ok := p.(googleConnection)
	if !ok {
		if method == api.MethodGoogleStatus {
			return api.GoogleStatusResult{Reason: "not_configured"}, nil
		}
		return nil, &api.Error{Code: api.CodeInternal, Message: "Google connection unavailable"}
	}
	switch method {
	case api.MethodGoogleStatus:
		return googleStatus(c.ConnectionStatus(ctx)), nil
	case api.MethodGoogleConnectStart:
		auth, _, err := c.Start(ctx)
		if err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
		}
		return api.GoogleConnectStartResult{AuthURL: auth}, nil
	case api.MethodGoogleConnectComplete:
		var params api.GoogleConnectCompleteParams
		if json.Unmarshal(raw, &params) != nil || params.Code == "" || params.State == "" || len(params.Code) > 8192 || len(params.State) > 256 {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "Google callback needs code and state"}
		}
		s, err := c.Complete(ctx, params.Code, params.State)
		if err != nil {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
		}
		return googleStatus(s), nil
	case api.MethodGoogleDisconnect:
		if err := c.Disconnect(ctx); err != nil {
			return nil, &api.Error{Code: api.CodeInternal, Message: err.Error()}
		}
		return googleStatus(c.ConnectionStatus(ctx)), nil
	}
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown Google method"}
}
