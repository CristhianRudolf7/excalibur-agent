package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// The daemon's memory service has no default scope, so chat calls to the
// recall tools that omit `scopes` (or use a free-form `scope`) fail with an
// input error and the assistant answers that it cannot remember. The chat
// path is personal and single-principal: default those calls to the general
// scope. Eval and delegate profiles do not use this runner.
var chatRecallTools = map[string]bool{"memory_search": true, "history_search": true}

const chatRecallNote = "These items come from earlier conversations. Use them to recall what was discussed; " +
	"if the earlier answer came from a library document, search that document again with library_search " +
	"and cite it instead of repeating the old answer from memory."

func normalizeChatRecallArgs(name string, args json.RawMessage) json.RawMessage {
	if !chatRecallTools[name] {
		return args
	}
	fields := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(args))) > 0 {
		if err := json.Unmarshal(args, &fields); err != nil || fields == nil {
			return args
		}
	}
	var scopes []string
	if raw, ok := fields["scopes"]; ok {
		_ = json.Unmarshal(raw, &scopes)
	}
	if raw, ok := fields["scope"]; ok {
		var one string
		if json.Unmarshal(raw, &one) == nil {
			one = strings.TrimSpace(one)
			if one != "" && !strings.EqualFold(one, "all") && !strings.EqualFold(one, "any") {
				scopes = append(scopes, one)
			}
		}
		delete(fields, "scope")
	}
	clean := scopes[:0]
	for _, s := range scopes {
		if s = strings.TrimSpace(s); s != "" && !strings.EqualFold(s, "all") && !strings.EqualFold(s, "any") {
			clean = append(clean, s)
		}
	}
	if len(clean) == 0 {
		clean = []string{"general"}
	}
	fields["scopes"], _ = json.Marshal(clean)
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
}

func annotateChatRecallResult(name string, res tools.Result) tools.Result {
	if name != "history_search" || res.Status != tools.StatusOK {
		return res
	}
	var out map[string]json.RawMessage
	if json.Unmarshal([]byte(res.Output), &out) != nil || out == nil {
		return res
	}
	var items []json.RawMessage
	if json.Unmarshal(out["items"], &items) != nil || len(items) == 0 {
		return res
	}
	out["note"], _ = json.Marshal(chatRecallNote)
	if b, err := json.Marshal(out); err == nil {
		res.Output = string(b)
	}
	return res
}

// chatRecallHistoryLimit bounds the earlier-conversation turns attached to a
// chat memory_search result and the text kept from each of them.
const (
	chatRecallHistoryLimit = 8
	chatRecallTextLimit    = 700
)

// withEarlierConversations widens a chat memory_search with history_search
// over the same query. Facts are only written to memory selectively, so a
// question like "do you remember what we talked about some days ago?" must
// also look at past conversation turns, which are always indexed.
func (r *assistantToolRunner) withEarlierConversations(ctx context.Context, name string, args json.RawMessage, res tools.Result, opts tools.ExecuteOptions) tools.Result {
	if name != "memory_search" || res.Status != tools.StatusOK || !r.allowed["history_search"] {
		return res
	}
	var in struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(args, &in) != nil || strings.TrimSpace(in.Query) == "" {
		return res
	}
	hargs, _ := json.Marshal(map[string]any{"text": in.Query, "scopes": []string{"general"}, "k": chatRecallHistoryLimit})
	hist, err := r.daemonToolRunner.Execute(ctx, "history_search", hargs, opts)
	if err != nil || hist.Status != tools.StatusOK {
		return res
	}
	var found struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal([]byte(hist.Output), &found) != nil || len(found.Items) == 0 {
		return res
	}
	for _, item := range found.Items {
		if text, ok := item["text"].(string); ok && utf8.RuneCountInString(text) > chatRecallTextLimit {
			item["text"] = string([]rune(text)[:chatRecallTextLimit]) + "…"
		}
	}
	var out map[string]any
	if json.Unmarshal([]byte(res.Output), &out) != nil || out == nil {
		out = map[string]any{"memory_result": res.Output}
	}
	out["earlier_conversations"] = found.Items
	out["earlier_conversations_note"] = chatRecallNote
	if b, err := json.Marshal(out); err == nil {
		res.Output = string(b)
	}
	return res
}
