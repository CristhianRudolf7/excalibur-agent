package engine

import (
	"encoding/json"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// requestPrefix belongs to a run and builder, never to the shared builder.
// Compaction is the explicit Section 11.4 boundary for refreshing L3.
type requestPrefix struct {
	owner       *DefaultBuilder
	initial     [4]string
	current     [4]string
	revision    int
	compactions int
	tools       []providers.ToolDefinition
}

func (b *DefaultBuilder) stabilizePrefix(run *Run, snap *ctxcompiler.Snapshot, compiled *ctxcompiler.Compiled, defs *[]providers.ToolDefinition, cfg ctxcompiler.Config, counter ctxcompiler.Counter) error {
	if !switchOn("EXCALIBUR_STABLE_PREFIX") {
		return nil
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	p := &run.prefix
	var current [4]string
	copy(current[:], compiled.Layers[:4])
	if p.owner != b {
		// Clone schemas too: catalog owners may reuse their backing arrays.
		raw, _ := json.Marshal(*defs)
		*p = requestPrefix{owner: b, initial: current, current: current, compactions: run.Compactions}
		_ = json.Unmarshal(raw, &p.tools)
	} else {
		if p.compactions != run.Compactions {
			p.initial[3] = current[3]
			p.current[3] = current[3]
			p.compactions = run.Compactions
		}
		var updates []string
		for i := 1; i < 4; i++ {
			if current[i] != p.current[i] {
				updates = append(updates, "L"+string(rune('0'+i))+" changes:\n"+lineDelta(p.current[i], current[i]))
			}
		}
		if len(updates) > 0 {
			p.revision++
			seq := run.nextHistorySeqLocked()
			ctxcompiler.AppendDelta(snap, seq, p.revision, updates...)
			run.History = append(run.History, snap.Activity[len(snap.Activity)-1])
			var err error
			*compiled, err = ctxcompiler.Compile(cfg, *snap, counter)
			if err != nil {
				return err
			}
		}
		p.current = current
	}
	for i := 0; i < 4; i++ {
		compiled.TotalTokens -= compiled.LayerTokens[i]
		compiled.Layers[i] = p.initial[i]
		compiled.Hashes[i] = ctxcompiler.HashLayer(p.initial[i])
		compiled.LayerTokens[i] = counter.Count(p.initial[i])
		compiled.TotalTokens += compiled.LayerTokens[i]
	}
	raw, _ := json.Marshal(p.tools)
	_ = json.Unmarshal(raw, defs)
	return nil
}

// Retire removed facts explicitly, retaining only changed lines at the tail.
func lineDelta(before, after string) string {
	old, next := strings.Split(before, "\n"), strings.Split(after, "\n")
	contains := func(lines []string, s string) bool {
		for _, v := range lines {
			if v == s {
				return true
			}
		}
		return false
	}
	var out []string
	for _, s := range old {
		if s != "" && !contains(next, s) {
			out = append(out, "retired: "+s)
		}
	}
	for _, s := range next {
		if s != "" && !contains(old, s) {
			out = append(out, "current: "+s)
		}
	}
	return strings.Join(out, "\n")
}
