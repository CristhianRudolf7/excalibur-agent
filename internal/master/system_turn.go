package master

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ReceiveSystem appends a system-originated instruction and queues a normal
// conversational turn. It bypasses user-command triage, and deduplicates the
// event identity using the same receipt store as channel messages.
func (m *Master) ReceiveSystem(ctx context.Context, eventID, conv, text string) (types.MessageID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !presentSystem(eventID, conv, text) {
		return "", fmt.Errorf("master: system turn needs event, conversation and instruction")
	}
	m.mu.Lock()
	if id, ok := m.store.LookupClientMsg("system", eventID); ok {
		m.mu.Unlock()
		return id, nil
	}
	msg := Normalized{Role: "system", Text: text, Channel: "system"}
	id, err := m.store.SaveMessage(ctx, conv, msg)
	if err == nil {
		m.store.RecordClientMsg("system", eventID, id)
	}
	m.mu.Unlock()
	if err != nil {
		return "", err
	}
	m.turns.Deliver(conv, IntakeResult{Msg: msg, MsgID: id, SentAt: time.Now()})
	return id, nil
}
func presentSystem(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return false
		}
	}
	return true
}
