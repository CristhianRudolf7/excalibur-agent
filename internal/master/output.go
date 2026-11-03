package master

import (
	"context"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// AssistantOutput configures the single persistence/publication boundary.
// Process validates complete text; Publish observes only persisted messages.
// Internal child reports retain raw refs until they reach this boundary.
type AssistantOutput struct {
	Process func(context.Context, string, []string) (string, []api.SourceRef, []string)
	Changes func([]string) []api.FileChange
	Publish func(api.MessageEvent)
	Dropped func(int)
}

// SetAssistantOutput installs the daemon output adapter before accepting turns.
func (m *Master) SetAssistantOutput(output AssistantOutput) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.output = output
}

// SendAssistant validates and persists an answer before publishing it. tree is
// supplied by trusted run delivery, never parsed from user or model text.
func (m *Master) SendAssistant(ctx context.Context, conv, text string, tree []string) (api.MessageEvent, error) {
	if err := ctx.Err(); err != nil {
		return api.MessageEvent{}, err
	}
	m.mu.Lock()
	output := m.output
	m.mu.Unlock()
	var sources []api.SourceRef
	var dropped []string
	if output.Process != nil {
		text, sources, dropped = output.Process(ctx, text, tree)
	}
	if err := ctx.Err(); err != nil {
		return api.MessageEvent{}, err
	}
	if strings.TrimSpace(text) == "" {
		return api.MessageEvent{}, nil
	}
	var changes []api.FileChange
	if output.Changes != nil {
		changes = output.Changes(tree)
	}
	now := time.Now().UTC()
	id, err := m.store.SaveMessage(ctx, conv, Normalized{CreatedAt: now, Role: "assistant", Text: text, Sources: cloneSources(sources), Changes: api.CloneChanges(changes)})
	if err != nil {
		return api.MessageEvent{}, err
	}
	ev := api.MessageEvent{CreatedAt: now, MessageID: id, ConversationID: types.ConversationID(conv), Role: "assistant", Text: text, Sources: sources, Changes: changes}
	if output.Dropped != nil && len(dropped) > 0 {
		output.Dropped(len(dropped))
	}
	if output.Publish != nil {
		output.Publish(ev)
	}
	return ev, nil
}

func cloneSources(sources []api.SourceRef) []api.SourceRef {
	out := append([]api.SourceRef(nil), sources...)
	for i := range out {
		out[i].Boxes = append(out[i].Boxes[:0:0], out[i].Boxes...)
	}
	return out
}
