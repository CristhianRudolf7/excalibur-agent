package api

import (
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

const (
	MethodAgentsList                           = "agents.list"
	MethodAgentsGet                            = "agents.get"
	MethodAgentsCreate                         = "agents.create"
	MethodAgentsUpdate                         = "agents.update"
	MethodAgentsArchive                        = "agents.archive"
	MethodAgentsRestore                        = "agents.restore"
	MethodAgentsSetFavorite                    = "agents.set_favorite"
	MethodAgentsMarkRead                       = "agents.mark_read"
	EventAgentsCreated         types.EventType = "agents.created"
	EventAgentsUpdated         types.EventType = "agents.updated"
	EventAgentsArchived        types.EventType = "agents.archived"
	EventAgentsRestored        types.EventType = "agents.restored"
	EventAgentsFavoriteChanged types.EventType = "agents.favorite_changed"
	EventAgentsUnread          types.EventType = "agents.unread"
)

type Agent = agents.PersonalAgent
type AvatarSpec = agents.AvatarSpec
type AgentsCreateParams = agents.CreateParams
type AgentsUpdateParams = agents.UpdateParams
type AgentsListParams struct {
	IncludeArchived bool `json:"include_archived,omitempty"`
}
type AgentIDParams struct {
	ID string `json:"id"`
}
type AgentsListResult struct {
	Agents     []Agent `json:"agents"`
	FavoriteID string  `json:"favorite_id"`
}
type AgentsFavoriteResult struct {
	FavoriteID string `json:"favorite_id"`
}
type AgentsMarkReadResult struct {
	Unread int `json:"unread"`
}
type AgentsUnreadEvent struct {
	AgentID       string     `json:"agent_id"`
	Unread        int        `json:"unread"`
	LastPreview   string     `json:"last_preview"`
	LastMessageAt *time.Time `json:"last_message_at"`
}
