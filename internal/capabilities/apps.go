package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Apps with a contextual agent (Section 14.8). Each application owns
// its UI; the harness side is the contract: a compact app_state
// provider the compiler includes as an L3 delta when it changes,
// operations on the app state with the app's own undo, manual-change
// events keeping user and agent edits synchronized, and a scoped
// conversation (app:<id>) with continuity over past actions. The
// app's own rules surface as checks the harness treats as acceptance
// criteria (for example the classroom quiz rule).

// AppKinds lists the applications of Section 14.8 with the tool each
// operation rides on.
var AppKinds = []string{
	"spreadsheet", "model3d", "video", "design", "whiteboard",
	"course", "jobtracker", "monitor", "simulation", "bi",
}

// AppsManifest publishes app_state plus one operation per application.
func AppsManifest() Manifest {
	ops := []Operation{
		Op("app_state", "Get the compact state of an app instance and the recent user actions. Fast", "read_only", "pure", "immediate", true, "apps", []string{"app", "state", "context", "status"}, ObjSchema([]string{"instance"}, map[string]string{"instance": "string", "since": "string"})),
		Op("spreadsheet_ops", "Read and modify spreadsheet content: cells, ranges, formulas, formats, sheets, charts and tables", "write_local", "keyed", "cooperative", false, "apps", []string{"spreadsheet", "cells", "formulas", "sheets"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("model3d_ops", "Create and modify geometry in the 3D editor: solids, transforms, dimensions, layers, views, undo", "write_local", "keyed", "cooperative", false, "apps", []string{"model3d", "geometry", "solids", "transform"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("video_timeline_ops", "Operate the video editor timeline: clips, tracks, cuts, transitions, audio, subtitles, export", "write_local", "keyed", "cooperative", false, "apps", []string{"video", "timeline", "clips", "subtitles"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("design_canvas_ops", "Modify the design copilot canvas: elements, texts, images, colors, typography, layout, templates", "write_local", "keyed", "cooperative", false, "apps", []string{"design", "canvas", "layout", "template"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("whiteboard_ops", "Open the interactive whiteboard and draw, write, annotate and clear to explain content", "write_local", "keyed", "cooperative", false, "apps", []string{"whiteboard", "draw", "explain", "annotate"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("course_ops", "Create and update courses, syllabi, classes and quizzes; record progress. Topics complete only with every answer correct", "write_local", "keyed", "cooperative", false, "apps", []string{"course", "quiz", "syllabus", "class"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("job_tracker_ops", "Create and update job applications with states, documents, evidence, messages and next actions", "write_local", "keyed", "cooperative", false, "apps", []string{"job", "tracker", "application", "cv"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("monitor_ops", "Configure information monitor sources, regions, layers, alerts and priorities", "write_local", "keyed", "cooperative", false, "apps", []string{"monitor", "alerts", "sources", "map"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("simulation_ops", "Define scenarios and parameters, run simulations and read their results", "write_local", "keyed", "cooperative", false, "apps", []string{"simulation", "scenario", "parameters", "run"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
		Op("bi_ops", "Query and visualize data in the business-intelligence app and manage its governance", "read_only", "keyed", "cooperative", false, "apps", []string{"bi", "data", "query", "dashboard"}, ObjSchema([]string{"instance", "ops"}, map[string]string{"instance": "string", "ops": "string"})),
	}
	return Manifest{
		ID: "apps", Version: "1",
		Operations: ops,
		Platforms:  []string{"linux"},
		Profiles:   []string{"default", "delegate", "eval"},
	}
}

// AppEvent is one manual user change inside an application. The
// compiler includes events since the last compile as an L3 delta, so
// the next agent operation starts from the updated state.
type AppEvent struct {
	Seq    int    `json:"seq"`
	Actor  string `json:"actor"`
	Change string `json:"change"`
	At     string `json:"at"`
}

// QuizQuestion is one multiple-choice or matching question of a
// course topic. A topic completes only when every answer is correct
// (Section 14.8).
type QuizQuestion struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Prompt  string `json:"prompt"`
	Answer  string `json:"answer"`
	Given   string `json:"given,omitempty"`
	Correct bool   `json:"correct,omitempty"`
}

type appInstance struct {
	id     string
	kind   string
	state  map[string]any
	events []AppEvent
	seq    int
	quiz   map[string][]QuizQuestion
}

// AppsFake is the deterministic apps simulation: per-instance state,
// manual-change events with delta reads, and the classroom completion
// rule exposed as a check.
type AppsFake struct {
	mu        sync.Mutex
	instances map[string]*appInstance
	dedup     KeyDedup
	calls     map[string]int
	effects   map[string]int
}

// NewAppsFake returns an apps simulation.
func NewAppsFake() *AppsFake {
	return &AppsFake{instances: map[string]*appInstance{}, calls: map[string]int{}, effects: map[string]int{}}
}

// Manifest implements Capability.
func (a *AppsFake) Manifest() Manifest { return AppsManifest() }

// Register creates an application instance of kind. Registering twice
// keeps the existing state: the first registration wins, mirroring
// the single capability.register of a live application.
func (a *AppsFake) Register(id, kind string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.instances[id]; ok {
		return
	}
	a.instances[id] = &appInstance{id: id, kind: kind, state: map[string]any{"kind": kind}, quiz: map[string][]QuizQuestion{}}
}

// Scope returns the conversation scope of an instance (app:<id>).
func Scope(instance string) string { return "app:" + instance }

// EmitUserChange records a manual user change inside the app.
func (a *AppsFake) EmitUserChange(instance, change string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst, ok := a.instances[instance]
	if !ok {
		return -1
	}
	inst.seq++
	inst.events = append(inst.events, AppEvent{Seq: inst.seq, Actor: "user", Change: change, At: "now"})
	return inst.seq
}

// DeltaSince returns the manual-change events after seq for the L3
// compiler delta.
func (a *AppsFake) DeltaSince(instance string, seq int) []AppEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst, ok := a.instances[instance]
	if !ok {
		return nil
	}
	out := []AppEvent{}
	for _, e := range inst.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}

// SetQuiz fixes the quiz of a course topic for tests.
func (a *AppsFake) SetQuiz(instance, topic string, questions []QuizQuestion) {
	a.mu.Lock()
	defer a.mu.Unlock()
	inst, ok := a.instances[instance]
	if !ok {
		return
	}
	inst.quiz[topic] = questions
}

// Quiz returns the current quiz state of a topic.
func (a *AppsFake) Quiz(instance, topic string) []QuizQuestion {
	a.mu.Lock()
	defer a.mu.Unlock()
	if inst, ok := a.instances[instance]; ok {
		return append([]QuizQuestion(nil), inst.quiz[topic]...)
	}
	return nil
}

// Invoke implements Capability.
func (a *AppsFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls[operation]++
	instance := argString(args, "instance")
	if operation != "app_state" && instance == "" {
		return Failed("", "apps: instance is required", ErrInput), nil
	}
	inst, ok := a.instances[instance]
	if !ok && operation != "app_state" {
		return Failed("", fmt.Sprintf("apps: unknown instance %q", instance), ErrInput), nil
	}
	switch operation {
	case "app_state":
		if !ok {
			return Failed("", fmt.Sprintf("apps: unknown instance %q", instance), ErrInput), nil
		}
		return Completed(instance, map[string]any{
			"instance": instance, "scope": Scope(instance),
			"state": inst.state, "recent": recent(inst, 5), "seq": inst.seq,
		}), nil
	case "course_ops":
		return a.course(inst, args, key)
	case "spreadsheet_ops", "model3d_ops", "video_timeline_ops",
		"design_canvas_ops", "whiteboard_ops", "job_tracker_ops",
		"monitor_ops", "simulation_ops", "bi_ops":
		return a.apply(inst, operation, args, key)
	default:
		// Route-2 extensions add their own operations with the same
		// contract: they mutate the same instance state the user
		// edits, so manual changes stay synchronized.
		return a.apply(inst, operation, args, key)
	}
}

func (a *AppsFake) apply(inst *appInstance, operation string, args json.RawMessage, key string) (TaskResult, error) {
	ops := argString(args, "ops")
	if ops == "" {
		return Failed("", fmt.Sprintf("apps: %s needs ops", operation), ErrInput), nil
	}
	if a.dedup.First(operation + "|" + key) {
		a.effects[operation]++
	}
	inst.seq++
	inst.state["last_ops"] = ops
	inst.events = append(inst.events, AppEvent{Seq: inst.seq, Actor: "agent", Change: operation + ": " + ops, At: "now"})
	return Completed(inst.id, map[string]any{"instance": inst.id, "applied": ops, "seq": inst.seq}), nil
}

// course enforces the classroom completion rule: a topic completes
// only when every quiz answer is correct; otherwise the check fails
// and the harness treats it as an unmet acceptance criterion.
func (a *AppsFake) course(inst *appInstance, args json.RawMessage, key string) (TaskResult, error) {
	ops := argString(args, "ops")
	if ops == "" {
		return Failed("", "apps: course_ops needs ops", ErrInput), nil
	}
	if a.dedup.First("course_ops|" + key) {
		a.effects["course_ops"]++
	}
	var op struct {
		Action  string            `json:"action"`
		Topic   string            `json:"topic"`
		Answers map[string]string `json:"answers"`
	}
	if err := json.Unmarshal([]byte(ops), &op); err != nil {
		inst.seq++
		inst.state["last_ops"] = ops
		return Completed(inst.id, map[string]any{"instance": inst.id, "applied": ops}), nil
	}
	switch op.Action {
	case "complete_topic":
		quiz := inst.quiz[op.Topic]
		if len(quiz) == 0 {
			return Failed(inst.id, fmt.Sprintf("course: topic %q has no quiz", op.Topic), ErrInput), nil
		}
		for i, q := range quiz {
			given := op.Answers[q.ID]
			quiz[i].Given = given
			quiz[i].Correct = given != "" && strings.EqualFold(strings.TrimSpace(given), strings.TrimSpace(q.Answer))
		}
		inst.quiz[op.Topic] = quiz
		wrong := []string{}
		for _, q := range quiz {
			if !q.Correct {
				wrong = append(wrong, q.ID)
			}
		}
		if len(wrong) > 0 {
			return Failed(inst.id,
				fmt.Sprintf("course: topic %q is not complete: wrong answers in %s (every answer must be correct)", op.Topic, strings.Join(wrong, ", ")),
				ErrInput), nil
		}
		inst.seq++
		inst.state["completed_"+op.Topic] = true
		return Completed(inst.id, map[string]any{"instance": inst.id, "topic": op.Topic, "completed": true}), nil
	default:
		inst.seq++
		inst.state["last_ops"] = ops
		return Completed(inst.id, map[string]any{"instance": inst.id, "applied": ops}), nil
	}
}

// Poll implements Capability.
func (a *AppsFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (a *AppsFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (a *AppsFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return a.Poll(taskID)
}

func recent(inst *appInstance, n int) []AppEvent {
	if len(inst.events) <= n {
		return append([]AppEvent(nil), inst.events...)
	}
	return append([]AppEvent(nil), inst.events[len(inst.events)-n:]...)
}
