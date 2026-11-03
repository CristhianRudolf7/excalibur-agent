package daemon

import (
	"context"
	"encoding/json"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

func (b *DaemonBackend) credentialsOp(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	// Transport buffers are mutable: erase the dedicated call body after decode.
	defer vault.ZeroBytes(raw)
	if b.credentials == nil {
		return nil, &api.Error{Code: api.CodeInternal, Message: "credential requests unavailable"}
	}
	invalid := &api.Error{Code: api.CodeInvalidParams, Message: "invalid credential parameters"}
	switch method {
	case api.MethodCredentialsPending:
		// An empty filter discovers cross-channel requests for this single-user daemon.
		var p struct {
			ConversationID *string `json:"conversation_id"`
		}
		if strictLibraryParams(raw, &p) != nil || p.ConversationID == nil {
			return nil, invalid
		}
		return b.credentials.Pending(*p.ConversationID), nil
	case api.MethodCredentialsSubmit:
		// []byte's default JSON encoding is base64. Decode through RawMessage so
		// ordinary JSON strings stay compatible while owned plaintext is zeroable.
		var p struct {
			RequestID string                     `json:"request_id"`
			Values    map[string]json.RawMessage `json:"values"`
			Remember  bool                       `json:"remember"`
		}
		defer func() {
			for f, v := range p.Values {
				vault.ZeroBytes(v)
				delete(p.Values, f)
			}
		}()
		if strictLibraryParams(raw, &p) != nil || p.RequestID == "" || len(p.Values) == 0 || len(p.Values) > 5 {
			return nil, invalid
		}
		values := map[string][]byte{}
		defer func() {
			for f, v := range values {
				vault.ZeroBytes(v)
				delete(values, f)
			}
		}()
		for f, v := range p.Values {
			var value string
			if !credentialFields[f] || len(v) > 100000 || json.Unmarshal(v, &value) != nil {
				return nil, invalid
			}
			values[f] = []byte(value)
			value = ""
		}
		if err := b.credentials.Submit(ctx, p.RequestID, values, p.Remember); err != nil {
			return nil, err
		}
		return api.CredentialsOKResult{OK: true}, nil
	case api.MethodCredentialsCancel:
		var p api.CredentialsCancelParams
		if strictLibraryParams(raw, &p) != nil || p.RequestID == "" {
			return nil, invalid
		}
		if err := b.credentials.Cancel(p.RequestID); err != nil {
			return nil, err
		}
		return api.CredentialsOKResult{OK: true}, nil
	}
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown credential method"}
}
