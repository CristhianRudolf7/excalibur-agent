package daemon

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/orchestrator"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func personalBackend(t *testing.T, db *store.DB) (*DaemonBackend, *agents.Registry) {
	t.Helper()
	bus := telemetry.NewBus(nil)
	r, cs, err := OpenAgentConversations(context.Background(), db, "", bus)
	if err != nil {
		t.Fatal(err)
	}
	b := NewDaemonBackend(master.New(master.Config{}, cs, nil, nil, nil, nil, nil), nil, bus)
	b.AttachServices(BackendServices{Agents: r})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = b.Master.Turns().Stop(ctx)
	})
	return b, r
}
func TestPersonalAgentPersonaAndHistoryIsolation(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	first := r.Favorite()
	second, err := r.Create(ctx, agents.CreateParams{Name: "Ada", Title: "Mathematician", Description: "Algebra guide", Personality: "Use exact fractions"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = b.Master.SendAssistant(ctx, first.ConversationID, "Private first history", nil)
	_, _ = b.Master.SendAssistant(ctx, second.ConversationID, "Private Ada history", nil)
	a := assistantTurns{backend: b, services: &Services{}}
	x := a.snapshot(ctx, first.ConversationID, master.Normalized{Text: "Hello"})
	y := a.snapshot(ctx, second.ConversationID, master.Normalized{Text: "Hello"})
	if strings.Contains(x.AgentProfile, "Ada") || !strings.Contains(y.AgentProfile, "Use exact fractions") || !strings.Contains(y.AgentProfile, "Mathematician") || strings.Contains(y.AgentProfile, "Agent name: Excalibur") {
		t.Fatalf("personas: %s / %s", x.AgentProfile, y.AgentProfile)
	}
	if len(x.Activity) != 1 || x.Activity[0].Content != "Private first history" || len(y.Activity) != 1 || y.Activity[0].Content != "Private Ada history" {
		t.Fatalf("mixed histories: %+v / %+v", x.Activity, y.Activity)
	}
	_, _ = b.Master.SendAssistant(ctx, second.ConversationID, "Another answer", nil)
	y2 := a.snapshot(ctx, second.ConversationID, master.Normalized{Text: "Different"})
	if y2.AgentProfile != y.AgentProfile {
		t.Fatal("activity changed stable persona prefix")
	}
	name := "New favorite"
	_, _ = r.Update(ctx, agents.UpdateParams{ID: first.ID, Name: &name})
	if y3 := a.snapshot(ctx, second.ConversationID, master.Normalized{}); y3.AgentProfile != y.AgentProfile {
		t.Fatal("another agent changed persona prefix")
	}
}
func TestPersonalAgentHistoryUnreadSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "chats.db")
	db, err := store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	b, r := personalBackend(t, db)
	a := r.Favorite()
	event, err := b.Master.SendAssistant(ctx, a.ConversationID, "A persisted answer", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Favorite().Unread != 1 {
		t.Fatal("assistant unread")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(ctx, path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	b2, r2 := personalBackend(t, db)
	history, err := b2.ConversationHistory(ctx, a.ConversationID, "", 10)
	if err != nil || len(history) != 1 || history[0].ID != event.MessageID || history[0].Role != "assistant" || history[0].Text != "A persisted answer" {
		t.Fatal(history, err)
	}
	if r2.Favorite().Unread != 1 {
		t.Fatal("lost unread")
	}
	if err = r2.MarkRead(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if r2.Favorite().Unread != 0 {
		t.Fatal("mark read")
	}
}
func TestPersonalSystemOutputUsesFavoriteAndOrigin(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	first := r.Favorite()
	a, _ := r.Create(ctx, agents.CreateParams{Name: "Ada"})
	_ = r.SetFavorite(ctx, a.ID)
	if b.SystemConversation("") != a.ConversationID || b.SystemConversation(first.ConversationID) != first.ConversationID {
		t.Fatal("system target")
	}
	n := AutomationNotifier{Master: b.Master, Backend: b}
	if err := n.Notify(ctx, "Automation result"); err != nil {
		t.Fatal(err)
	}
	orch := orchestrator.New(orchestrator.DefaultConfig(), orchestrator.NewMemoryStore(), orchestrator.NoopJournal{})
	launcher, err := NewOrchestratorLauncher(orch, nil)
	if err != nil {
		t.Fatal(err)
	}
	launcher.BindChat(b)
	if err = launcher.LaunchReminder(ctx, agenda.Reminder{ID: "reminder", Text: "Remember this"}); err != nil {
		t.Fatal(err)
	}
	if len(b.Master.History(first.ConversationID, 0)) != 0 || len(b.Master.History(a.ConversationID, 0)) != 2 {
		t.Fatal("system output used legacy default")
	}
	ev := types.Event{ID: "class-favorite", Type: "classroom.class_requested", Payload: []byte(`{"course_id":"course","topic_id":"topic"}`)}
	if err = b.handleClassroomEvent(ctx, ev, time.Now()); err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, a.ConversationID, 3)
	if len(b.Master.History(first.ConversationID, 0)) != 0 {
		t.Fatal("classroom used legacy default")
	}
	ev.ID = "class-origin"
	ev.Payload = []byte(`{"course_id":"course","topic_id":"other","conversation_id":"` + first.ConversationID + `"}`)
	if err = b.handleClassroomEvent(ctx, ev, time.Now()); err != nil {
		t.Fatal(err)
	}
	awaitAssistant(t, b, first.ConversationID, 1)
}
func TestPersonalAgentArchivedConversationRejected(t *testing.T) {
	ctx := context.Background()
	b, r := personalBackend(t, nil)
	a, _ := r.Create(ctx, agents.CreateParams{Name: "Archived"})
	_, _ = r.Archive(ctx, a.ID)
	if _, _, err := b.ConversationSend(ctx, "client", "1", a.ConversationID, "Hello"); err == nil {
		t.Fatal("archived send")
	}
	if _, _, err := b.ConversationSendContext(ctx, "client", "2", a.ConversationID, "Hello", nil, &api.AppContext{App: "classroom"}); err == nil {
		t.Fatal("archived contextual send")
	}
	if _, _, err := b.ConversationSend(ctx, "client", "3", "cnv_agent_missing", "Hello"); err == nil {
		t.Fatal("unknown agent conversation")
	}
}

func TestPersonalMessageReceiptAndUnreadCommitTogether(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "atomic.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r, cs, err := OpenAgentConversations(ctx, db, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := cs.(*agentConversationStore)
	a := r.Favorite()
	msg := master.Normalized{Role: "user", Text: "A relay", RelayedBy: "sender", CreatedAt: time.Now().UTC()}
	if _, err = s.SaveClientMessage(ctx, a.ConversationID, msg, "relay:sender", "receipt"); err != nil {
		t.Fatal(err)
	}
	// A receipt constraint failure rolls back both the new message and unread.
	if _, err = s.SaveClientMessage(ctx, a.ConversationID, msg, "relay:sender", "receipt"); err == nil {
		t.Fatal("duplicate receipt committed")
	}
	if got := r.Favorite(); got.Unread != 1 {
		t.Fatal(got)
	}
	if rows := s.History(a.ConversationID, 0); len(rows) != 1 {
		t.Fatal(rows)
	}
	reopened, _, err := OpenAgentConversations(ctx, db, "", nil)
	if err != nil || reopened.Favorite().Unread != 1 {
		t.Fatal(reopened, err)
	}
}

func TestPersonalConversationSendSelectsPersonaAndSharedMemory(t *testing.T) {
	t.Setenv("EXCALIBUR_MASTER_ENGINE_TURNS", "1")
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "personas.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = memory.EnsureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	mem := memory.NewService(db, memory.WithScopes("general"))
	userDoc, err := mem.ResolveDoc(ctx, "general", memory.DocUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = mem.AppendFragment(ctx, nil, userDoc, "preferences", "Shared user preference: concise answers.", ""); err != nil {
		t.Fatal(err)
	}
	b, r := personalBackend(t, db)
	first := r.Favorite()
	other, err := r.Create(ctx, agents.CreateParams{Name: "Ada", Personality: "Exact fractions only"})
	if err != nil {
		t.Fatal(err)
	}
	model := models.NewGateway(models.GatewayConfig{MaxAttempts: 1, DisableBudgetRetries: true, Roles: map[types.ModelRole]models.RoleEntry{types.RoleInterlocutor: {Primary: models.ModelRef{Provider: "personas", Model: "test"}}}})
	defer model.Close()
	requests := make(chan string, 2)
	model.Register(&turnProvider{FakeProvider: fake.New("personas"), chat: func(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
		var prompt strings.Builder
		for _, msg := range req.Messages {
			prompt.WriteString(msg.Content)
		}
		requests <- prompt.String()
		return textReply("Done"), nil
	}})
	s, err := Assemble(AssembleOptions{Models: model, Memory: mem})
	if err != nil {
		t.Fatal(err)
	}
	WireAssistantTurns(b, s, model, "test")
	defer s.delegations.stop()
	for i, agent := range []agents.PersonalAgent{first, other} {
		marker := []string{"Private Excalibur question", "Private Ada question"}[i]
		if _, _, err = b.ConversationSend(ctx, "client", marker, agent.ConversationID, marker); err != nil {
			t.Fatal(err)
		}
		awaitAssistant(t, b, agent.ConversationID, 1)
		prompt := <-requests
		if !strings.Contains(prompt, "Shared user preference") || !strings.Contains(prompt, marker) || !strings.Contains(prompt, "Agent name: "+agent.Name) {
			t.Fatal("missing selected persona, history, or shared memory")
		}
		if i == 0 && strings.Contains(prompt, "Exact fractions only") || i == 1 && strings.Contains(prompt, "Private Excalibur question") {
			t.Fatal("another agent leaked into the prompt")
		}
	}
}

func TestPersonalConversationReadsPreservedLegacyMessages(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "legacy.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		for _, sql := range []string{
			`INSERT INTO principals(id,name) VALUES('legacy-owner','Legacy user')`,
			`INSERT INTO conversations(id,principal_id) VALUES('cnv_web_excalibur','legacy-owner')`,
			`INSERT INTO messages(id,conversation_id,role,text,sent_at_ms) VALUES('msg_legacy','cnv_web_excalibur','assistant','A preserved legacy answer',1234567890000)`,
		} {
			if _, e := ex.Exec(ctx, sql); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, r := personalBackend(t, db)
	rows, err := b.ConversationHistory(ctx, r.Favorite().ConversationID, "", 10)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	row := rows[0]
	if row.ID != "msg_legacy" || row.Role != "assistant" || row.Text != "A preserved legacy answer" || row.CreatedAt.UnixMilli() != 1234567890000 {
		t.Fatal(row)
	}
	if got := r.Favorite(); got.LastPreview != row.Text || got.Unread != 0 {
		t.Fatal(got)
	}
}

func TestPersonalMessagesKeepExistingUserIdentity(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "principal.db"), store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language) VALUES('existing-owner','Existing user','Europe/London','en')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	b, r := personalBackend(t, db)
	additional, err := r.Create(ctx, agents.CreateParams{Name: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []agents.PersonalAgent{r.Favorite(), additional} {
		if _, err = b.Master.SendAssistant(ctx, agent.ConversationID, "A new answer", nil); err != nil {
			t.Fatal(err)
		}
	}
	principal, err := NewPrincipalService(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := principal.Get(ctx)
	if err != nil || profile.ID != "existing-owner" || profile.Name != "Existing user" || profile.Timezone != "Europe/London" {
		t.Fatal(profile, err)
	}
	if err = db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		var count int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("chat persistence replaced the existing user: %d principals", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
