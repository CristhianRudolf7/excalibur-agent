package daemon

import (
	"context"
	"encoding/json"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
)

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
