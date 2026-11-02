package api

import "github.com/CristhianRudolf7/excalibur-agent/internal/types"

// Artifact is a visual output recorded by a tool in one conversation run.
type Artifact struct {
	ID             string `json:"id"`
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	ToolCallID     string `json:"tool_call_id"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	Format         string `json:"format"`
	Dialect        string `json:"dialect,omitempty"`
	Group          string `json:"group,omitempty"`
	MIME           string `json:"mime"`
	Size           int64  `json:"size"`
	CreatedAt      string `json:"created_at"`
	URLOrPath      string `json:"url_or_path"`
}

const (
	EventArtifactCreated types.EventType = "artifact.created"
	EventArtifactReady   types.EventType = "artifact.ready"
)

// ArtifactCreatedEvent announces a persisted artifact before the turn finishes.
type ArtifactCreatedEvent struct {
	ConversationID string   `json:"conversation_id"`
	RunID          string   `json:"run_id"`
	Artifact       Artifact `json:"artifact"`
}

// ArtifactReadyEvent announces all retained artifacts once a turn finishes.
type ArtifactReadyEvent struct {
	ConversationID string     `json:"conversation_id"`
	RunID          string     `json:"run_id"`
	Artifacts      []Artifact `json:"artifacts"`
}

const (
	MethodArtifactsGet  = "artifacts.get"
	MethodArtifactsList = "artifacts.list"
)

// ArtifactsGetParams resolves an opaque ID inside its originating conversation
// and run. It deliberately accepts no filesystem path.
type ArtifactsGetParams struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id"`
	ID             string `json:"id"`
}

type ArtifactsGetResult struct {
	Artifact Artifact `json:"artifact"`
	Content  string   `json:"content"`
	Encoding string   `json:"encoding"`
	Source   string   `json:"source"`
}

// ArtifactsListParams discovers retained outputs after reconnecting. With no
// run filter, at most the latest 100 artifacts are returned.
type ArtifactsListParams struct {
	ConversationID string `json:"conversation_id"`
	RunID          string `json:"run_id,omitempty"`
}

type ArtifactsListResult struct {
	Artifacts []Artifact `json:"artifacts"`
}
