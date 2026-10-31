package memory

import (
	"context"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// HistoryQuery narrows episode search (Section 10.1, 10.7): messages
// and recorded actions with their results.
type HistoryQuery struct {
	Text string `json:"text,omitempty"`
	// Conversation restricts to one conversation; empty searches
	// every conversation in the scope set.
	Conversation string `json:"conversation,omitempty"`
	From         int64  `json:"from_ms,omitempty"`
	To           int64  `json:"to_ms,omitempty"`
	// Tool restricts actions to payloads naming this tool.
	Tool string `json:"tool,omitempty"`
	// State restricts actions to this journal status.
	State string `json:"state,omitempty"`
	// Run restricts actions to one run.
	Run string `json:"run,omitempty"`
	K   int    `json:"k,omitempty"`
}

// HistoryItem is one episode hit: a conversation turn or a recorded
// action with its result.
type HistoryItem struct {
	ID           string `json:"id"`
	Conversation string `json:"conversation_id,omitempty"`
	RunID        string `json:"run_id,omitempty"`
	Kind         string `json:"kind"`
	Text         string `json:"text"`
	At           int64  `json:"at_ms"`
	Tool         string `json:"tool,omitempty"`
	State        string `json:"state,omitempty"`
}

// History searches past messages and actions (Section 10.7,
// history_search). Conversation turns index with their text plus
// every fact extracted from them as an additional key pointing at
// the original turn: a memory hit pulls its source turns in.
func (s *Service) History(ctx context.Context, scopes []string, hq HistoryQuery) ([]HistoryItem, error) {
	scopes, err := s.resolveScopes(scopes)
	if err != nil {
		return nil, err
	}
	k := hq.K
	if k <= 0 {
		k = 20
	}
	var items []HistoryItem
	seen := map[string]bool{}
	add := func(it HistoryItem) {
		if len(items) >= k || seen[it.Kind+"\x00"+it.ID] {
			return
		}
		seen[it.Kind+"\x00"+it.ID] = true
		items = append(items, it)
	}
	// Action filters (tool, state, run) narrow to recorded actions:
	// turns carry none of those attributes.
	actionOnly := hq.Tool != "" || hq.State != "" || hq.Run != ""
	if !actionOnly {
		// 1. Lexical turns.
		for _, t := range s.historyLexical(ctx, scopes, hq, k) {
			add(t)
		}
		// 2. Enriched keys: memories matching the query pull
		// their source turns.
		if hq.Text != "" {
			for _, t := range s.historyViaMemories(ctx, scopes, hq, k) {
				add(t)
			}
		}
	}
	// 3. Recorded actions.
	for _, a := range s.historyActions(ctx, scopes, hq, k) {
		add(a)
	}
	sortHistory(items)
	if len(items) > k {
		items = items[:k]
	}
	return items, nil
}

func (s *Service) historyLexical(ctx context.Context, scopes []string, hq HistoryQuery, k int) []HistoryItem {
	var items []HistoryItem
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := []any{}
		query := `SELECT m.id, m.conversation_id, m.role, m.text, m.sent_at_ms FROM messages m WHERE m.scope_id IN (` + placeholders(len(scopes)) + `)`
		args = append(args, scopeArgs(scopes)...)
		if hq.Text != "" {
			match := ftsOr(hq.Text, quotedPhrases(hq.Text))
			if match == "" {
				return nil
			}
			query = `SELECT m.id, m.conversation_id, m.role, m.text, m.sent_at_ms FROM messages_fts x JOIN messages m ON m.id = x.id
				WHERE messages_fts MATCH ? AND m.scope_id IN (` + placeholders(len(scopes)) + `)`
			args = append([]any{match}, args...)
		}
		if hq.Conversation != "" {
			query += ` AND m.conversation_id=?`
			args = append(args, hq.Conversation)
		}
		if hq.From > 0 {
			query += ` AND m.sent_at_ms>=?`
			args = append(args, hq.From)
		}
		if hq.To > 0 {
			query += ` AND m.sent_at_ms<?`
			args = append(args, hq.To)
		}
		if hq.Text != "" {
			query += ` ORDER BY bm25(messages_fts)`
		} else {
			query += ` ORDER BY m.sent_at_ms DESC`
		}
		query += ` LIMIT ?`
		args = append(args, k)
		rows, err := q.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it HistoryItem
			var role string
			if err := rows.Scan(&it.ID, &it.Conversation, &role, &it.Text, &it.At); err != nil {
				return err
			}
			it.Kind = "message:" + role
			items = append(items, it)
		}
		return rows.Err()
	})
	return items
}

func (s *Service) historyViaMemories(ctx context.Context, scopes []string, hq HistoryQuery, k int) []HistoryItem {
	ret, err := s.Retrieve(ctx, Query{Text: hq.Text, Scopes: scopes, K: k})
	if err != nil {
		return nil
	}
	want := map[string]bool{}
	for _, it := range ret.Items {
		for _, src := range it.Sources {
			if src.Kind == "message" && src.Ref != "" {
				want[src.Ref] = true
			}
		}
	}
	if len(want) == 0 {
		return nil
	}
	var items []HistoryItem
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		for _, fc := range chunkStrings(keysOf(want), 200) {
			args := make([]any, 0, len(fc))
			for _, id := range fc {
				args = append(args, id)
			}
			rows, err := q.Query(ctx, `SELECT id, conversation_id, role, text, sent_at_ms, scope_id FROM messages WHERE id IN (`+placeholders(len(fc))+`)`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var it HistoryItem
				var role, scopeID string
				if err := rows.Scan(&it.ID, &it.Conversation, &role, &it.Text, &it.At, &scopeID); err != nil {
					rows.Close()
					return err
				}
				if !inScopes(scopeID, scopes) {
					continue
				}
				if hq.Conversation != "" && it.Conversation != hq.Conversation {
					continue
				}
				it.Kind = "message:" + role
				items = append(items, it)
			}
			rows.Close()
		}
		return nil
	})
	return items
}

func (s *Service) historyActions(ctx context.Context, scopes []string, hq HistoryQuery, k int) []HistoryItem {
	var items []HistoryItem
	_ = s.read(ctx, func(ctx context.Context, q store.Queryer) error {
		args := scopeArgs(scopes)
		query := `SELECT j.run_id, j.seq, j.type, j.payload, j.status, j.created_at FROM journal j JOIN runs r ON r.id=j.run_id WHERE r.scope_id IN (` + placeholders(len(scopes)) + `)`
		if hq.Run != "" {
			query += ` AND j.run_id=?`
			args = append(args, hq.Run)
		}
		if hq.State != "" {
			query += ` AND j.status=?`
			args = append(args, hq.State)
		}
		if hq.Tool != "" {
			query += ` AND j.payload LIKE ?`
			args = append(args, "%"+hq.Tool+"%")
		}
		if hq.Text != "" {
			query += ` AND (j.payload LIKE ? OR j.type LIKE ?)`
			args = append(args, "%"+hq.Text+"%", "%"+hq.Text+"%")
		}
		if hq.From > 0 {
			query += ` AND j.created_at>=?`
			args = append(args, hq.From)
		}
		if hq.To > 0 {
			query += ` AND j.created_at<?`
			args = append(args, hq.To)
		}
		query += ` ORDER BY j.created_at DESC LIMIT ?`
		args = append(args, k)
		rows, err := q.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run, typ, payload, status string
			var seq, at int64
			if err := rows.Scan(&run, &seq, &typ, &payload, &status, &at); err != nil {
				return err
			}
			text := payload
			if len(text) > 2000 {
				text = text[:2000] + "…"
			}
			items = append(items, HistoryItem{
				ID: run + ":" + itoa(seq), RunID: run, Kind: "action:" + typ,
				Text: text, At: at, Tool: toolOf(payload), State: status,
			})
		}
		return rows.Err()
	})
	return items
}

// toolOf extracts a probable tool name from a journal payload for
// display; the Tool filter above does the precise matching.
func toolOf(payload string) string {
	for _, key := range []string{`"tool"`, `"name"`, `tool_call`} {
		if i := strings.Index(payload, key); i >= 0 {
			rest := payload[i+len(key):]
			if j := strings.Index(rest, `"`); j >= 0 {
				rest = rest[j+1:]
				if k := strings.Index(rest, `"`); k > 0 && k < 64 {
					return rest[:k]
				}
			}
		}
	}
	return ""
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func inScopes(scope string, scopes []string) bool {
	for _, sc := range scopes {
		if sc == scope {
			return true
		}
	}
	return false
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func sortHistory(items []HistoryItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].At > items[j-1].At; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
