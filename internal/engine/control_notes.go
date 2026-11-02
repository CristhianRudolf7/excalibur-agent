package engine

import (
	"context"
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
	if r.noteKeys == nil {
		r.noteKeys = map[string]string{}
	}
	kept := r.Volatile[:0]
	for _, old := range r.Volatile {
		oldKey := r.noteKeys[old]
		if oldKey == "" {
			oldKey = controlNoteKey(old)
		}
		if oldKey != key {
			kept = append(kept, old)
		}
	}
	r.Volatile = kept
	activeKeys := map[string]string{}
	for _, old := range kept {
		activeKeys[old] = r.noteKeys[old]
	}
	r.noteKeys = activeKeys
	if note != "" {
		clipped := controlClip(note, controlTailBytes())
		r.Volatile = append(r.Volatile, clipped)
		r.noteKeys[clipped] = key
	}
	size := len(strings.Join(r.Volatile, "\n"))
	for size > controlTailBytes() && len(r.Volatile) > 1 {
		size -= len(r.Volatile[0]) + 1
		r.Volatile = r.Volatile[1:]
	}
}

// Existing producers carry a stable category before ':'; explicit callers
// can supply an issue key without depending on the prose.
func controlNoteKey(note string) string {
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
