package master

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// ReceiveRelay delivers a user-rendered message directly to the target's normal
// conversation turn. A receipt deduplicates tool retries, including after boot.
// RelayedBy travels with the turn so its tools can prohibit further relays.
func (m *Master) ReceiveRelay(ctx context.Context, receipt, conv, text, by string) (types.MessageID, bool, error) {
	if strings.TrimSpace(receipt) == "" || strings.TrimSpace(conv) == "" || strings.TrimSpace(text) == "" || by == "" {
		return "", false, fmt.Errorf("master: relay needs receipt, conversation, message and sender")
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	msg := Normalized{Role: "user", Text: text, RelayedBy: by, Channel: "api", Language: "en", CreatedAt: time.Now().UTC()}
	id, duplicate, err := m.saveClientMessage(ctx, conv, msg, "relay:"+by, receipt)
	if err != nil || duplicate {
		return id, duplicate, err
	}
	m.turns.Deliver(conv, IntakeResult{Msg: msg, MsgID: id, SentAt: msg.CreatedAt})
	return id, false, nil
}

// saveClientMessage commits the message and its receipt together when supported.
// The master lock also makes retries atomic for embedded conversation stores.
func (m *Master) saveClientMessage(ctx context.Context, conv string, msg Normalized, client, receipt string) (types.MessageID, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, ok := m.store.LookupClientMsg(client, receipt); ok {
		return id, true, nil
	}
	var id types.MessageID
	var err error
	if atomic, ok := m.store.(interface {
		SaveClientMessage(context.Context, string, Normalized, string, string) (types.MessageID, error)
	}); ok {
		id, err = atomic.SaveClientMessage(ctx, conv, msg, client, receipt)
	} else {
		id, err = m.store.SaveMessage(ctx, conv, msg)
		if err == nil {
			m.store.RecordClientMsg(client, receipt, id)
		}
	}
	return id, false, err
}
