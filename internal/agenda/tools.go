package agenda

// Tool surfaces of the agenda (21.1-21.4, 21.6). These are the
// definitions the tool registry exposes; the daemon binds each name to
// the corresponding Agenda, TriggerEngine, Engine or Simulate entry
// point. Descriptions stay short (P8).

// ToolName identifies an agenda tool.
type ToolName string

// Agenda tool names.
const (
	ToolSchedule     ToolName = "schedule"
	ToolReminders    ToolName = "reminders"
	ToolTriggerRules ToolName = "trigger_rules"
	ToolWorkflow     ToolName = "workflow"
	ToolFlowSimulate ToolName = "flow_simulate"
)

// AllTools lists the agenda tool names.
func AllTools() []ToolName {
	return []ToolName{ToolSchedule, ToolReminders, ToolTriggerRules, ToolWorkflow, ToolFlowSimulate}
}

// ToolDescription returns the short registry description of a tool.
func ToolDescription(name ToolName) string {
	switch name {
	case ToolSchedule:
		return "Create, list and cancel at/every/cron/after/window schedules with an explicit IANA zone"
	case ToolReminders:
		return "Create, list, snooze, complete and delete reminders with repeat policy"
	case ToolTriggerRules:
		return "Manage event trigger rules: filter, action, rate limit and dedup"
	case ToolWorkflow:
		return "Define, version, dry-run, test and run durable workflows; instances pin their version"
	case ToolFlowSimulate:
		return "Enumerate walkthroughs of a canonical flow with decision/pair/exhaustive coverage and review"
	}
	return ""
}
