package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

func TestClassroomMessageContextFollowsQueuedTurn(t *testing.T) {
	type observed struct {
		prompt string
		app    *api.AppContext
	}
	seen := make(chan observed, 3)
	b, _ := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		var text strings.Builder
		for _, m := range req.Messages {
			text.WriteString(m.Content)
		}
		seen <- observed{prompt: text.String(), app: api.AppContextFrom(ctx)}
		return textReply("Response"), nil
	})
	server := httptest.NewServer(gateway.New(gateway.Config{}, b, gateway.NewMemoryBlobs(), b.Bus))
	defer server.Close()
	inputs := []*api.AppContext{{App: "classroom", CourseID: "course-a", TopicID: "topic-a"}, {App: "classroom", CourseID: "course-b"}, nil}
	for i, app := range inputs {
		key := []string{"first", "second", "third"}[i]
		req, _ := api.NewRequest(key, api.MethodConversationSend, api.ConversationSendParams{ConversationID: "one-history", ClientMsgID: key, Text: "Explain this", Context: app})
		raw, _ := json.Marshal(req)
		response, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		var result api.Response
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || result.Error != nil {
			t.Fatal(result, err)
		}
		history := awaitAssistant(t, b, "one-history", i+1)
		if len(history) != (i+1)*2 {
			t.Fatal("conversation history split", history)
		}
		got := <-seen
		if app == nil {
			if got.app != nil || strings.Contains(got.prompt, "Current message application context") {
				t.Fatal("context leaked from prior turn", got)
			}
		} else {
			if got.app == nil || *got.app != *app || !strings.Contains(got.prompt, `"course_id":"`+app.CourseID+`"`) {
				t.Fatal("message context lost", got)
			}
			if i == 1 && strings.Contains(got.prompt, `"course_id":"course-a"`) {
				t.Fatal("stale course context", got)
			}
		}
		for _, message := range history {
			if message.Role == "user" && message.Text != "Explain this" {
				t.Fatal("context changed stored message text", message)
			}
		}
	}
	normalized := b.Master.TurnHistory("one-history", 0)
	if normalized[0].AppContext == nil || normalized[0].AppContext.CourseID != "course-a" || normalized[2].AppContext.CourseID != "course-b" {
		t.Fatal("context missing from queued envelope")
	}
}

func TestClassroomMessageContextValidation(t *testing.T) {
	backend := NewDaemonBackend(nil, nil, nil)
	server := httptest.NewServer(gateway.New(gateway.Config{}, backend, gateway.NewMemoryBlobs(), nil))
	defer server.Close()
	for _, app := range []api.AppContext{{App: "other"}, {App: "classroom", TopicID: "topic"}} {
		req, _ := api.NewRequest("bad", api.MethodConversationSend, api.ConversationSendParams{ConversationID: "history", ClientMsgID: "message", Text: "Explain", Context: &app})
		raw, _ := json.Marshal(req)
		response, err := http.Post(server.URL+"/rpc", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		var result api.Response
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || result.Error == nil || result.Error.Code != api.CodeInvalidParams {
			t.Fatal(result, err)
		}
	}
}
