package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
)

func controlTailBytes() int {
	if n, err := strconv.Atoi(os.Getenv("EXCALIBUR_CONTROL_TAIL_BYTES")); err == nil && n > 0 {
		return n
	}
	return 2048
}

func controlClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// InjectControlNote replaces one issue's guidance. An empty note retires it.
// The unabridged note is journaled before applying the model-visible cap.
func (r *Run) InjectControlNote(key, note string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.controlNoteLocked(key, note)
}

func (r *Run) controlNoteLocked(key, note string) {
	if r.noteJournal != nil {
		_, _ = r.noteJournal.Store.Append(context.Background(), journal.Entry{RunID: r.ID, Type: "control_note", Payload: journal.MarshalPayload(map[string]string{"key": key, "note": note})})
	}
	if !switchOn("EXCALIBUR_CONTROL_TAIL") {
		if note != "" {
			r.Volatile = append(r.Volatile, note)
		}
		return
	}
	if len(r.controlKeys) != len(r.Volatile) {
		r.controlKeys = make([]string, len(r.Volatile))
		for i, note := range r.Volatile {
			r.controlKeys[i] = controlNoteKey(note)
		}
	}
	kept := make([]string, 0, len(r.Volatile)+1)
	keys := make([]string, 0, len(r.Volatile)+1)
	for i, old := range r.Volatile {
		if r.controlKeys[i] != key {
			kept = append(kept, old)
			keys = append(keys, r.controlKeys[i])
		}
	}
	if note != "" {
		if switchOn("EXCALIBUR_CONTROL_ISSUE_PRIORITY") {
			kept = append(kept, note)
		} else {
			kept = append(kept, controlClip(note, controlTailBytes()))
		}
		keys = append(keys, key)
	}
	if switchOn("EXCALIBUR_CONTROL_ISSUE_PRIORITY") {
		entries := make([]controlEntry, 0, len(kept))
		for i, text := range kept {
			entries = append(entries, controlEntry{key: keys[i], text: text, priority: controlIssuePriority(keys[i])})
		}
		fitted, changes := fitControlEntries(entries, controlTailBytes())
		r.auditControlLossLocked("notes", changes)
		positions := map[string]int{}
		for i, key := range keys {
			positions[key] = i
		}
		sort.SliceStable(fitted, func(i, j int) bool { return positions[fitted[i].key] < positions[fitted[j].key] })
		r.Volatile = nil
		r.controlKeys = nil
		for _, entry := range fitted {
			r.Volatile = append(r.Volatile, entry.text)
			r.controlKeys = append(r.controlKeys, entry.key)
		}
		return
	}
	size := len(strings.Join(kept, "\n"))
	for size > controlTailBytes() && len(kept) > 1 {
		size -= len(kept[0]) + 1
		kept = kept[1:]
		keys = keys[1:]
	}
	r.Volatile = kept
	r.controlKeys = keys
}

// Existing producers carry a stable category before ':'; explicit callers
// can supply an issue key without depending on the prose.
func controlNoteKey(note string) string {
	if switchOn("EXCALIBUR_CONTROL_ISSUE_PRIORITY") && strings.HasPrefix(note, "long command detected (") {
		return "command.lifecycle"
	}
	if strings.HasPrefix(note, "finish refused") {
		return "finish.refusal"
	}
	if strings.HasPrefix(note, "finish checks passed") || strings.HasPrefix(note, "finish confirmation pending") {
		return "finish.confirmation"
	}
	if strings.HasPrefix(note, "milestone plan") {
		return "pacing"
	}
	if at := strings.Index(note, ":"); at >= 0 {
		return note[:at]
	}
	words := strings.Fields(note)
	if len(words) > 5 {
		words = words[:5]
	}
	return strings.Join(words, " ")
}

// controlEntry keeps issue identity separate from rendered prose. High-priority
// entries share reserved room so a long refusal cannot erase the budget state.
type controlEntry struct {
	key, text string
	priority  int
}
type controlLoss struct {
	Key    string `json:"issue_id"`
	Action string `json:"action"`
}

func controlIssuePriority(key string) int {
	if strings.HasPrefix(key, "finish.") || strings.HasPrefix(key, "gate") || key == "verification" || key == "refusal" {
		return 3
	}
	if strings.HasPrefix(key, "budget") || strings.HasPrefix(key, "time reserve") {
		return 4
	}
	return 1
}

func fitControlEntries(entries []controlEntry, limit int) ([]controlEntry, []controlLoss) {
	// Newer advice wins ties, while current refusal/budget issues outrank tips.
	order := make([]int, len(entries))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := entries[order[i]], entries[order[j]]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		return order[i] > order[j]
	})
	critical := 0
	for _, entry := range entries {
		if entry.priority >= 3 {
			critical++
		}
	}
	quota := limit
	if critical > 0 {
		quota = max(1, (limit-critical+1)/critical)
	}
	remaining := limit
	var kept []controlEntry
	var loss []controlLoss
	for _, index := range order {
		entry := entries[index]
		room := remaining
		if len(kept) > 0 {
			room--
		}
		if room <= 0 {
			loss = append(loss, controlLoss{entry.key, "evicted"})
			continue
		}
		if entry.priority >= 3 && critical > 1 {
			room = min(room, quota)
		}
		text := controlClip(entry.text, room)
		if text != entry.text {
			loss = append(loss, controlLoss{entry.key, "clipped"})
		}
		if len(kept) > 0 {
			remaining--
		}
		remaining -= len(text)
		entry.text = text
		kept = append(kept, entry)
	}
	return kept, loss
}

// Called under the run lock. Only issue identities are journaled here; the
// original control_note event already holds the unabridged producer message.
func (r *Run) auditControlLossLocked(layer string, loss []controlLoss) {
	if len(loss) == 0 || r.noteJournal == nil {
		return
	}
	_, _ = r.noteJournal.Store.Append(context.Background(), journal.Entry{RunID: r.ID, Type: "control_layer_loss", Payload: journal.MarshalPayload(struct {
		Layer  string        `json:"layer"`
		Issues []controlLoss `json:"issues"`
	}{layer, loss})})
}

func boundControlLayer(c *ctxcompiler.Compiled, counter ctxcompiler.Counter, run *Run, volatile ctxcompiler.Volatile) {
	if !switchOn("EXCALIBUR_CONTROL_TAIL") {
		return
	}
	i := ctxcompiler.LayerVolatile
	c.TotalTokens -= c.LayerTokens[i]
	if switchOn("EXCALIBUR_CONTROL_ISSUE_PRIORITY") {
		now := volatile.Now.UTC()
		if now.IsZero() {
			now = time.Unix(0, 0).UTC()
		}
		entries := []controlEntry{{"clock", "now: " + now.Format(time.RFC3339), 1}}
		if volatile.ContinuationNote != "" {
			entries = append(entries, controlEntry{"continuation", "continuation: " + volatile.ContinuationNote, 1})
		}
		if volatile.BudgetState != "" {
			entries = append(entries, controlEntry{"budget", "budget: " + volatile.BudgetState, 4})
		}
		run.mu.Lock()
		identities := map[string]string{}
		for n, note := range run.Volatile {
			if n < len(run.controlKeys) {
				identities[note] = run.controlKeys[n]
			}
		}
		for n, note := range volatile.GateReports {
			key := identities[note]
			if key == "" {
				key = fmt.Sprintf("gate.%d", n)
			}
			entries = append(entries, controlEntry{key, "gate: " + note, controlIssuePriority(key)})
		}
		for _, group := range []struct {
			key    string
			values []string
		}{{"directives", volatile.Directives}, {"recovery", volatile.RecoveryNotices}, {"master jobs", volatile.MasterJobs}} {
			for n, text := range group.values {
				entries = append(entries, controlEntry{fmt.Sprintf("%s.%d", group.key, n), group.key + ": " + text, 1})
			}
		}
		fitted, loss := fitControlEntries(entries, controlTailBytes())
		run.auditControlLossLocked("volatile", loss)
		run.mu.Unlock()
		var texts []string
		for _, entry := range fitted {
			texts = append(texts, entry.text)
		}
		c.Layers[i] = strings.Join(texts, "\n")
	} else {
		c.Layers[i] = controlClip(c.Layers[i], controlTailBytes())
	}
	c.Hashes[i] = ctxcompiler.HashLayer(c.Layers[i])
	c.LayerTokens[i] = counter.Count(c.Layers[i])
	c.TotalTokens += c.LayerTokens[i]
}

// User instructions are not bounded control guidance. Keep their complete
// literal text as append-only activity, where the control cap cannot drop it.
func (r *Run) appendDirectiveLocked(text string, activity []ctxcompiler.ActivityEvent) {
	r.rememberDirectiveLocked(text)
	seq := r.nextHistorySeqLocked()
	for _, ev := range activity {
		if ev.Seq >= seq {
			seq = ev.Seq + 1
		}
	}
	r.contextRevision++
	snap := ctxcompiler.Snapshot{}
	ctxcompiler.AppendDelta(&snap, seq, r.contextRevision, text)
	r.History = append(r.History, snap.Activity[0])
}

func promoteSnapshotDirectives(r *Run, snap *ctxcompiler.Snapshot) {
	raw, _ := json.Marshal(snap.Volatile.Directives)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.directiveSnapshot != string(raw) && len(snap.Volatile.Directives) > 0 {
		text := "pending instructions:\n" + strings.Join(snap.Volatile.Directives, "\n")
		if switchOn("EXCALIBUR_CONTROL_TAIL") {
			r.appendDirectiveLocked(text, snap.Activity)
		} else {
			r.rememberDirectiveLocked(text)
		}
	}
	r.directiveSnapshot = string(raw)
	if switchOn("EXCALIBUR_CONTROL_TAIL") {
		snap.Volatile.Directives = nil
	}
}

func (r *Run) rememberDirectiveLocked(text string) {
	if !switchOn("EXCALIBUR_CONTROL_TAIL") && !switchOn("EXCALIBUR_VERDICT_REUSE") {
		return
	}
	for _, old := range r.literalDirectives {
		if old == text {
			return
		}
	}
	r.literalDirectives = append(r.literalDirectives, text)
}

func (r *Run) literalDirectivesCopy() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.literalDirectives...)
}

func (r *Run) verificationRequestLocked() string {
	if len(r.literalDirectives) == 0 {
		return r.Request
	}
	return r.Request + "\nAdditional user instructions (later updates take precedence):\n" + strings.Join(r.literalDirectives, "\n")
}
