package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// robotLabRelayMaster permits continuation tests without a model or network.
type robotLabRelayMaster interface {
	ReceiveRelay(context.Context, string, string, string, string) (types.MessageID, bool, error)
	TurnHistory(string, int) []master.Normalized
}

type robotLabContinuations struct {
	ctx         context.Context
	cancel      context.CancelFunc
	service     *robotlab.Service
	master      robotLabRelayMaster
	active      func(string) bool
	mu          sync.Mutex
	stopped     bool
	pending     map[string]*robotLabContinuation
	sent        map[string]int
	sending     map[string]bool
	wg          sync.WaitGroup
	unsubscribe func()
}

type robotLabContinuation struct {
	session string
	started time.Time
	ctx     context.Context
	cancel  context.CancelFunc
}

func newRobotLabContinuations(service *robotlab.Service, m robotLabRelayMaster, active func(string) bool) *robotLabContinuations {
	ctx, cancel := context.WithCancel(context.Background())
	c := &robotLabContinuations{ctx: ctx, cancel: cancel, service: service, master: m, active: active, pending: map[string]*robotLabContinuation{}, sent: map[string]int{}, sending: map[string]bool{}}
	if service != nil && service.Sidecar != nil {
		c.unsubscribe = service.Sidecar.Subscribe(c.sessionEvent)
	}
	return c
}

func (c *robotLabContinuations) sessionEvent(event robotlab.Event) {
	if event.Name != "session" {
		return
	}
	var result struct {
		Session robotlab.SessionState `json:"session"`
	}
	if json.Unmarshal(event.Data, &result) != nil || result.Session.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for conversation, pending := range c.pending {
		if (result.Session.ID == pending.session && result.Session.Status != "live") ||
			(result.Session.ID != pending.session && result.Session.Status == "live") {
			pending.cancel()
			delete(c.pending, conversation)
		}
	}
}
func (c *robotLabContinuations) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	c.cancel()
	unsubscribe := c.unsubscribe
	c.unsubscribe = nil
	c.mu.Unlock()
	if unsubscribe != nil {
		unsubscribe()
	}
	c.wg.Wait()
}
func (c *robotLabContinuations) queue(turn context.Context, conversation string, called bool) {
	if c == nil || !called || c.service == nil || c.master == nil || turn.Done() == nil {
		return
	}
	started := c.service.TurnStartedAt(turn, conversation)
	if started.IsZero() {
		started = time.Now()
	}
	probe, done := context.WithTimeout(c.ctx, 5*time.Second)
	state, err := c.service.LiveSession(probe, conversation)
	done()
	if err != nil || state == nil {
		return
	}
	c.mu.Lock()
	if c.stopped || c.pending[conversation] != nil {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	pending := &robotLabContinuation{session: state.ID, started: started, ctx: ctx, cancel: cancel}
	c.pending[conversation] = pending
	c.wg.Add(1)
	c.mu.Unlock()
	safego.Go("robotlab-continuation", nil, func() {
		defer c.wg.Done()
		defer cancel()
		defer func() {
			c.mu.Lock()
			if c.pending[conversation] == pending {
				delete(c.pending, conversation)
			}
			c.mu.Unlock()
		}()
		// TurnManager cancels its context after replying and draining queued input.
		// Relaying inside CompleteTurn would trigger its short-turn restart rule.
		ready := false
		select {
		case <-pending.ctx.Done():
		case <-turn.Done():
			ready = true
		}
		if !ready || pending.ctx.Err() != nil {
			return
		}
		if c.active != nil && c.active(conversation) {
			return
		} // Canceled/restarted turn.
		ctx, cancel := context.WithTimeout(pending.ctx, 5*time.Second)
		defer cancel()
		c.send(ctx, conversation, pending.session, pending.started)
	})
}
func (c *robotLabContinuations) send(ctx context.Context, conversation, session string, started time.Time) {
	if ctx.Err() != nil || !robotlab.Enabled() {
		return
	}
	state, err := c.service.LiveSession(ctx, conversation)
	if err != nil || state == nil || state.MaxAttempts < 1 || state.ID != session {
		return
	}
	prefix := "Continue training session " + state.ID + ":"
	c.mu.Lock()
	if c.stopped || ctx.Err() != nil || c.sending[state.ID] {
		c.mu.Unlock()
		return
	}
	c.sending[state.ID] = true
	count, known := c.sent[state.ID]
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.sending, state.ID)
		c.mu.Unlock()
	}()
	history := c.master.TurnHistory(conversation, 0)
	if robotLabNewUserMessage(history, started) {
		return
	}
	if !known {
		// History and receipt deduplication preserve the bound across daemon restarts.
		for _, msg := range history {
			if msg.RelayedBy == "robot-lab" && strings.HasPrefix(msg.Text, prefix) {
				count++
			}
		}
	}
	if count >= 3*state.MaxAttempts {
		return
	}
	if c.active != nil && c.active(conversation) {
		return
	}
	// Recheck after reading history: a stop or session switch can happen while
	// those reads are in flight. The relay stays bound to the queued session.
	current, err := c.service.LiveSession(ctx, conversation)
	if err != nil || current == nil || current.ID != state.ID || ctx.Err() != nil {
		return
	}
	if robotLabNewUserMessage(c.master.TurnHistory(conversation, 0), started) || ctx.Err() != nil {
		return
	}
	if c.active != nil && c.active(conversation) {
		return
	}
	next := count + 1
	text := fmt.Sprintf("Continue training session %s: attempt %d of %d, status %s. Read the robot card and relevant markdown skills. Plan small keyframes, check with ik, then prefer sequence calls with 4–12 keyframes. Read state after every call (dog: support, margin and terrain). After each failed or abandoned attempt, reflect and write or improve a markdown skill with the lesson and exact keyframes that worked so far (failures=[N]), then attempt_reset; give no_skill_reason only when no useful lesson can be saved. Keep going until the goal is met or the attempt limit is reached. Never claim success unless goal.met=true or the session succeeded.", state.ID, state.Attempt, state.MaxAttempts, state.Status)
	_, _, err = c.master.ReceiveRelay(ctx, fmt.Sprintf("robotlab:%s:%d", state.ID, next), conversation, text, "robot-lab")
	if err == nil {
		c.mu.Lock()
		c.sent[state.ID] = next
		c.mu.Unlock()
	}
}

func robotLabNewUserMessage(history []master.Normalized, started time.Time) bool {
	if started.IsZero() {
		return false
	}
	for _, msg := range history {
		if msg.RelayedBy == "" && (msg.Role == "" || msg.Role == "user") && msg.CreatedAt.After(started) {
			return true
		}
	}
	return false
}

func robotLabTurnLimits(ctx context.Context, service *robotlab.Service, conversation string, defs []tools.Definition, steps int, timeout time.Duration) (int, time.Duration) {
	service.RecordTurnStart(ctx, conversation)
	if !robotlab.Enabled() || os.Getenv("EXCALIBUR_MASTER_STEPS") != "" {
		return steps, timeout
	}
	active := false
	for _, def := range defs {
		if def.Name == "robot_lab" {
			active = true
			break
		}
	}
	if !active && service != nil {
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		state, err := service.LiveSession(probe, conversation)
		cancel()
		active = err == nil && state != nil
	}
	if active {
		return 120, max(timeout, 30*time.Minute)
	}
	return steps, timeout
}
