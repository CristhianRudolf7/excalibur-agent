package wireengine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// ToolActivation is a bounded, run-local selection, separate from the core prefix.
type ToolActivation struct {
	Definitions []tools.Definition      `json:"-"`
	Names       []string                `json:"tools"`
	Skills      []ctxcompiler.SkillView `json:"skills,omitempty"`
}

// SuggestTools extends deferred search and skill suggestions at the user-message
// boundary. Eval keeps its frozen tool surface and prompt byte-for-byte.
func SuggestTools(cat *tools.DeferredCatalog, ix *skills.Index, request, model, profile string) ToolActivation {
	var activation ToolActivation
	if cat == nil || profile == "eval" {
		return activation
	}
	for _, hit := range cat.Suggest(request) {
		activation.Definitions = append(activation.Definitions, hit.Definition)
		activation.Names = append(activation.Names, hit.Definition.Name)
	}
	if ix != nil && len(activation.Names) > 0 {
		query := request + " " + strings.ReplaceAll(activation.Names[0], "_", " ")
		for _, hint := range ix.SuggestForTools(query, model, activation.Names) {
			activation.Skills = append(activation.Skills, ctxcompiler.SkillView{Name: hint.Name, Why: hint.Why})
		}
	}
	return activation
}

// Record persists the selected tool and hint names before a model sees them.
// The request text is deliberately absent from this diagnostic record.
func (a ToolActivation) Record(ctx context.Context, store journal.Store, runID string) error {
	if store == nil || len(a.Names) == 0 {
		return nil
	}
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = store.Append(ctx, journal.Entry{RunID: runID, Type: "tools_activated", Payload: data})
	return err
}
