package classroom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Tool exposes trusted classroom writes and answer-bearing reads to the agent.
type Tool struct{ Service AgentService }

func (*Tool) Definition() tools.Definition {
	return tools.Definition{
		Name: "classroom", Description: "Create personalized courses with a complete syllabus, read course and quiz history with answers, store markdown classes, generate new closed quizzes and write feedback without revealing correct options. Load course-creation for study requests. Use whiteboard for visual teaching; always save the markdown with write_class. Final exams require max(10, twice the largest topic quiz) questions.",
		InputSchema: json.RawMessage(toolSchema),
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemNonIdempotent,
		Cancel: types.CancelCooperative, Timeout: 30 * time.Second, OutputBudget: 24000,
		Fast: true, Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "personal-assistant", Keywords: []string{"course", "curso", "learn", "aprender", "study", "estudiar", "class", "clase", "quiz", "exam", "teach", "syllabus", "classroom"},
	}
}

const toolSchema = `{"type":"object","required":["action"],"additionalProperties":false,"properties":{"action":{"type":"string","enum":["create_course","update_syllabus","get_course","write_class","create_quiz","write_feedback","list_courses"]},"course_id":{"type":"string"},"topic_id":{"type":"string"},"attempt_id":{"type":"string"},"title":{"type":"string"},"goal":{"type":"string"},"level":{"type":"string"},"language":{"type":"string"},"units":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"title":{"type":"string"},"topics":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["title","summary"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"summary":{"type":"string"}}}}},"required":["topics"]}},"format":{"enum":["chat","whiteboard"]},"content":{"type":"string"},"whiteboard_id":{"type":"string"},"final":{"type":"boolean"},"questions":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["kind","prompt","explanation"],"properties":{"id":{"type":"string"},"kind":{"enum":["single","multiple","match"]},"prompt":{"type":"string"},"options":{"type":"array","items":{"type":"string"}},"correct":{"type":"array","items":{"type":"integer"}},"left":{"type":"array","items":{"type":"string"}},"right":{"type":"array","items":{"type":"string"}},"pairs":{"type":"array","items":{"type":"integer"}},"explanation":{"type":"string"}}}},"feedback":{"type":"string"}}}`

type toolAction struct {
	Action       string             `json:"action"`
	CourseID     string             `json:"course_id"`
	TopicID      string             `json:"topic_id"`
	AttemptID    string             `json:"attempt_id"`
	Title        string             `json:"title"`
	Goal         string             `json:"goal"`
	Level        string             `json:"level"`
	Language     string             `json:"language"`
	Units        []toolSyllabusUnit `json:"units"`
	Format       string             `json:"format"`
	Content      string             `json:"content"`
	WhiteboardID string             `json:"whiteboard_id"`
	Final        bool               `json:"final"`
	Questions    []Question         `json:"questions"`
	Feedback     string             `json:"feedback"`
}

type toolSyllabusUnit struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Topics []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
	} `json:"topics"`
}

func (*Tool) ResourceKeys(json.RawMessage) ([]string, error) { return []string{"classroom"}, nil }

func (t *Tool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	fail := func(status tools.ResultStatus, err error) (tools.Result, error) {
		return tools.Result{Status: status, Output: err.Error()}, nil
	}
	if err := ctx.Err(); err != nil {
		return fail(tools.StatusCanceled, err)
	}
	if len(inv.Arguments) > 2<<20 || !json.Valid(inv.Arguments) {
		return fail(tools.StatusInputError, fmt.Errorf("invalid or oversized classroom arguments"))
	}
	var a toolAction
	d := json.NewDecoder(bytes.NewReader(inv.Arguments))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return fail(tools.StatusInputError, err)
	}
	if err := a.validate(); err != nil {
		return fail(tools.StatusInputError, err)
	}
	if t.Service == nil {
		return fail(tools.StatusToolError, fmt.Errorf("classroom requires the daemon; start it with excalibur daemon start and use the classroom chat"))
	}
	out, err := t.execute(ctx, a)
	if err != nil {
		status := tools.StatusToolError
		var e *api.Error
		if errors.As(err, &e) && (e.Code == api.CodeInvalidParams || e.Code == api.CodeUnknownTarget || e.Code == api.CodeConflict) {
			status = tools.StatusInputError
		}
		if ctx.Err() != nil {
			status = tools.StatusCanceled
		}
		return fail(status, err)
	}
	raw, err := json.Marshal(out)
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, err
}

func agentInputError(message string) error {
	return &api.Error{Code: api.CodeInvalidParams, Message: message}
}
func hasToolFields(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return false
		}
	}
	return true
}

func (a toolAction) syllabus() []Unit {
	units := make([]Unit, len(a.Units))
	for i, u := range a.Units {
		units[i] = Unit{ID: u.ID, Title: u.Title, Topics: make([]Topic, len(u.Topics))}
		for j, t := range u.Topics {
			units[i].Topics[j] = Topic{ID: t.ID, Title: t.Title, Summary: t.Summary}
		}
	}
	return units
}

func (a toolAction) validate() error {
	switch a.Action {
	case "create_course":
		if !hasToolFields(a.Title, a.Goal, a.Level, a.Language) {
			return agentInputError("title, goal, level and language are required")
		}
	case "update_syllabus", "get_course", "write_class", "create_quiz":
		if !hasToolFields(a.CourseID) {
			return agentInputError("course_id is required")
		}
	case "write_feedback":
		if !hasToolFields(a.AttemptID, a.Feedback) {
			return agentInputError("attempt_id and feedback are required")
		}
	case "list_courses":
	default:
		return agentInputError("unknown classroom action")
	}
	if a.Action == "create_course" || a.Action == "update_syllabus" {
		if len(a.Units) == 0 {
			return agentInputError("a complete syllabus is required")
		}
		seen := map[string]bool{}
		for _, u := range a.Units {
			if len(u.Topics) == 0 {
				return agentInputError("every unit needs topics")
			}
			if u.ID != "" {
				if seen[u.ID] {
					return agentInputError("duplicate syllabus id")
				}
				seen[u.ID] = true
			}
			for _, t := range u.Topics {
				if !hasToolFields(t.Title, t.Summary) {
					return agentInputError("every topic needs title and summary")
				}
				if t.ID != "" {
					if seen[t.ID] {
						return agentInputError("duplicate syllabus id")
					}
					seen[t.ID] = true
				}
			}
		}
	}
	if a.Action == "write_class" {
		if !hasToolFields(a.TopicID, a.Content) || (a.Format != "chat" && a.Format != "whiteboard") {
			return agentInputError("topic_id, markdown content and format chat or whiteboard are required")
		}
		if a.Format == "whiteboard" && !hasToolFields(a.WhiteboardID) {
			return agentInputError("whiteboard_id is required for a whiteboard class")
		}
		if a.Format == "chat" && a.WhiteboardID != "" {
			return agentInputError("chat classes cannot have a whiteboard_id")
		}
	}
	if a.Action == "create_quiz" {
		if a.Final == hasToolFields(a.TopicID) {
			return agentInputError("select exactly one topic_id or final:true")
		}
		if len(a.Questions) == 0 {
			return agentInputError("questions are required")
		}
		ids := map[string]bool{}
		for _, q := range a.Questions {
			if q.ID != "" {
				if ids[q.ID] {
					return agentInputError("duplicate question id")
				}
				ids[q.ID] = true
			}
			if err := validateToolQuestion(q); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateToolQuestion(q Question) error {
	if !hasToolFields(q.Prompt, q.Explanation) {
		return agentInputError("every question needs prompt and explanation")
	}
	switch q.Kind {
	case KindSingle, KindMultiple:
		if len(q.Options) < 2 || !hasToolFields(q.Options...) || len(q.Correct) == 0 || (q.Kind == KindSingle && len(q.Correct) != 1) || len(q.Left)+len(q.Right)+len(q.Pairs) > 0 {
			return agentInputError("invalid choice question")
		}
		if !validToolIndexes(q.Correct, len(q.Options)) {
			return agentInputError("correct indexes must be unique and in range")
		}
	case KindMatch:
		if len(q.Left) < 2 || len(q.Left) != len(q.Right) || len(q.Pairs) != len(q.Left) || !hasToolFields(q.Left...) || !hasToolFields(q.Right...) || len(q.Options)+len(q.Correct) > 0 || !validToolIndexes(q.Pairs, len(q.Right)) {
			return agentInputError("matching pairs must be a permutation of equally sized left/right lists")
		}
	default:
		return agentInputError("only single, multiple and match questions are supported")
	}
	return nil
}
func validToolIndexes(xs []int, n int) bool {
	seen := map[int]bool{}
	for _, x := range xs {
		if x < 0 || x >= n || seen[x] {
			return false
		}
		seen[x] = true
	}
	return true
}

func agentCourseSummary(c Course) any {
	topics, completed := 0, 0
	for _, u := range c.Units {
		for _, t := range u.Topics {
			topics++
			if t.Status == "completed" {
				completed++
			}
		}
	}
	return map[string]any{"course_id": c.ID, "title": c.Title, "progress": c.Progress, "topics": topics, "completed_topics": completed}
}
func (t *Tool) execute(ctx context.Context, a toolAction) (any, error) {
	s := t.Service
	switch a.Action {
	case "create_course":
		c, err := s.CreateCourse(ctx, Course{Title: a.Title, Goal: a.Goal, Level: a.Level, Language: a.Language, Units: a.syllabus()})
		return agentCourseSummary(c), err
	case "update_syllabus":
		c, err := s.UpdateSyllabus(ctx, a.CourseID, a.syllabus())
		return agentCourseSummary(c), err
	case "get_course":
		return s.GetCourse(ctx, a.CourseID)
	case "write_class":
		_, err := s.WriteClass(ctx, a.CourseID, a.TopicID, Class{Format: a.Format, Content: a.Content, WhiteboardID: a.WhiteboardID})
		return map[string]any{"course_id": a.CourseID, "topic_id": a.TopicID, "stored": err == nil}, err
	case "create_quiz":
		c, err := s.GetCourse(ctx, a.CourseID)
		if err != nil {
			return nil, err
		}
		if a.Final {
			if !c.FinalExam.Unlocked {
				return nil, agentInputError("final exam is locked")
			}
			minimum := MinFinalQuestions
			for _, q := range c.Quizzes {
				if !q.Final {
					minimum = max(minimum, 2*len(q.Questions))
				}
			}
			if len(a.Questions) < minimum {
				return nil, agentInputError(fmt.Sprintf("final exam requires at least %d questions", minimum))
			}
		} else {
			found := false
			for _, u := range c.Units {
				for _, topic := range u.Topics {
					found = found || topic.ID == a.TopicID
				}
			}
			if !found {
				return nil, agentInputError("unknown topic_id")
			}
		}
		q, err := s.CreateQuiz(ctx, Quiz{CourseID: a.CourseID, TopicID: a.TopicID, Final: a.Final, Questions: a.Questions})
		return map[string]any{"quiz_id": q.ID, "question_count": len(q.Questions), "final": q.Final}, err
	case "write_feedback":
		at, err := s.WriteFeedback(ctx, a.AttemptID, a.Feedback)
		return map[string]any{"attempt_id": at.ID, "stored": err == nil}, err
	case "list_courses":
		cs, err := s.ListCourses(ctx)
		out := make([]any, 0, len(cs))
		for _, c := range cs {
			out = append(out, agentCourseSummary(c))
		}
		return map[string]any{"courses": out}, err
	}
	return nil, agentInputError("unknown classroom action")
}
