// Package agenda implements reminders, cron with time zones, event
// triggers and durable workflows.
//
// Architecture reference: Section 21 (automation), with the missed
// schedule policies of Section 17.4, the multi-day session pattern of
// Section 17.6 and the retry policy of Section 17.7. Implemented in
// milestones H10 (agenda, reminders) and H13 (triggers, workflows,
// simulation, built-ins, research hooks, internal jobs).
//
// The package is intentionally self-contained: persistence goes through
// the narrow Store interface defined here (in-memory implementation
// included), and launching work goes through the Launcher interface,
// so the daemon, orchestrator and tool registry can adopt it without
// the package depending on them.
package agenda
