package api

const (
	EventAutomationUpdated = "automation.updated"
	EventAutomationDeleted = "automation.deleted"
	EventAutomationRun     = "automation.run"
)

type AutomationsParams struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}
type AutomationDeletedEvent struct {
	ID string `json:"id"`
}
