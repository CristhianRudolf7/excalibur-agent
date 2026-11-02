package api

import (
	"encoding/json"
	"time"
)

type Viewport struct {
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Zoom float64 `json:"zoom"`
}
type Style struct {
	Stroke      string   `json:"stroke,omitempty"`
	Fill        string   `json:"fill,omitempty"`
	StrokeWidth *float64 `json:"stroke_width,omitempty"`
	StrokeStyle string   `json:"stroke_style,omitempty"`
	Roughness   *float64 `json:"roughness,omitempty"`
	FontSize    *float64 `json:"font_size,omitempty"`
	FontFamily  string   `json:"font_family,omitempty"`
	TextAlign   string   `json:"text_align,omitempty"`
	Opacity     *float64 `json:"opacity,omitempty"`
}
type Element struct {
	ID           string          `json:"id"`
	Type         string          `json:"type"`
	X            float64         `json:"x"`
	Y            float64         `json:"y"`
	Width        float64         `json:"width,omitempty"`
	Height       float64         `json:"height,omitempty"`
	Points       [][2]float64    `json:"points,omitempty"`
	Text         string          `json:"text,omitempty"`
	Latex        string          `json:"latex,omitempty"`
	Mermaid      string          `json:"mermaid,omitempty"`
	Code         string          `json:"code,omitempty"`
	Language     string          `json:"language,omitempty"`
	Src          string          `json:"src,omitempty"`
	StartBinding json.RawMessage `json:"start_binding,omitempty"`
	EndBinding   json.RawMessage `json:"end_binding,omitempty"`
	Style        *Style          `json:"style,omitempty"`
	GroupID      string          `json:"group_id,omitempty"`
	FrameID      string          `json:"frame_id,omitempty"`
	Z            float64         `json:"z,omitempty"`
	Locked       bool            `json:"locked,omitempty"`
	Author       string          `json:"author"`
}
type Board struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Title          string    `json:"title"`
	Version        int64     `json:"version"`
	Elements       []Element `json:"elements"`
	Viewport       *Viewport `json:"viewport,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type BoardSummary struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Title          string    `json:"title"`
	Version        int64     `json:"version"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (b Board) Summary() BoardSummary {
	return BoardSummary{b.ID, b.ConversationID, b.Title, b.Version, b.UpdatedAt}
}

type Op struct {
	Op       string            `json:"op"`
	Elements []json.RawMessage `json:"elements,omitempty"`
	IDs      []string          `json:"ids,omitempty"`
	KeepUser bool              `json:"keep_user,omitempty"`
}

// Whiteboard client parameters are shared by the daemon and JSON-RPC clients.
type WhiteboardListParams struct {
	ConversationID string `json:"conversation_id,omitempty"`
}
type WhiteboardGetParams struct {
	BoardID string `json:"board_id"`
}
type WhiteboardApplyParams struct {
	BoardID     string `json:"board_id"`
	BaseVersion int64  `json:"base_version"`
	Ops         []Op   `json:"ops"`
	ClientOpID  string `json:"client_op_id"`
}
type WhiteboardSnapshotParams struct {
	BoardID   string `json:"board_id"`
	ImageBlob string `json:"image_blob"`
	RequestID string `json:"request_id,omitempty"`
}
type WhiteboardApplyResult struct {
	Version int64 `json:"version"`
}
type WhiteboardListResult struct {
	Boards []BoardSummary `json:"boards"`
}

type WhiteboardOpenedEvent struct {
	WhiteboardEvent
	Board  Board  `json:"board"`
	Layout string `json:"layout"`
}
type WhiteboardOpsEvent struct {
	WhiteboardEvent
	Version int64  `json:"version"`
	Ops     []Op   `json:"ops"`
	Animate bool   `json:"animate,omitempty"`
	StepMS  int    `json:"step_ms,omitempty"`
	Author  string `json:"author"`
}
type WhiteboardRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}
type WhiteboardFocusEvent struct {
	WhiteboardEvent
	IDs     []string        `json:"ids,omitempty"`
	Rect    *WhiteboardRect `json:"rect,omitempty"`
	Zoom    float64         `json:"zoom,omitempty"`
	Version int64           `json:"version"`
}
type WhiteboardHighlightEvent struct {
	WhiteboardEvent
	IDs        []string `json:"ids"`
	Style      string   `json:"style"`
	DurationMS int      `json:"duration_ms,omitempty"`
	Version    int64    `json:"version"`
}
type WhiteboardSnapshotRequestedEvent struct {
	WhiteboardEvent
	RequestID string `json:"request_id"`
	Version   int64  `json:"version"`
}
