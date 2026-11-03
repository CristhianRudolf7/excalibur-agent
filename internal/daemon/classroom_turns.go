package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ClassroomConversation is the existing local-user chat, shared by app views.
const ClassroomConversation = "cnv_web_excalibur"

// classroomTurnEvent consumes only identifiers from backend events. Private
// answer fields, lesson bodies and client-supplied prose never become instructions.
type classroomTurnEvent struct {
	CourseID  string `json:"course_id"`
	TopicID   string `json:"topic_id"`
	AttemptID string `json:"attempt_id"`
	Final     bool   `json:"final"`
}
type classroomTurns struct {
	once    sync.Once
	mu      sync.Mutex
	seen    map[string]bool
	order   []string
	pending map[string]time.Time
}

// StartClassroomTurns subscribes before returning and stops with the daemon.
// A periodic replay closes gaps if the bus's bounded live queue overflows.
func (b *DaemonBackend) StartClassroomTurns(ctx context.Context) {
	if b.Master == nil || b.Bus == nil {
		return
	}
	b.classroomTurns.once.Do(func() {
		live := b.Bus.Subscribe(128)
		// Do not regenerate lessons from requests left over before startup.
		cursor := types.Cursor{}
		for _, e := range b.Bus.Replay(cursor) {
			cursor.Seq = max(cursor.Seq, e.Seq)
		}
		safego.Go("classroom-agent-turns", nil, func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			drain := func() {
				for _, e := range b.Bus.Replay(cursor) {
					if ctx.Err() != nil {
						return
					}
					if err := b.handleClassroomEvent(ctx, e, time.Now()); err != nil {
						if ctx.Err() != nil {
							return
						}
						// Keep consuming ready events even when the generation queue is full.
						// The user can repeat the request after this visible failure.
						_, _ = b.Bus.Publish(types.EventNoticePosted, nil, nil, api.NoticeEvent{Content: "Classroom generation could not be queued. Reopen the topic or request the quiz again."})
					}
					cursor.Seq = e.Seq
				}
			}
			for {
				select {
				case <-ctx.Done():
					return
				case <-live:
					drain()
				case <-ticker.C:
					drain()
				}
			}
		})
	})
}

func classroomRequestKey(e classroomTurnEvent, kind string) string {
	switch kind {
	case "feedback":
		return fmt.Sprintf("feedback:%q", e.AttemptID)
	case "class":
		return fmt.Sprintf("class:%q:%q", e.CourseID, e.TopicID)
	default:
		return fmt.Sprintf("quiz:%q:%q:%t", e.CourseID, e.TopicID, e.Final)
	}
}
func (b *DaemonBackend) handleClassroomEvent(ctx context.Context, ev types.Event, now time.Time) error {
	kind := ""
	ready := false
	switch ev.Type {
	case "classroom.class_requested":
		kind = "class"
	case "classroom.quiz_requested":
		kind = "quiz"
	case "classroom.attempt_graded":
		kind = "feedback"
	case "classroom.class_ready":
		kind = "class"
		ready = true
	case "classroom.quiz_ready":
		kind = "quiz"
		ready = true
	case "classroom.feedback_ready":
		kind = "feedback"
		ready = true
	default:
		return nil
	}
	var e classroomTurnEvent
	if json.Unmarshal(ev.Payload, &e) != nil {
		return nil
	}
	if len(e.CourseID) > 200 || len(e.TopicID) > 200 || len(e.AttemptID) > 200 {
		return nil
	}
	if e.CourseID == "" && !(ready && kind == "feedback") {
		return nil
	}
	if kind == "class" && e.TopicID == "" || kind == "quiz" && e.Final == (e.TopicID != "") || kind == "feedback" && e.AttemptID == "" {
		return nil
	}
	c := &b.classroomTurns
	c.mu.Lock()
	defer c.mu.Unlock()
	key := classroomRequestKey(e, kind)
	if ev.ID == "" || len(ev.ID) > 256 || c.seen[ev.ID] {
		return nil
	}
	if c.pending == nil {
		c.pending = map[string]time.Time{}
		c.seen = map[string]bool{}
	}
	if ready {
		delete(c.pending, key)
		c.remember(ev.ID)
		return nil
	}
	for key, until := range c.pending {
		if !now.Before(until) {
			delete(c.pending, key)
		}
	}
	if _, ok := c.pending[key]; ok {
		c.remember(ev.ID)
		return nil
	}
	if len(c.pending) >= 128 {
		return fmt.Errorf("daemon: classroom turn queue is full")
	}
	instruction := ""
	switch kind {
	case "class":
		instruction = fmt.Sprintf("The user opened topic %q of course %q. Load course-creation, read classroom get_course, prepare the class and store its markdown with classroom write_class. Use whiteboard when it helps.", e.TopicID, e.CourseID)
	case "quiz":
		instruction = fmt.Sprintf("The user requested a classroom quiz for course %q, topic %q, final=%t. Load course-creation, read classroom get_course and create NEW closed questions with classroom create_quiz. Respect the final exam size rule; never reveal correct options in chat.", e.CourseID, e.TopicID, e.Final)
	case "feedback":
		instruction = fmt.Sprintf("Classroom attempt %q in course %q was graded. Load course-creation, read classroom get_course, and write targeted feedback with classroom write_feedback. Never reveal correct options, indexes or matching pairs.", e.AttemptID, e.CourseID)
	}
	ctx = api.WithAppContext(ctx, api.AppContext{App: "classroom", CourseID: e.CourseID, TopicID: e.TopicID})
	if _, err := b.Master.ReceiveSystem(ctx, "classroom:"+ev.ID, ClassroomConversation, instruction); err != nil {
		return err
	}
	c.pending[key] = now.Add(10 * time.Minute)
	c.remember(ev.ID)
	return nil
}

func (c *classroomTurns) remember(id string) {
	c.seen[id] = true
	c.order = append(c.order, id)
	if len(c.order) > 512 {
		delete(c.seen, c.order[0])
		c.order = c.order[1:]
	}
}
