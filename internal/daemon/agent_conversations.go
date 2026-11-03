package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/agents"
	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// OpenAgentConversations installs the registry and durable chat boundary before
// intake starts. Nil db supports embedded daemon tests with the same semantics.
func OpenAgentConversations(ctx context.Context, db *store.DB, name string, bus *telemetry.Bus) (*agents.Registry, master.ConversationStore, error) {
	var registryDB agents.RegistryDB
	if db != nil {
		registryDB = db
	}
	registry, err := agents.OpenRegistry(ctx, registryDB, name, func(kind string, payload any) {
		if bus != nil {
			_, _ = bus.Publish(types.EventType(kind), nil, nil, payload)
		}
	})
	if err != nil {
		return nil, nil, err
	}
	return registry, &agentConversationStore{db: db, agents: registry, mem: master.NewMemStore(), bus: bus}, nil
}

type agentConversationStore struct {
	db     *store.DB
	agents *agents.Registry
	mem    *master.MemStore
	bus    *telemetry.Bus
}

func (s *agentConversationStore) SaveMessage(ctx context.Context, conv string, msg master.Normalized) (types.MessageID, error) {
	return s.SaveClientMessage(ctx, conv, msg, "", "")
}
func (s *agentConversationStore) SaveClientMessage(ctx context.Context, conv string, msg master.Normalized, client, receipt string) (types.MessageID, error) {
	if msg.Role == "" {
		msg.Role = "user"
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.UserTimezone == "" {
		msg.UserTimezone = "UTC"
		if s.db != nil {
			if p, err := memory.NewPrincipalService(s.db).Get(ctx); err == nil {
				msg.UserTimezone = p.Timezone
			}
		}
	}
	var id types.MessageID
	if s.db == nil {
		var err error
		id, err = s.mem.SaveMessage(ctx, conv, msg)
		if err != nil {
			return "", err
		}
		if err = s.agents.ObserveMessage(ctx, conv, msg.Text, msg.CreatedAt, msg.Role == "assistant" || msg.RelayedBy != ""); err != nil {
			return "", err
		}
	} else {
		id = types.NewMessageID()
		raw, err := json.Marshal(msg)
		if err != nil {
			return "", err
		}
		attachments, err := json.Marshal(msg.Attachments)
		if err != nil {
			return "", err
		}
		err = s.agents.PersistMessage(ctx, conv, msg.Text, msg.CreatedAt, msg.Role == "assistant" || msg.RelayedBy != "", func(ctx context.Context, ex store.Execer) error {
			// The legacy schema requires a principal even for a local conversation.
			if _, err := ex.Exec(ctx, `INSERT INTO principals(id,name) SELECT 'local','User' WHERE NOT EXISTS (SELECT 1 FROM principals) ON CONFLICT(id) DO NOTHING`); err != nil {
				return err
			}
			if _, err := ex.Exec(ctx, `INSERT INTO conversations(id,principal_id,channel) VALUES(?,(SELECT id FROM principals ORDER BY (id='local') DESC,id LIMIT 1),?) ON CONFLICT(id) DO NOTHING`, conv, msg.Channel); err != nil {
				return err
			}
			if receipt != "" {
				if _, err := ex.Exec(ctx, `INSERT INTO client_messages(client_id,client_msg_id,msg_id) VALUES(?,?,?)`, client, receipt, string(id)); err != nil {
					return err
				}
			}
			_, err := ex.Exec(ctx, `INSERT INTO messages(id,conversation_id,role,text,attachments_json,language,sent_at_ms,normalized_json,user_tz) VALUES(?,?,?,?,?,?,?,?,?)`, string(id), conv, msg.Role, msg.Text, string(attachments), msg.Language, msg.CreatedAt.UnixMilli(), string(raw), msg.UserTimezone)
			return err
		})
		if err != nil {
			return "", err
		}
	}
	if receipt != "" && s.db == nil {
		s.mem.RecordClientMsg(client, receipt, id)
	}
	// Assistant output publishes through the citation boundary; only relayed
	// user messages need their own message.created event here.
	if msg.RelayedBy != "" && s.bus != nil {
		_, _ = s.bus.Publish(types.EventMessageCreated, nil, nil, api.MessageEvent{MessageID: id, ConversationID: types.ConversationID(conv), Role: msg.Role, Text: msg.Text, RelayedBy: msg.RelayedBy, CreatedAt: msg.CreatedAt})
	}
	return id, nil
}
func (s *agentConversationStore) history(conv string, limit int) ([]master.Normalized, []types.MessageID) {
	if s.db == nil {
		return s.mem.History(conv, limit), s.mem.MessageIDs(conv, limit)
	}
	out := []master.Normalized{}
	ids := []types.MessageID{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
		query := `SELECT id,role,text,attachments_json,language,sent_at_ms,normalized_json FROM messages WHERE conversation_id=? ORDER BY sent_at_ms DESC,id DESC`
		args := []any{conv}
		if limit > 0 {
			query += ` LIMIT ?`
			args = append(args, limit)
		}
		rows, err := q.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, role, text, attachments, language, raw string
			var ms int64
			if err = rows.Scan(&id, &role, &text, &attachments, &language, &ms, &raw); err != nil {
				return err
			}
			var msg master.Normalized
			if err = json.Unmarshal([]byte(raw), &msg); err != nil {
				return err
			}
			msg.Role = role
			msg.Text = text
			msg.Language = language
			if msg.CreatedAt.IsZero() && ms > 0 {
				msg.CreatedAt = time.UnixMilli(ms).UTC()
			}
			if msg.Attachments == nil {
				if err = json.Unmarshal([]byte(attachments), &msg.Attachments); err != nil {
					return err
				}
			}
			out = append(out, msg)
			ids = append(ids, types.MessageID(id))
		}
		return rows.Err()
	})
	if err != nil {
		slog.Error("could not read conversation history", "error", err)
		return nil, nil
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
		ids[i], ids[j] = ids[j], ids[i]
	}
	return out, ids
}

// HistoryWithIDs keeps search citations aligned with messages during concurrent writes.
func (s *agentConversationStore) HistoryWithIDs(conv string, limit int) ([]master.Normalized, []types.MessageID) {
	return s.history(conv, limit)
}

func (s *agentConversationStore) History(conv string, limit int) []master.Normalized {
	rows, _ := s.history(conv, limit)
	return rows
}
func (s *agentConversationStore) MessageIDs(conv string, limit int) []types.MessageID {
	_, ids := s.history(conv, limit)
	return ids
}
func (s *agentConversationStore) ActiveScope(conv string) string {
	if s.db == nil {
		return s.mem.ActiveScope(conv)
	}
	scope := "general"
	_ = s.db.ReadInteractive(context.Background(), func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT active_scope FROM conversations WHERE id=?`, conv).Scan(&scope)
	})
	return scope
}
func (s *agentConversationStore) SetActiveScope(conv, scope string) {
	if s.db == nil {
		s.mem.SetActiveScope(conv, scope)
		return
	}
	if err := s.db.Write(context.Background(), store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE conversations SET active_scope=? WHERE id=?`, scope, conv)
		return err
	}); err != nil {
		slog.Error("could not save conversation scope", "error", err)
	}
}
func (s *agentConversationStore) LookupClientMsg(client, id string) (types.MessageID, bool) {
	if s.db == nil {
		return s.mem.LookupClientMsg(client, id)
	}
	var msg string
	err := s.db.ReadInteractive(context.Background(), func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT msg_id FROM client_messages WHERE client_id=? AND client_msg_id=?`, client, id).Scan(&msg)
	})
	return types.MessageID(msg), err == nil
}
func (s *agentConversationStore) RecordClientMsg(client, id string, msg types.MessageID) {
	if s.db == nil {
		s.mem.RecordClientMsg(client, id, msg)
		return
	}
	if err := s.db.Write(context.Background(), store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO client_messages(client_id,client_msg_id,msg_id) VALUES(?,?,?) ON CONFLICT DO NOTHING`, client, id, string(msg))
		return err
	}); err != nil {
		slog.Error("could not save conversation receipt", "error", err)
	}
}

func (b *DaemonBackend) agentRegistry() *agents.Registry {
	if b.Services != nil {
		return b.Services.Agents
	}
	return nil
}
func (b *DaemonBackend) checkAgentConversation(conv string) error {
	if r := b.agentRegistry(); r != nil {
		a, err := r.ByConversation(conv)
		if err == nil && a.Archived {
			return &api.Error{Code: api.CodeConflict, Message: agents.ErrArchived.Error()}
		}
		if err != nil && (conv == agents.DefaultConversation || strings.HasPrefix(conv, "cnv_agent_")) {
			return &api.Error{Code: api.CodeUnknownTarget, Message: "unknown agent conversation"}
		}
	}
	return nil
}

// SystemConversation honors an active origin and otherwise resolves the current
// favorite at delivery time, including after an archive or favorite change.
func (b *DaemonBackend) SystemConversation(origin string) string {
	if r := b.agentRegistry(); r != nil {
		if a, err := r.ByConversation(origin); err == nil && !a.Archived {
			return a.ConversationID
		}
		return r.Favorite().ConversationID
	}
	if origin != "" {
		return origin
	}
	return agents.DefaultConversation
}
