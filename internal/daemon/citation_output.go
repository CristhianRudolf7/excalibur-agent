package daemon

import (
	"context"
	"fmt"
	"net/url"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func (b *DaemonBackend) configureAssistantOutput() {
	if b.Master == nil {
		return
	}
	b.Master.SetAssistantOutput(master.AssistantOutput{
		// Trusted run trees come from successful spawn results, never model text.
		// Each master turn has a fresh run ID; unrelated conversations cannot
		// contribute edits even when they execute concurrently.
		Changes: func(tree []string) []api.FileChange {
			if b.Services == nil || b.Services.History == nil {
				return nil
			}
			var roots []string
			if b.Services.Projects != nil {
				for _, project := range b.Services.Projects.List() {
					roots = append(roots, project.Roots...)
				}
			}
			return b.Services.History.Changes(tree, roots)
		},
		Process: func(ctx context.Context, text string, tree []string) (string, []api.SourceRef, []string) {
			var library memory.CitationLibrary
			if b.Services != nil && b.Services.Memory != nil {
				library = b.Services.Memory
			}
			text, sources, dropped := (memory.CiteFlow{Library: library}).Process(ctx, text, tree)
			if b.Services != nil && b.Services.Sources != nil {
				runs := map[string]bool{}
				for _, id := range tree {
					runs[id] = true
				}
				seen := map[string]bool{}
				for _, source := range sources {
					if source.Kind == "web" {
						seen[source.URL] = true
					}
				}
				for _, event := range b.Services.Sources.Events() {
					if event.Blocked || event.RunID == "" || !runs[event.RunID] {
						continue
					}
					switch event.Tool {
					case "web_search", "web_read", "source_search":
					default:
						continue
					}
					u, err := url.Parse(event.URL)
					if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
						continue
					}
					// Only identity metadata is retained, regardless of provider storage rights.
					source := api.SourceRef{N: len(sources) + 1, Kind: "web", URL: event.URL, Title: event.Title, Domain: u.Hostname(), ReadAt: event.At, Tool: event.Tool}
					if seen[event.URL] {
						for i := range sources {
							if sources[i].Kind == "web" && sources[i].URL == event.URL {
								source.N = sources[i].N
								sources[i] = source
								break
							}
						}
						continue
					}
					seen[event.URL] = true
					sources = append(sources, source)
				}
			}
			return text, sources, dropped
		},
		Publish: func(ev api.MessageEvent) {
			if b.Bus != nil {
				_, _ = b.Bus.Publish(types.EventMessageCreated, nil, nil, ev)
			}
		},
		Dropped: func(n int) {
			if b.Bus != nil {
				_, _ = b.Bus.Publish(types.EventMetricRecorded, nil, nil, api.MetricEvent{Name: "citations_dropped", Value: float64(n), Unit: "refs"})
			}
		},
	})
}

// SendAssistant is the trusted final run-delivery entry point. Child reports
// keep literal references in mailboxes; the master validates them on delivery.
func (b *DaemonBackend) SendAssistant(ctx context.Context, conv, text, runID string) (api.MessageEvent, error) {
	if b.Master == nil {
		return api.MessageEvent{}, fmt.Errorf("daemon: assistant output needs a master")
	}
	tree := []string{}
	if runID != "" {
		if b.Ctrls == nil || b.Ctrls.Store == nil {
			tree = append(tree, runID)
		} else {
			rec, err := b.Ctrls.Store.GetRun(types.RunID(runID))
			if err != nil {
				return api.MessageEvent{}, err
			}
			agents := map[types.AgentID]bool{rec.Run.AgentID: true}
			all := b.Ctrls.Store.ListAgents()
			for changed := true; changed; {
				changed = false
				for _, a := range all {
					if a.ParentID != nil && agents[*a.ParentID] && !agents[a.ID] {
						agents[a.ID] = true
						changed = true
					}
				}
			}
			tree = append(tree, runID)
			for _, r := range b.Ctrls.Store.ListRuns() {
				if r.Run.AgentID != rec.Run.AgentID && agents[r.Run.AgentID] {
					tree = append(tree, string(r.Run.ID))
				}
			}
		}
	}
	return b.Master.SendAssistant(ctx, conv, text, tree)
}
