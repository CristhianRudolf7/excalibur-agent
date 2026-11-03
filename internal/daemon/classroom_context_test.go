package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

type blockedClassroomDecider struct{ release <-chan struct{} }

func (d blockedClassroomDecider) Classify(ctx context.Context, _ string, _ master.Preanalysis) master.Triage {
	select {
	case <-d.release:
	case <-ctx.Done():
	}
	return master.Triage{Type: master.TriageConversation, Confidence: 1}
}

func TestClassroomContextSurvivesDeferredReceipt(t *testing.T) {
	seen := make(chan bool, 1)
	b, s := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		app := api.AppContextFrom(ctx)
		hasNote, hasTool := false, false
		for _, msg := range req.Messages {
			hasNote = hasNote || strings.Contains(msg.Content, "Current app view")
		}
		for _, tool := range req.Tools {
			hasTool = hasTool || tool.Name == "classroom"
		}
		seen <- app != nil && app.CourseID == "course" && app.TopicID == "topic" && hasNote && hasTool
		return textReply("Answer"), nil
	})
	release := make(chan struct{})
	b.Master = master.New(master.Config{}, nil, nil, blockedClassroomDecider{release}, nil, nil, nil)
	WireAssistantTurns(b, s, s.engineOptions.Models, "test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	app := &api.AppContext{App: "classroom", CourseID: "course", TopicID: "topic"}
	id, _, err := b.ConversationSendContext(ctx, "client", "message", "conversation", "Explain this", []string{"document"}, app)
	if err != nil || id == "" || ctx.Err() != nil {
		close(release)
		t.Fatal("receipt waited for blocked triage", id, err)
	}
	app.CourseID = "changed after receipt"
	cancel()
	close(release)
	select {
	case ok := <-seen:
		if !ok {
			t.Fatal("deferred classroom turn lost app context, prompt note or tool")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deferred classroom turn did not run after receipt cancellation")
	}
	awaitAssistant(t, b, "conversation", 1)
	rows := b.Master.TurnHistory("conversation", 0)
	if rows[0].Context == nil || rows[0].Context.CourseID != "course" || len(rows[0].Attachments) != 1 {
		t.Fatal("deferred persistence lost message metadata")
	}
	if _, _, err := b.ConversationSendContext(context.Background(), "client", "stop", "conversation", "/stop", nil, &api.AppContext{App: "classroom"}); err != nil {
		t.Fatal(err)
	}
	history := awaitAssistant(t, b, "conversation", 2)
	if !strings.Contains(history[len(history)-1].Text, "Interrupted the master turn") {
		t.Fatal("contextual fast-lane reply was not persisted")
	}
}

func TestClassroomMessageContextFollowsQueuedTurn(t *testing.T) {
	type observed struct {
		prompt string
		app    *api.AppContext
	}
	seen := make(chan observed, 3)
	release := make(chan struct{})
	b, _ := assistantTestWiring(t, false, func(ctx context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		var text strings.Builder
		for _, m := range req.Messages {
			text.WriteString(m.Content)
		}
		seen <- observed{prompt: text.String(), app: api.AppContextFrom(ctx)}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return textReply("Response"), nil
	}, master.Config{Turns: master.TurnConfig{CancelWindow: time.Nanosecond}})
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
		deadline := time.Now().Add(5 * time.Second)
		for !b.Master.Turns().Active("one-history") || b.Master.Turns().PendingCount("one-history") != i {
			if time.Now().After(deadline) {
				t.Fatal("deferred message was not delivered to the turn")
			}
			time.Sleep(time.Millisecond)
		}
	}
	if b.Master.Turns().PendingCount("one-history") != 2 {
		t.Fatal("messages were not queued")
	}
	close(release)
	history := awaitAssistant(t, b, "one-history", 3)
	if len(history) != 6 {
		t.Fatal("conversation history split", history)
	}
	for i, app := range inputs {
		got := <-seen
		if app == nil {
			if got.app != nil || strings.Contains(got.prompt, "Current app view") {
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
	if normalized[0].Context == nil || normalized[0].Context.CourseID != "course-a" || normalized[1].Context == nil || normalized[1].Context.CourseID != "course-b" || normalized[2].Context != nil {
		t.Fatal("context missing from queued envelope")
	}
}

func TestClassroomMessageContextValidation(t *testing.T) {
	backend := NewDaemonBackend(nil, nil, nil)
	server := httptest.NewServer(gateway.New(gateway.Config{}, backend, gateway.NewMemoryBlobs(), nil))
	defer server.Close()
	for _, app := range []api.AppContext{{App: ""}, {App: "classroom", TopicID: strings.Repeat("t", 201)}} {
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
