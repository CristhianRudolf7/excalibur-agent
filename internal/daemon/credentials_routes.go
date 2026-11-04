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
	case api.MethodCredentialsSubmit, api.MethodCredentialsSaveLogin:
		// Decode values through RawMessage so ordinary JSON strings stay compatible
		// while owned plaintext remains zeroable. Each method retains strict fields.
		var requestID, site, label, startURL string
		var remember bool
		var encoded map[string]json.RawMessage
		defer func() {
			for f, v := range encoded {
				vault.ZeroBytes(v)
				delete(encoded, f)
			}
			requestID, site, label, startURL = "", "", "", ""
		}()
		var decodeErr error
		if method == api.MethodCredentialsSubmit {
			var p struct {
				RequestID string                     `json:"request_id"`
				Values    map[string]json.RawMessage `json:"values"`
				Remember  bool                       `json:"remember"`
			}
			decodeErr = strictLibraryParams(raw, &p)
			requestID, encoded, remember = p.RequestID, p.Values, p.Remember
			p.RequestID, p.Values = "", nil
		} else {
			var p struct {
				Site     string                     `json:"site"`
				Label    string                     `json:"label"`
				StartURL string                     `json:"start_url"`
				Values   map[string]json.RawMessage `json:"values"`
			}
			decodeErr = strictLibraryParams(raw, &p)
			site, label, startURL, encoded = p.Site, p.Label, p.StartURL, p.Values
			p.Site, p.Label, p.StartURL, p.Values = "", "", "", nil
		}
		if decodeErr != nil || (method == api.MethodCredentialsSubmit && requestID == "") || len(encoded) == 0 || len(encoded) > 5 {
			return nil, invalid
		}
		values := map[string][]byte{}
		defer clearCredentialValues(values)
		for f, v := range encoded {
			var value string
			if !credentialFields[f] || len(v) > 100000 || json.Unmarshal(v, &value) != nil {
				return nil, invalid
			}
			values[f] = []byte(value)
			value = ""
		}
		var err error
		if method == api.MethodCredentialsSaveLogin {
			err = b.credentials.SaveLogin(ctx, site, label, startURL, values)
		} else {
			err = b.credentials.Submit(ctx, requestID, values, remember)
		}
		if err != nil {
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
