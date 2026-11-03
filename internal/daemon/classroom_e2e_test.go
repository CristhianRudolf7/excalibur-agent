package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Exercise the production tool adapter, event subscriber, conversation engine,
// SQLite service and browser RPC views together, without a network model.
func TestClassroomLearningFlowThroughAgentAndRPC(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "classroom.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bus := telemetry.NewBus(nil)
	service := classroom.NewService(classroom.NewStore(db), bus)
	type script struct {
		instruction string
		actions     []func(string) any
	}
	scripts := make(chan script, 1)
	var current *script
	step := 0
	lastOutput := ""
	model := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "classroom-test", Model: "fake"}}}})
	defer model.Close()
	model.Register(&turnProvider{FakeProvider: fake.New("classroom-test"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		if current == nil {
			select {
			case next := <-scripts:
				current = &next
				step = 0
			default:
				return providers.ChatResponse{}, fmt.Errorf("unexpected classroom agent turn")
			}
			found := false
			for _, m := range req.Messages {
				found = found || strings.Contains(m.Content, current.instruction)
			}
			if !found {
				t.Errorf("missing turn instruction %q", current.instruction)
			}
		}
		if step > 0 {
			var result *providers.ToolResult
			for i := range req.Messages {
				if req.Messages[i].ToolResult != nil {
					result = req.Messages[i].ToolResult
				}
			}
			if result == nil || result.IsError {
				return providers.ChatResponse{}, fmt.Errorf("classroom tool failed: %+v", result)
			}
			lastOutput = result.Output
		}
		if step == len(current.actions) {
			current = nil
			return textReply("Prepared."), nil
		}
		args, err := json.Marshal(current.actions[step](lastOutput))
		if err != nil {
			return providers.ChatResponse{}, err
		}
		step++
		return providers.ChatResponse{Message: providers.Message{Role: providers.RoleAssistant, ToolCalls: []providers.ToolCall{{ID: fmt.Sprint(step), Name: "classroom", Arguments: args}}}}, nil
	}})
	b := NewDaemonBackend(master.New(master.Config{}, nil, nil, nil, nil, nil, nil), nil, bus)
	b.AttachServices(BackendServices{Classroom: service})
	services, err := Assemble(AssembleOptions{Classroom: service, Bus: bus, Models: model})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, services, model, "fake")
	defer func() {
		cancel()
		services.delegations.stop()
		stop, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = b.Master.Turns().Stop(stop)
	}()
	b.StartClassroomTurns(ctx)
	server := httptest.NewServer(gateway.New(gateway.Config{}, b, gateway.NewMemoryBlobs(), bus))
	defer server.Close()
	serial := 0
	rpc := func(method string, params, out any) json.RawMessage {
		t.Helper()
		serial++
		req, err := api.NewRequest(fmt.Sprint(serial), method, params)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(req)
		res, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var result api.Response
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		if out != nil {
			if err := json.Unmarshal(result.Result, out); err != nil {
				t.Fatal(err)
			}
		}
		return result.Result
	}
	fixed := func(args any) func(string) any { return func(string) any { return args } }
	scripts <- script{instruction: "classroom home", actions: []func(string) any{fixed(map[string]any{
		"action": "create_course", "title": "Algebra", "goal": "Balance equations", "level": "beginner", "language": "en",
		"units": []any{map[string]any{"title": "Foundations", "topics": []any{map[string]any{"title": "Equations", "summary": "Keep both sides balanced"}}}},
	})}}
	rpc(api.MethodConversationSend, api.ConversationSendParams{ConversationID: ClassroomConversation, ClientMsgID: "onboarding", Text: "I want to study algebra", Context: &api.AppContext{App: "classroom"}}, nil)
	awaitAssistant(t, b, ClassroomConversation, 1)
	var list api.ClassroomListResult
	rpc(api.MethodClassroomList, struct{}{}, &list)
	if len(list.Courses) != 1 {
		t.Fatal(list)
	}
	courseID := list.Courses[0].ID
	var course api.ClassroomCourseView
	rpc(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: courseID}, &course)
	topicID := course.Units[0].Topics[0].ID
	read := fixed(map[string]any{"action": "get_course", "course_id": courseID})
	queue := func(instruction string, action func(string) any) {
		scripts <- script{instruction: instruction, actions: []func(string) any{read, action}}
	}
	queue("write_class", fixed(map[string]any{"action": "write_class", "course_id": courseID, "topic_id": topicID, "format": "chat", "content": "# Balance\nApply the same operation to both sides."}))
	rpc(api.MethodClassroomOpenTopic, api.ClassroomTopicParams{CourseID: courseID, TopicID: topicID}, nil)
	awaitAssistant(t, b, ClassroomConversation, 2)
	rpc(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: courseID}, &course)
	if course.Units[0].Topics[0].Class == nil {
		t.Fatal("agent did not store lesson")
	}
	target := api.ClassroomRequestQuizParams{CourseID: courseID, TopicID: topicID}
	question := func(prompt string) any {
		return map[string]any{"kind": "single", "prompt": prompt, "options": []string{"Same operation", "Different operations"}, "correct": []int{0}, "explanation": "PRIVATE_ANSWER_EXPLANATION"}
	}
	quizAction := func(prompt string) func(string) any {
		return fixed(map[string]any{"action": "create_quiz", "course_id": courseID, "topic_id": topicID, "questions": []any{question(prompt)}})
	}
	queue("create_quiz", quizAction("How do you preserve equality?"))
	var requested api.ClassroomRequestQuizResult
	requested = api.ClassroomRequestQuizResult{}
	rpc(api.MethodClassroomRequestQuiz, target, &requested)
	if !requested.Requested {
		t.Fatal(requested)
	}
	awaitAssistant(t, b, ClassroomConversation, 3)
	requested = api.ClassroomRequestQuizResult{}
	rpc(api.MethodClassroomRequestQuiz, target, &requested)
	if requested.Requested || requested.Quiz == nil {
		t.Fatal("unanswered quiz was not reused", requested)
	}
	first := *requested.Quiz
	var publicQuiz api.ClassroomQuizView
	raw := rpc(api.MethodClassroomGetQuiz, api.ClassroomGetQuizParams{QuizID: first.ID}, &publicQuiz)
	if bytes.Contains(raw, []byte(`"correct"`)) || bytes.Contains(raw, []byte("PRIVATE_ANSWER")) {
		t.Fatal("quiz exposed answers", string(raw))
	}
	feedback := func(output string) any {
		var trusted classroom.AgentCourse
		if err := json.Unmarshal([]byte(output), &trusted); err != nil || len(trusted.Attempts) == 0 {
			t.Errorf("missing private attempt history: %v", err)
			return nil
		}
		if len(trusted.Quizzes) == 0 || len(trusted.Quizzes[0].Questions[0].Correct) == 0 {
			t.Error("trusted history omitted answer keys")
		}
		return map[string]any{"action": "write_feedback", "attempt_id": trusted.Attempts[len(trusted.Attempts)-1].ID, "feedback": "Review balancing both sides, then try a fresh question."}
	}
	queue("write_feedback", feedback)
	var wrong api.ClassroomSubmitResult
	raw = rpc(api.MethodClassroomSubmit, api.ClassroomSubmitParams{QuizID: first.ID, Answers: []api.ClassroomAnswer{{QuestionID: first.Questions[0].ID, Selected: []int{1}}}}, &wrong)
	if wrong.Passed || len(wrong.Results) != 1 || wrong.Results[0].Correct || bytes.Contains(raw, []byte("PRIVATE_ANSWER")) || bytes.Contains(raw, []byte(`"selected"`)) {
		t.Fatal("unsafe or incorrect failed result", string(raw))
	}
	awaitAssistant(t, b, ClassroomConversation, 4)
	var attempts api.ClassroomAttemptsResult
	raw = rpc(api.MethodClassroomAttempts, api.ClassroomAttemptsParams{CourseID: courseID, TopicID: topicID}, &attempts)
	if len(attempts.Attempts) != 1 || attempts.Attempts[0].Feedback == "" || bytes.Contains(raw, []byte(`"answers"`)) {
		t.Fatal("missing feedback or unsafe attempt history", string(raw))
	}
	rpc(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: courseID}, &course)
	if course.FinalExam.Unlocked || course.Progress != 0 || course.Units[0].Topics[0].Status == "completed" {
		t.Fatal("wrong attempt completed topic", course)
	}
	queue("create_quiz", quizAction("Which operation keeps an equation balanced?"))
	requested = api.ClassroomRequestQuizResult{}
	rpc(api.MethodClassroomRequestQuiz, target, &requested)
	if !requested.Requested {
		t.Fatal("retry did not request generation")
	}
	awaitAssistant(t, b, ClassroomConversation, 5)
	requested = api.ClassroomRequestQuizResult{}
	rpc(api.MethodClassroomRequestQuiz, target, &requested)
	second := requested.Quiz
	if second == nil || second.ID == first.ID || second.Questions[0].Prompt == first.Questions[0].Prompt {
		t.Fatal("retry did not produce fresh questions", second)
	}
	queue("write_feedback", feedback)
	var right api.ClassroomSubmitResult
	rpc(api.MethodClassroomSubmit, api.ClassroomSubmitParams{QuizID: second.ID, Answers: []api.ClassroomAnswer{{QuestionID: second.Questions[0].ID, Selected: []int{0}}}}, &right)
	if !right.Passed {
		t.Fatal(right)
	}
	awaitAssistant(t, b, ClassroomConversation, 6)
	rpc(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: courseID}, &course)
	if course.Units[0].Topics[0].Status != "completed" || course.Progress != 99 || !course.FinalExam.Unlocked || course.FinalExam.Passed {
		t.Fatal("incorrect completion or exam state", course)
	}
	rpc(api.MethodClassroomList, struct{}{}, &list)
	if list.Courses[0].CompletedTopics != 1 || list.Courses[0].Progress != 99 {
		t.Fatal(list)
	}
	counts := map[types.EventType]int{}
	for _, event := range bus.Replay(types.Cursor{}) {
		counts[event.Type]++
		if strings.HasPrefix(string(event.Type), "classroom.") && bytes.Contains(event.Payload, []byte("PRIVATE_ANSWER")) {
			t.Fatal("event exposed answer explanation", event.Type)
		}
	}
	for kind, want := range map[types.EventType]int{"classroom.course_created": 1, "classroom.class_requested": 1, "classroom.class_ready": 1, "classroom.quiz_requested": 2, "classroom.quiz_ready": 2, "classroom.attempt_graded": 2, "classroom.feedback_ready": 2, "classroom.topic_completed": 1} {
		if counts[kind] != want {
			t.Errorf("%s count = %d, want %d", kind, counts[kind], want)
		}
	}
}
