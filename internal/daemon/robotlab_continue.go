package daemon

import (
	"context"
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
	ctx     context.Context
	cancel  context.CancelFunc
	service *robotlab.Service
	master  robotLabRelayMaster
	active  func(string) bool
	mu      sync.Mutex
	stopped bool
	pending map[string]bool
	sent    map[string]int
	wg      sync.WaitGroup
}

func newRobotLabContinuations(service *robotlab.Service, m robotLabRelayMaster, active func(string) bool) *robotLabContinuations {
	ctx, cancel := context.WithCancel(context.Background())
	return &robotLabContinuations{ctx: ctx, cancel: cancel, service: service, master: m, active: active, pending: map[string]bool{}, sent: map[string]int{}}
}
func (c *robotLabContinuations) stop() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.stopped = true
	c.cancel()
	c.mu.Unlock()
	c.wg.Wait()
}
func (c *robotLabContinuations) queue(turn context.Context, conversation string, called bool) {
	if c == nil || !called || c.service == nil || c.master == nil || turn.Done() == nil {
		return
	}
	c.mu.Lock()
	if c.stopped || c.pending[conversation] {
		c.mu.Unlock()
		return
	}
	c.pending[conversation] = true
	c.wg.Add(1)
	c.mu.Unlock()
	safego.Go("robotlab-continuation", nil, func() {
		defer c.wg.Done()
		// TurnManager cancels its context after replying and draining queued input.
		// Relaying inside CompleteTurn would trigger its short-turn restart rule.
		ready := false
		select {
		case <-c.ctx.Done():
		case <-turn.Done():
			ready = true
		}
		c.mu.Lock()
		delete(c.pending, conversation)
		c.mu.Unlock()
		if !ready {
			return
		}
		if c.active != nil && c.active(conversation) {
			return
		} // Canceled/restarted turn.
		ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		defer cancel()
		c.send(ctx, conversation)
	})
}
func (c *robotLabContinuations) send(ctx context.Context, conversation string) {
	if ctx.Err() != nil || !robotlab.Enabled() {
		return
	}
	state, err := c.service.LiveSession(ctx, conversation)
	if err != nil || state == nil || state.MaxAttempts < 1 || state.MaxAttempts > 100 {
		return
	}
	prefix := "Continue training session " + state.ID + ":"
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || ctx.Err() != nil {
		return
	}
	count, known := c.sent[state.ID]
	if !known {
		// History and receipt deduplication preserve the bound across daemon restarts.
		for _, msg := range c.master.TurnHistory(conversation, 0) {
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
	next := count + 1
	text := fmt.Sprintf("Continue training session %s: attempt %d of %d, status %s. Keep going until the goal is met or the attempt limit is reached. Remember: write or improve a skill after each failed attempt.", state.ID, state.Attempt, state.MaxAttempts, state.Status)
	_, _, err = c.master.ReceiveRelay(ctx, fmt.Sprintf("robotlab:%s:%d", state.ID, next), conversation, text, "robot-lab")
	if err == nil {
		c.sent[state.ID] = next
	}
}

func robotLabTurnLimits(ctx context.Context, service *robotlab.Service, conversation string, defs []tools.Definition, steps int, timeout time.Duration) (int, time.Duration) {
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
		return 48, max(timeout, 15*time.Minute)
	}
	return steps, timeout
}
