package daemon

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// App tools are selected from the message envelope, independently of lexical
// intent ranking. Study intent uses the same selection without app context.
func (s *Services) chatToolActivation(request, model string, app *api.AppContext) wireengine.ToolActivation {
	activation := wireengine.SuggestTools(s.Catalog, s.Skills, request, model, "default")
	// Personal-agent actions must remain discoverable even when broad words
	// such as "create" rank unrelated deferred tools ahead of them.
	for _, name := range []string{"manage_agents", "send_to_agent", "search_agent_chats", "design_avatar"} {
		if slices.Contains(activation.Names, name) {
			continue
		}
		tool, err := s.Tools.Get(name)
		if err != nil {
			continue
		}
		def := tool.Definition()
		for _, keyword := range def.Keywords {
			if strings.Contains(strings.ToLower(request), strings.ToLower(keyword)) {
				activation.Names = append(activation.Names, name)
				activation.Definitions = append(activation.Definitions, def)
				break
			}
		}
	}
	if (app == nil || app.App != "classroom") && !slices.Contains(activation.Names, "classroom") {
		return activation
	}
	for _, name := range []string{"classroom", "whiteboard"} {
		if slices.Contains(activation.Names, name) || name == "whiteboard" && !whiteboard.Enabled() {
			continue
		}
		if tool, err := s.Tools.Get(name); err == nil {
			activation.Names = append(activation.Names, name)
			activation.Definitions = append(activation.Definitions, tool.Definition())
		}
	}
	if s.Skills != nil {
		if _, ok := s.Skills.Get("course-creation"); ok && !slices.ContainsFunc(activation.Skills, func(h ctxcompiler.SkillView) bool { return h.Name == "course-creation" }) {
			activation.Skills = append(activation.Skills, ctxcompiler.SkillView{Name: "course-creation", Why: "Create courses and prepare classroom lessons, quizzes and feedback."})
		}
	}
	return activation
}

func (a *assistantTurns) appContextNote(ctx context.Context, app *api.AppContext) string {
	if app == nil || app.App != "classroom" {
		return ""
	}
	// Structured values are view data, not instructions. No lesson bodies,
	// questions, answer keys or arbitrary user-controlled app text enter this note.
	if app.CourseID == "" {
		return "Current app view (data for this turn only): the user is in the classroom home."
	}
	fields := map[string]any{"app": "classroom", "course_id": app.CourseID}
	if app.TopicID != "" {
		fields["topic_id"] = app.TopicID
	}
	fields["status"] = "unavailable"
	if a.services != nil && a.services.ClassroomAgent != nil {
		if c, err := a.services.ClassroomAgent.GetCourse(ctx, app.CourseID); err == nil {
			fields["course_title"] = boundedTurnText(c.Title, 160)
			fields["progress"] = min(100, max(0, c.Progress))
			fields["status"] = "in_progress"
			if c.FinalExam.Passed {
				fields["status"] = "completed"
			}
			if app.TopicID != "" {
				fields["topic_status"] = "unavailable"
				for _, u := range c.Units {
					for _, t := range u.Topics {
						if t.ID == app.TopicID {
							fields["topic_title"] = boundedTurnText(t.Title, 160)
							// Only contract states are admitted as a status value.
							switch t.Status {
							case "not_started", "in_progress", "completed":
								fields["topic_status"] = t.Status
							}
						}
					}
				}
			}
		}
	}
	raw, _ := json.Marshal(fields)
	return "Current app view (data for this turn only): " + string(raw)
}
