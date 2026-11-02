package api

import "github.com/CristhianRudolf7/excalibur-agent/internal/types"

const EventActivityUpdated types.EventType = "activity.updated"

type ActivityListParams struct {
	ConversationID string `json:"conversation_id"`
}
type ActivityProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}
type ActivityItem struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Goal       string            `json:"goal,omitempty"`
	Profile    string            `json:"profile,omitempty"`
	State      string            `json:"state"`
	Step       string            `json:"step,omitempty"`
	Progress   *ActivityProgress `json:"progress,omitempty"`
	ParentID   string            `json:"parent_id,omitempty"`
	StartedAt  string            `json:"started_at"`
	UpdatedAt  string            `json:"updated_at"`
	FinishedAt string            `json:"finished_at,omitempty"`
	Error      string            `json:"error,omitempty"`
}
type ActivityListResult struct {
	Items []ActivityItem `json:"items"`
}
type ActivityUpdatedEvent struct {
	Item ActivityItem `json:"item"`
}
type FilesOpenParams struct {
	Path      string `json:"path"`
	ProjectID string `json:"project_id,omitempty"`
}
type FilesOpenResult struct {
	Path       string  `json:"path"`
	MIME       string  `json:"mime"`
	Kind       string  `json:"kind"`
	Content    *string `json:"content,omitempty"`
	DocumentID string  `json:"document_id,omitempty"`
	BlobURL    string  `json:"blob_url,omitempty"`
	Size       int64   `json:"size"`
}
