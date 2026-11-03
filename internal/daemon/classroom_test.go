package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/classroom"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/coder/websocket"
)

func TestClassroomGatewayRoutes(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			bus := telemetry.NewBus(nil)
			service := classroom.NewService(nil, bus)
			c, err := service.CreateCourse(ctx, classroom.Course{Title: "Course", Units: []classroom.Unit{{Topics: []classroom.Topic{{Title: "Topic"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			topicID := c.Units[0].Topics[0].ID
			backend := NewDaemonBackend(nil, nil, bus)
			backend.AttachServices(BackendServices{Classroom: service})
			server := httptest.NewServer(gateway.New(gateway.Config{}, backend, gateway.NewMemoryBlobs(), bus))
			defer server.Close()
			var conn *websocket.Conn
			if transport == "websocket" {
				conn, _, err = websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close(websocket.StatusNormalClosure, "")
			}
			call := func(method string, params any) json.RawMessage {
				t.Helper()
				req, err := api.NewRequest("request", method, params)
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(req)
				var result api.Response
				if conn != nil {
					if err = conn.Write(ctx, websocket.MessageText, raw); err != nil {
						t.Fatal(err)
					}
					_, raw, err = conn.Read(ctx)
					if err != nil {
						t.Fatal(err)
					}
					err = json.Unmarshal(raw, &result)
				} else {
					response, e := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
					if e != nil {
						t.Fatal(e)
					}
					defer response.Body.Close()
					err = json.NewDecoder(response.Body).Decode(&result)
				}
				if err != nil || result.Error != nil {
					t.Fatalf("%s: %+v %v", method, result, err)
				}
				return result.Result
			}
			call(api.MethodClassroomList, struct{}{})
			call(api.MethodClassroomGet, api.ClassroomCourseParams{CourseID: c.ID})
			call(api.MethodClassroomUpdateSyllabus, api.ClassroomUpdateSyllabusParams{CourseID: c.ID, Units: []api.ClassroomSyllabusUnit{{ID: c.Units[0].ID, Topics: []api.ClassroomSyllabusTopic{{ID: topicID, Title: "Edited"}}}}})
			call(api.MethodClassroomOpenTopic, api.ClassroomTopicParams{CourseID: c.ID, TopicID: topicID})
			call(api.MethodClassroomRequestQuiz, api.ClassroomRequestQuizParams{CourseID: c.ID, TopicID: topicID})
			q, err := service.CreateQuiz(ctx, c.ID, topicID, false, []classroom.Question{{Kind: classroom.KindSingle, Prompt: "Pick", Options: []string{"a", "b"}, Correct: []int{1}}})
			if err != nil {
				t.Fatal(err)
			}
			quiz := call(api.MethodClassroomGetQuiz, api.ClassroomGetQuizParams{QuizID: q.ID})
			if bytes.Contains(quiz, []byte(`"correct"`)) {
				t.Fatal(string(quiz))
			}
			call(api.MethodClassroomSubmit, api.ClassroomSubmitParams{QuizID: q.ID, Answers: []api.ClassroomAnswer{{QuestionID: q.Questions[0].ID, Selected: []int{1}}}})
			call(api.MethodClassroomAttempts, api.ClassroomAttemptsParams{CourseID: c.ID})
			call(api.MethodClassroomDelete, api.ClassroomCourseParams{CourseID: c.ID})
		})
	}
}

func TestClassroomAssemblySharesProvidedService(t *testing.T) {
	service := classroom.NewService(nil, telemetry.NewBus(nil))
	s, err := Assemble(AssembleOptions{Classroom: service})
	if err != nil {
		t.Fatal(err)
	}
	if s.Classroom != service {
		t.Fatal("assembly replaced provided classroom service")
	}
	tool, err := s.Tools.Get("classroom")
	if err != nil {
		t.Fatal(err)
	}
	adapter, ok := tool.(*classroom.Tool).Service.(classroom.AgentAdapter)
	if !ok || adapter.Service != service {
		t.Fatal("tool does not share the RPC service")
	}
	course, err := adapter.CreateCourse(context.Background(), classroom.Course{Title: "Shared", Units: []classroom.Unit{{Topics: []classroom.Topic{{Title: "Topic"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetCourse(context.Background(), course.ID); err != nil {
		t.Fatal(err)
	}
	defaults, err := Assemble(AssembleOptions{})
	if err != nil || defaults.Classroom == nil {
		t.Fatal(defaults, err)
	}
	backend := NewDaemonBackend(nil, nil, nil)
	_, err = backend.Passthrough(context.Background(), api.MethodClassroomList, json.RawMessage(`{}`))
	var e *api.Error
	if !errors.As(err, &e) || e.Code != api.CodeInternal {
		t.Fatal(err)
	}
}
