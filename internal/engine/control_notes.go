package engine

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
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
		kept = append(kept, controlClip(note, controlTailBytes()))
		keys = append(keys, key)
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

func boundControlLayer(c *ctxcompiler.Compiled, counter ctxcompiler.Counter) {
	if !switchOn("EXCALIBUR_CONTROL_TAIL") {
		return
	}
	i := ctxcompiler.LayerVolatile
	c.TotalTokens -= c.LayerTokens[i]
	c.Layers[i] = controlClip(c.Layers[i], controlTailBytes())
	c.Hashes[i] = ctxcompiler.HashLayer(c.Layers[i])
	c.LayerTokens[i] = counter.Count(c.Layers[i])
	c.TotalTokens += c.LayerTokens[i]
}

// User instructions are not bounded control guidance. Keep their complete
// literal text as append-only activity, where the control cap cannot drop it.
func (r *Run) appendDirectiveLocked(text string, activity []ctxcompiler.ActivityEvent) {
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
	if !switchOn("EXCALIBUR_CONTROL_TAIL") {
		return
	}
	raw, _ := json.Marshal(snap.Volatile.Directives)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.directiveSnapshot != string(raw) && len(snap.Volatile.Directives) > 0 {
		r.appendDirectiveLocked("pending instructions:\n"+strings.Join(snap.Volatile.Directives, "\n"), snap.Activity)
	}
	r.directiveSnapshot = string(raw)
	snap.Volatile.Directives = nil
}
