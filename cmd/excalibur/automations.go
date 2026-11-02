package main

import (
	"context"
	"log/slog"

	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// managerLifecycle adapts Manager.Start's error result to the daemon lifecycle.
type managerLifecycle struct{ *automations.Manager }

func (m managerLifecycle) Start(ctx context.Context) {
	if err := m.Manager.Start(ctx); err != nil {
		slog.Error("automations: start failed", "err", err)
	}
}

// newAutomationService builds the automation manager; nil disables automations.
func newAutomationService(db *store.DB, workDir, language string, agent automations.AgentRunner, invoker automations.ToolInvoker, notifier automations.Notifier) daemon.AutomationLifecycle {
	m, err := automations.NewManager(automations.Config{
		DB:          db,
		AgentRunner: agent,
		ToolInvoker: invoker,
		Notifier:    notifier,
		WorkDir:     workDir,
		Language:    language,
	})
	if err != nil {
		slog.Error("automations: disabled", "err", err)
		return nil
	}
	return managerLifecycle{m}
}
