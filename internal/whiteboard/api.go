package whiteboard

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

// Op routes the additive client API. User authorship is assigned at this boundary.
func (s *Service) Op(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if s == nil || !Enabled() {
		return nil, &api.Error{Code: api.CodeInternal, Message: "whiteboard unavailable"}
	}
	if len(raw) > MaxBytes {
		return nil, invalid("input exceeds 2 MiB")
	}
	var p struct {
		ConversationID string `json:"conversation_id"`
		BoardID        string `json:"board_id"`
		BaseVersion    *int64 `json:"base_version"`
		Ops            []Op   `json:"ops"`
		ClientOpID     string `json:"client_op_id"`
		ImageBlob      string `json:"image_blob"`
		RequestID      string `json:"request_id"`
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if !json.Valid(raw) || bytes.TrimSpace(raw)[0] != '{' {
		return nil, invalid("params must be an object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return nil, invalid(err.Error())
	}
	if method == api.MethodWhiteboardList {
		boards, err := s.Store.List(ctx, p.ConversationID)
		return map[string]any{"boards": boards}, err
	}
	if strings.TrimSpace(p.BoardID) == "" {
		return nil, invalid("board_id required")
	}
	switch method {
	case api.MethodWhiteboardGet:
		return s.Store.Get(ctx, p.BoardID)
	case api.MethodWhiteboardApply:
		if p.BaseVersion == nil || *p.BaseVersion < 1 {
			return nil, invalid("base_version required and must be positive")
		}
		b, err := s.Apply(ctx, p.BoardID, *p.BaseVersion, p.ClientOpID, "user", p.Ops, false, 0)
		return map[string]any{"version": b.Version}, err
	case api.MethodWhiteboardSnapshot:
		if p.ImageBlob == "" {
			return nil, invalid("image_blob required")
		}
		err := s.Snapshot(ctx, p.BoardID, p.RequestID, p.ImageBlob)
		return map[string]any{"ok": err == nil}, err
	case api.MethodWhiteboardClose:
		err := s.Close(ctx, p.BoardID)
		return map[string]any{"ok": err == nil}, err
	default:
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown whiteboard method"}
	}
}
