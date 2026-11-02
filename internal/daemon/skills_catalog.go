package daemon

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
)

func skillSummary(ix *skills.Index, e *skills.Entry) api.SkillSummary {
	state := string(e.State)
	switch e.State {
	case skills.StateActive:
		state = "enabled"
	case skills.StateArchived:
		state = "disabled"
	}
	return api.SkillSummary{Name: e.Name, Group: e.Group, Description: e.Description, Version: e.Version, Source: e.Root, State: state, Editable: ix.Editable(e), Deletable: e.Root != skills.RootBuiltin, Tools: append([]string{}, e.Tools...)}
}

func catalogError(err error) error {
	if err == nil {
		return nil
	}
	var validation *skills.ValidationError
	if errors.As(err, &validation) {
		diagnostics := []string{}
		for _, d := range validation.Diagnostics {
			diagnostics = append(diagnostics, d.Error())
		}
		return &api.Error{Code: api.CodeInvalidParams, Message: "invalid SKILL.md", Data: map[string]any{"diagnostics": diagnostics}}
	}
	if errors.Is(err, skills.ErrNotFound) {
		return &api.Error{Code: api.CodeUnknownTarget, Message: err.Error()}
	}
	if errors.Is(err, skills.ErrBuiltinDelete) {
		return &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	return &api.Error{Code: api.CodeInternal, Message: err.Error()}
}

func skillsCatalogOp(ix *skills.Index, method string, params json.RawMessage) (any, error) {
	if !clientFeatureEnabled("EXCALIBUR_SKILLS_MANAGEMENT") {
		return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "skills management is disabled"}
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	var p struct {
		Name    string `json:"name"`
		Content string `json:"content"`
		State   string `json:"state"`
		api.SkillsListParams
	}
	if err := json.Unmarshal(params, &p); err != nil || string(params) == "null" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "expected skills parameters object"}
	}
	switch method {
	case api.MethodSkillsGroups:
		counts := map[string]int{}
		for _, e := range ix.All() {
			counts[e.Group]++
		}
		groups := []api.SkillGroup{}
		for _, g := range skills.Groups() {
			groups = append(groups, api.SkillGroup{ID: g.ID, NameEN: g.NameEN, NameES: g.NameES, DescriptionEN: g.DescEN, DescriptionES: g.DescES, Count: counts[g.ID]})
		}
		return map[string]any{"groups": groups}, nil
	case api.MethodSkillsList:
		if p.Source != "" && p.Source != skills.RootBuiltin && p.Source != skills.RootUser && p.Source != skills.RootInstalled && p.Source != skills.RootProject {
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "unknown skill source"}
		}
		result := []api.SkillSummary{}
		for _, e := range ix.All() {
			if p.Group != "" && e.Group != p.Group || p.Source != "" && e.Root != p.Source {
				continue
			}
			if p.Text != "" && !strings.Contains(strings.ToLower(e.Name+" "+e.Description), strings.ToLower(p.Text)) {
				continue
			}
			result = append(result, skillSummary(ix, e))
		}
		return map[string]any{"skills": result}, nil
	}
	if p.Name == "" {
		return nil, &api.Error{Code: api.CodeInvalidParams, Message: "skill name is required"}
	}
	e, ok := ix.Get(p.Name)
	if !ok {
		return nil, catalogError(skills.ErrNotFound)
	}
	switch method {
	case api.MethodSkillsGet:
		content, err := ix.Content(p.Name)
		if err != nil {
			return nil, catalogError(err)
		}
		diagnostics := []string{}
		for _, d := range ix.Diagnostics() {
			if d.Skill == p.Name {
				diagnostics = append(diagnostics, d.Error())
			}
		}
		return api.SkillDetail{SkillSummary: skillSummary(ix, e), Content: content, PathKind: e.Root, Diagnostics: diagnostics}, nil
	case api.MethodSkillsUpdate:
		e, err := ix.Update(p.Name, p.Content)
		if err != nil {
			return nil, catalogError(err)
		}
		return skillSummary(ix, e), nil
	case api.MethodSkillsSetState:
		var state skills.State
		switch p.State {
		case "enabled":
			state = skills.StateActive
		case "disabled":
			state = skills.StateArchived
		default:
			return nil, &api.Error{Code: api.CodeInvalidParams, Message: "state must be enabled or disabled"}
		}
		if err := ix.SetState(p.Name, state); err != nil {
			return nil, catalogError(err)
		}
		e, ok = ix.Get(p.Name)
		if !ok {
			return nil, catalogError(skills.ErrNotFound)
		}
		return skillSummary(ix, e), nil
	case api.MethodSkillsDelete:
		if err := ix.Delete(p.Name); err != nil {
			return nil, catalogError(err)
		}
		return map[string]any{"name": p.Name, "deleted": true}, nil
	}
	return nil, &api.Error{Code: api.CodeMethodNotFound, Message: "unknown skills method"}
}
