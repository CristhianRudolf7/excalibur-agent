package agenda

import (
	"fmt"
	"sort"
	"strings"
)

// Canonical flow model for textual simulation (21.6). A flow is a
// compact list of nodes: one line per node with id, kind, condition
// and possible outputs, e.g. "D3 condition:payment approved -> yes: N4
// | no: N7". Decisions, loops and planted errors enumerate to
// walkthroughs with descriptors, narrations and review findings.

// FlowNodeKind distinguishes simulation nodes.
type FlowNodeKind string

// Flow node kinds (21.6).
const (
	FlowStep      FlowNodeKind = "step"
	FlowDecision  FlowNodeKind = "decision"
	FlowLoop      FlowNodeKind = "loop"
	FlowEnd       FlowNodeKind = "end"
	FlowErrorKind FlowNodeKind = "error"
)

// FlowNode is one canonical node: id, kind, question and outputs.
// For decisions, Outputs maps outcome label to next node ID.
// A loop node repeats Body up to Bound times.
type FlowNode struct {
	ID       string            `json:"id"`
	Kind     FlowNodeKind      `json:"kind"`
	Question string            `json:"question,omitempty"`
	Outputs  map[string]string `json:"outputs,omitempty"`
	Body     []string          `json:"body,omitempty"`
	Bound    int               `json:"bound,omitempty"`
	// ErrorHere marks a planted error at this node (suite fixtures).
	ErrorHere string `json:"error_here,omitempty"`
	// NoExit marks a node with no outgoing transition (dead-end probe).
	NoExit bool `json:"no_exit,omitempty"`
}

// CanonicalFlow is the normalized compact representation (21.6 step 1).
type CanonicalFlow struct {
	ID    string     `json:"id"`
	Nodes []FlowNode `json:"nodes"`
	Entry string     `json:"entry"`
}

// FlowByID returns the node with the given ID.
func (f *CanonicalFlow) FlowByID(id string) (*FlowNode, bool) {
	for i := range f.Nodes {
		if f.Nodes[i].ID == id {
			return &f.Nodes[i], true
		}
	}
	return nil, false
}

// ParseCanonicalFlow parses the one-line-per-node format (21.6 step 1):
//
//	D3 decision:payment approved -> yes: N4 | no: N7
//	N4 step:notify customer -> N8
//	B2 loop:retry up to 3 -> body: R1, R2 -> N9
//	N9 end:
//
// A step with "->" and no target ends the walkthrough.
func ParseCanonicalFlow(id string, lines []string) (*CanonicalFlow, error) {
	flow := &CanonicalFlow{ID: id}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		node, err := parseFlowLine(line)
		if err != nil {
			return nil, err
		}
		flow.Nodes = append(flow.Nodes, *node)
	}
	if len(flow.Nodes) == 0 {
		return nil, fmt.Errorf("agenda: flow %q has no nodes", id)
	}
	flow.Entry = flow.Nodes[0].ID
	if err := flow.Validate(); err != nil {
		return nil, err
	}
	return flow, nil
}

func parseFlowLine(line string) (*FlowNode, error) {
	head, rest, _ := strings.Cut(line, " ")
	node := &FlowNode{ID: strings.TrimSpace(head)}
	rest = strings.TrimSpace(rest)
	kind, payload, _ := strings.Cut(rest, ":")
	switch FlowNodeKind(strings.TrimSpace(kind)) {
	case FlowStep:
		node.Kind = FlowStep
	case FlowDecision:
		node.Kind = FlowDecision
	case FlowLoop:
		node.Kind = FlowLoop
	case FlowEnd:
		node.Kind = FlowEnd
	case FlowErrorKind:
		node.Kind = FlowErrorKind
	default:
		return nil, fmt.Errorf("agenda: bad flow line %q: unknown kind %q", line, kind)
	}
	payload = strings.TrimSpace(payload)
	// Split outputs on "->" and "|" (but not inside "up to").
	node.Question, node.Outputs = parseFlowOutputs(payload, node)
	return node, nil
}

func parseFlowOutputs(payload string, node *FlowNode) (string, map[string]string) {
	outs := map[string]string{}
	// Loop form: "retry up to 3 -> body: R1, R2 -> N9".
	if node.Kind == FlowLoop {
		head, tail, _ := strings.Cut(payload, "->")
		head = strings.TrimSpace(head)
		var bound int
		if _, err := fmt.Sscanf(head, "%*s up to %d", &bound); err == nil && bound > 0 {
			node.Bound = bound
			words := strings.Fields(head)
			node.Question = strings.Join(words[:len(words)-3], " ")
		} else {
			node.Question = head
			node.Bound = 2
		}
		rest := strings.TrimSpace(tail)
		if body, after, ok := strings.Cut(rest, "->"); ok {
			rest = strings.TrimSpace(after)
			body = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "body:"))
			for _, b := range strings.Split(body, ",") {
				if s := strings.TrimSpace(b); s != "" {
					node.Body = append(node.Body, s)
				}
			}
			if rest != "" {
				outs["done"] = rest
			}
			return node.Question, outs
		}
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "body:"))
		for _, b := range strings.Split(rest, ",") {
			if s := strings.TrimSpace(b); s != "" {
				node.Body = append(node.Body, s)
			}
		}
		return node.Question, outs
	}
	head, tail, hasArrow := strings.Cut(payload, "->")
	if !hasArrow {
		return strings.TrimSpace(payload), outs
	}
	q := strings.TrimSpace(head)
	tail = strings.TrimSpace(tail)
	if tail == "" {
		return q, outs
	}
	if node.Kind == FlowDecision {
		for _, part := range strings.Split(tail, "|") {
			label, target, _ := strings.Cut(part, ":")
			label, target = strings.TrimSpace(label), strings.TrimSpace(target)
			if label == "" || target == "" {
				continue
			}
			outs[label] = target
		}
		return q, outs
	}
	outs["next"] = tail
	return q, outs
}

// Validate checks node references and loop bounds.
func (f *CanonicalFlow) Validate() error {
	known := map[string]bool{}
	for _, n := range f.Nodes {
		if known[n.ID] {
			return fmt.Errorf("agenda: flow %q: duplicate node %q", f.ID, n.ID)
		}
		known[n.ID] = true
	}
	for _, n := range f.Nodes {
		for _, to := range n.Outputs {
			if !known[to] {
				return fmt.Errorf("agenda: flow %q: node %q points at unknown %q", f.ID, n.ID, to)
			}
		}
		for _, b := range n.Body {
			if !known[b] {
				return fmt.Errorf("agenda: flow %q: loop %q references unknown %q", f.ID, n.ID, b)
			}
		}
		if n.Kind == FlowLoop && n.Bound < 1 {
			return fmt.Errorf("agenda: flow %q: loop %q must be bounded (>= 1)", f.ID, n.ID)
		}
		if n.Kind == FlowDecision && len(n.Outputs) == 0 && !n.NoExit {
			return fmt.Errorf("agenda: flow %q: decision %q has no outputs", f.ID, n.ID)
		}
	}
	return nil
}

// Decisions lists the decision node IDs in order.
func (f *CanonicalFlow) Decisions() []string {
	var out []string
	for _, n := range f.Nodes {
		if n.Kind == FlowDecision {
			out = append(out, n.ID)
		}
	}
	return out
}

// CountWalkthroughs predicts the total number of walkthroughs with
// loops unfolded to at most k turns each (21.6: the count precedes
// enumeration, and exhaustive mode needs total <= limit).
func (f *CanonicalFlow) CountWalkthroughs(k int) int {
	if k < 1 {
		k = 1
	}
	memo := map[string]int{}
	var count func(id string, depth int) int
	count = func(id string, depth int) int {
		if depth > 1000 {
			return 1
		}
		n, ok := f.FlowByID(id)
		if !ok {
			return 1
		}
		switch n.Kind {
		case FlowEnd:
			return 1
		case FlowStep, FlowErrorKind:
			next, ok := n.Outputs["next"]
			if !ok {
				if n.NoExit || len(n.Outputs) == 0 {
					return 1
				}
				return 1
			}
			return count(next, depth+1)
		case FlowDecision:
			if len(n.Outputs) == 0 {
				return 1
			}
			total := 0
			for _, to := range n.Outputs {
				total += count(to, depth+1)
			}
			return total
		case FlowLoop:
			// Sum over 1..k executed turns, each turn sequencing body.
			key := fmt.Sprintf("%s@%d", id, depth)
			if v, ok := memo[key]; ok {
				return v
			}
			sub := 1
			for _, b := range n.Body {
				sub *= count(b, depth+1)
			}
			total := 0
			pow := 1
			for t := 1; t <= k && t <= n.Bound; t++ {
				pow *= sub
				total += pow
			}
			after := 1
			if to, ok := n.Outputs["done"]; ok {
				after = count(to, depth+1)
			}
			memo[key] = total * after
			return total * after
		}
		return 1
	}
	return count(f.Entry, 0)
}

// Walkthrough is one enumerated recorrido (21.6 step 2).
type Walkthrough struct {
	// Choices records decision outcomes: "D1=yes".
	Choices []string `json:"choices"`
	// Visits is the node sequence.
	Visits []string `json:"visits"`
	// Errors lists planted errors encountered.
	Errors []string `json:"errors,omitempty"`
}

// Descriptor is the lossless compact form (21.6 step 3):
// "D1=yes, D3=no, B2x2". Together with the canonical flow it
// regenerates the full walkthrough.
func (w Walkthrough) Descriptor() string {
	return strings.Join(w.Choices, ", ")
}

// SimulationCoverage selects the enumeration strategy (21.6 step 2).
type SimulationCoverage string

// Coverage strategies (21.6).
const (
	CoverageDecisions SimulationCoverage = "decisions"
	CoveragePairs     SimulationCoverage = "pairs"
	CoverageFull      SimulationCoverage = "exhaustive"
)

// ExhaustiveLimit caps exhaustive enumeration (Appendix B: 500).
const ExhaustiveLimit = 500

// SimulationReport is the review output (21.6 step 5).
type SimulationReport struct {
	FlowID   string             `json:"flow_id"`
	Coverage SimulationCoverage `json:"coverage"`
	// Exhaustive is true only when the total fit under the limit and
	// every walkthrough was enumerated.
	Exhaustive bool `json:"exhaustive"`
	Total      int  `json:"total_walkthroughs"`
	// DecisionCoverage maps "D1=yes" to the walkthrough indexes covering it.
	DecisionCoverage map[string][]int `json:"decision_coverage"`
	// PairCoverage maps "D1=yes+D3=no" pairs to walkthrough indexes.
	PairCoverage map[string][]int `json:"pair_coverage,omitempty"`
	Walkthroughs []Walkthrough    `json:"walkthroughs"`
	// Narrations holds one natural-language narration per walkthrough.
	Narrations []string `json:"narrations"`
	// Findings lists unexpected results, dead ends and endless loops.
	Findings []string `json:"findings"`
	// DescriptorRoundtrip is true when every descriptor regenerated
	// its walkthrough.
	DescriptorRoundtrip bool `json:"descriptor_roundtrip"`
}

// Simulate enumerates walkthroughs per the coverage strategy (21.6).
// Loops unfold to at most loopUnfold turns each.
func Simulate(f *CanonicalFlow, coverage SimulationCoverage, loopUnfold int) (*SimulationReport, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	if loopUnfold < 1 {
		loopUnfold = 1
	}
	total := f.CountWalkthroughs(loopUnfold)
	all := enumerateAll(f, loopUnfold, ExhaustiveLimit*4)
	rep := &SimulationReport{FlowID: f.ID, Coverage: coverage, Total: total, DecisionCoverage: map[string][]int{}}
	switch coverage {
	case CoverageDecisions:
		rep.Walkthroughs = coverDecisions(all)
	case CoveragePairs:
		rep.Walkthroughs = coverPairs(f, all)
		rep.PairCoverage = pairIndex(rep.Walkthroughs)
	case CoverageFull:
		if total > ExhaustiveLimit {
			return nil, fmt.Errorf("agenda: flow %q has %d walkthroughs, over the exhaustive limit %d: use decisions or pairs coverage", f.ID, total, ExhaustiveLimit)
		}
		rep.Walkthroughs = all
		rep.Exhaustive = true
		rep.PairCoverage = pairIndex(rep.Walkthroughs)
	default:
		return nil, fmt.Errorf("agenda: unknown coverage %q", coverage)
	}
	rep.DecisionCoverage = decisionIndex(rep.Walkthroughs)
	rep.Narrations = make([]string, len(rep.Walkthroughs))
	for i, w := range rep.Walkthroughs {
		rep.Narrations[i] = Narrate(f, w, i+1)
	}
	rep.Findings = Review(f, rep.Walkthroughs)
	rep.DescriptorRoundtrip = checkRoundtrip(f, rep.Walkthroughs)
	return rep, nil
}

// enumerateAll unfolds every path up to a cap.
func enumerateAll(f *CanonicalFlow, k, cap int) []Walkthrough {
	var out []Walkthrough
	var walk func(id string, choices, visits []string, errs []string, loops map[string]int)
	walk = func(id string, choices, visits []string, errs []string, loops map[string]int) {
		if len(out) >= cap {
			return
		}
		n, ok := f.FlowByID(id)
		if !ok || len(visits) > 500 {
			out = append(out, Walkthrough{Choices: append([]string(nil), choices...), Visits: append([]string(nil), visits...), Errors: append([]string(nil), errs...)})
			return
		}
		visits = append(visits, id)
		if n.ErrorHere != "" {
			errs = append(errs, n.ID+": "+n.ErrorHere)
		}
		switch n.Kind {
		case FlowEnd:
			out = append(out, Walkthrough{Choices: append([]string(nil), choices...), Visits: visits, Errors: append([]string(nil), errs...)})
		case FlowStep, FlowErrorKind:
			next, ok := n.Outputs["next"]
			if !ok {
				out = append(out, Walkthrough{Choices: append([]string(nil), choices...), Visits: visits, Errors: append([]string(nil), errs...)})
				return
			}
			walk(next, choices, visits, errs, loops)
		case FlowDecision:
			if len(n.Outputs) == 0 {
				out = append(out, Walkthrough{Choices: append([]string(nil), choices...), Visits: visits, Errors: append([]string(nil), errs...)})
				return
			}
			labels := sortedMapKeys(n.Outputs)
			for _, label := range labels {
				walk(n.Outputs[label], append(append([]string(nil), choices...), n.ID+"="+label), visits, errs, loops)
				if len(out) >= cap {
					return
				}
			}
		case FlowLoop:
			var enter func(lmap map[string]int, ch, vs []string, er []string)
			enter = func(lmap map[string]int, ch, vs []string, er []string) {
				if len(out) >= cap {
					return
				}
				used := lmap[n.ID]
				if used < k && used < n.Bound {
					next := copyIntMap(lmap)
					next[n.ID] = used + 1
					var unfold func(idx int, ch []string, vs []string, er []string)
					unfold = func(idx int, ch []string, vs []string, er []string) {
						if idx == len(n.Body) {
							// One full turn done: loop again or exit.
							enter(next, ch, vs, er)
							return
						}
						b := n.Body[idx]
						bn, ok := f.FlowByID(b)
						if !ok {
							unfold(idx+1, ch, vs, er)
							return
						}
						if bn.Kind == FlowDecision {
							for _, label := range sortedMapKeys(bn.Outputs) {
								unfold(idx+1,
									append(append([]string(nil), ch...), bn.ID+"="+label),
									append(append([]string(nil), vs...), b+"="+label),
									appendErrIf(er, bn))
							}
							return
						}
						unfold(idx+1, ch,
							append(append([]string(nil), vs...), b),
							appendErrIf(er, bn))
					}
					unfold(0, ch, vs, er)
				} else {
					// Loop finished after `used` turns: record the turn
					// marker in the descriptor, then exit or end.
					ch = append(append([]string(nil), ch...), fmt.Sprintf("%sx%d", n.ID, used))
					if to, ok := n.Outputs["done"]; ok {
						walk(to, ch, vs, er, lmap)
					} else {
						out = append(out, Walkthrough{Choices: ch, Visits: vs, Errors: er})
					}
				}
			}
			enter(loops, choices, visits, errs)
		}
	}
	walk(f.Entry, nil, nil, nil, map[string]int{})
	return out
}

// appendErrIf records a planted error found at a body node.
func appendErrIf(errs []string, bn *FlowNode) []string {
	if bn.ErrorHere != "" {
		return append(append([]string(nil), errs...), bn.ID+": "+bn.ErrorHere)
	}
	return errs
}

// coverDecisions keeps a minimal set covering every decision outcome.
func coverDecisions(all []Walkthrough) []Walkthrough {
	covered := map[string]bool{}
	var out []Walkthrough
	for _, w := range all {
		adds := false
		for _, c := range w.Choices {
			if !covered[c] {
				adds = true
			}
		}
		if adds {
			for _, c := range w.Choices {
				covered[c] = true
			}
			out = append(out, w)
		}
	}
	return out
}

// coverPairs keeps walkthroughs covering interacting decision pairs.
func coverPairs(f *CanonicalFlow, all []Walkthrough) []Walkthrough {
	decisions := f.Decisions()
	if len(decisions) < 2 {
		return coverDecisions(all)
	}
	need := map[string]bool{}
	for _, w := range all {
		for i := 0; i < len(w.Choices); i++ {
			for j := i + 1; j < len(w.Choices); j++ {
				need[pairKey(w.Choices[i], w.Choices[j])] = true
			}
		}
	}
	covered := map[string]bool{}
	var out []Walkthrough
	for _, w := range all {
		adds := false
		for i := 0; i < len(w.Choices); i++ {
			for j := i + 1; j < len(w.Choices); j++ {
				if k := pairKey(w.Choices[i], w.Choices[j]); need[k] && !covered[k] {
					adds = true
				}
			}
		}
		if adds {
			for i := 0; i < len(w.Choices); i++ {
				for j := i + 1; j < len(w.Choices); j++ {
					covered[pairKey(w.Choices[i], w.Choices[j])] = true
				}
			}
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return coverDecisions(all)
	}
	return out
}

func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "+" + b
}

func decisionIndex(ws []Walkthrough) map[string][]int {
	idx := map[string][]int{}
	for i, w := range ws {
		for _, c := range w.Choices {
			idx[c] = append(idx[c], i)
		}
	}
	return idx
}

func pairIndex(ws []Walkthrough) map[string][]int {
	idx := map[string][]int{}
	for i, w := range ws {
		for a := 0; a < len(w.Choices); a++ {
			for b := a + 1; b < len(w.Choices); b++ {
				k := pairKey(w.Choices[a], w.Choices[b])
				idx[k] = append(idx[k], i)
			}
		}
	}
	return idx
}

// Narrate renders one walkthrough in natural language (21.6 step 4).
// Structure comes from deterministic templates; a cheap model only
// polishes wording.
func Narrate(f *CanonicalFlow, w Walkthrough, num int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Walkthrough %d: ", num)
	parts := []string{}
	choiceByNode := map[string]string{}
	loopTurns := map[string]string{}
	for _, c := range w.Choices {
		if id, val, ok := strings.Cut(c, "="); ok {
			choiceByNode[id] = val
			continue
		}
		if base, num, ok := strings.Cut(c, "x"); ok {
			if _, exists := f.FlowByID(base); exists {
				loopTurns[base] = num
			}
		}
	}
	for _, v := range w.Visits {
		base := v
		if i := strings.Index(v, "="); i >= 0 {
			base = v[:i]
		}
		n, ok := f.FlowByID(base)
		if !ok {
			parts = append(parts, v)
			continue
		}
		switch n.Kind {
		case FlowStep:
			if n.Question != "" {
				parts = append(parts, n.Question)
			} else {
				parts = append(parts, base)
			}
		case FlowDecision:
			q := n.Question
			if q == "" {
				q = base
			}
			if val, ok := choiceByNode[base]; ok {
				parts = append(parts, fmt.Sprintf("since %s is %s", q, val))
			} else if strings.Contains(base, "=") {
				parts = append(parts, fmt.Sprintf("decides %s", v))
			} else {
				parts = append(parts, fmt.Sprintf("decides %s", q))
			}
		case FlowLoop:
			parts = append(parts, fmt.Sprintf("loops %s", n.Question))
		case FlowErrorKind:
			parts = append(parts, fmt.Sprintf("fails at %s (%s)", base, n.ErrorHere))
		case FlowEnd:
			if n.Question != "" {
				parts = append(parts, n.Question)
			} else {
				parts = append(parts, "the flow ends")
			}
		}
	}
	sb.WriteString(strings.Join(parts, "; "))
	sb.WriteString(".")
	for _, id := range sortedMapKeys(loopTurns) {
		fmt.Fprintf(&sb, " Loop %s runs %s turn(s).", id, loopTurns[id])
	}
	if len(w.Errors) > 0 {
		fmt.Fprintf(&sb, " Planted errors: %s.", strings.Join(w.Errors, "; "))
	}
	fmt.Fprintf(&sb, " [%s]", w.Descriptor())
	return sb.String()
}

// Review reads the walkthroughs for unexpected results, dead ends and
// endless loops (21.6 step 5).
func Review(f *CanonicalFlow, ws []Walkthrough) []string {
	var findings []string
	for i, w := range ws {
		if len(w.Visits) == 0 {
			findings = append(findings, fmt.Sprintf("walkthrough %d is empty", i+1))
			continue
		}
		last := w.Visits[len(w.Visits)-1]
		base := last
		if idx := strings.Index(last, "="); idx >= 0 {
			base = last[:idx]
		}
		if idx := strings.Index(base, "x"); idx > 1 {
			base = base[:idx]
		}
		n, ok := f.FlowByID(base)
		if !ok {
			findings = append(findings, fmt.Sprintf("walkthrough %d ends at unknown node %q", i+1, last))
			continue
		}
		switch n.Kind {
		case FlowEnd:
			// clean end
		case FlowErrorKind:
			findings = append(findings, fmt.Sprintf("walkthrough %d ends in error at %q: %s", i+1, n.ID, n.ErrorHere))
		default:
			hasNext := len(n.Outputs) > 0 || n.Kind == FlowLoop
			if n.NoExit || !hasNext {
				findings = append(findings, fmt.Sprintf("walkthrough %d ends at %q with no exit (dead end)", i+1, n.ID))
			}
		}
		if len(w.Errors) > 0 {
			findings = append(findings, fmt.Sprintf("walkthrough %d hits planted errors: %s", i+1, strings.Join(w.Errors, "; ")))
		}
		seen := map[string]int{}
		for _, v := range w.Visits {
			seen[v]++
			if seen[v] > 10 {
				findings = append(findings, fmt.Sprintf("walkthrough %d revisits %q %d times (possible endless loop)", i+1, v, seen[v]))
				break
			}
		}
	}
	// Decisions with a single outcome across all walkthroughs are
	// suspicious: one branch never executes.
	outcomes := map[string]map[string]bool{}
	for _, w := range ws {
		for _, c := range w.Choices {
			if id, val, ok := strings.Cut(c, "="); ok {
				if outcomes[id] == nil {
					outcomes[id] = map[string]bool{}
				}
				outcomes[id][val] = true
			}
		}
	}
	for _, d := range f.Decisions() {
		if n, ok := f.FlowByID(d); ok && len(n.Outputs) > 1 && len(outcomes[d]) < 2 {
			findings = append(findings, fmt.Sprintf("decision %q never varies across walkthroughs", d))
		}
	}
	return findings
}

// checkRoundtrip verifies every descriptor regenerates its walkthrough
// (21.6 step 3: the descriptor is lossless with the canonical flow).
func checkRoundtrip(f *CanonicalFlow, ws []Walkthrough) bool {
	for _, w := range ws {
		regen, err := regenerate(f, w.Descriptor())
		if err != nil {
			return false
		}
		if strings.Join(regen.Visits, "|") != strings.Join(w.Visits, "|") {
			return false
		}
	}
	return true
}

// regenerate rebuilds a walkthrough from its descriptor. Decision
// outcomes are consumed in order (repeated "R1=yes, R1=no" entries
// feed successive loop turns); "BxN" markers fix the turn counts.
func regenerate(f *CanonicalFlow, descriptor string) (Walkthrough, error) {
	type token struct {
		id  string
		val string
		// loop, if true, is a turn-count marker (id "BxN" form).
		loop  bool
		turns int
	}
	var tokens []token
	if strings.TrimSpace(descriptor) != "" {
		for _, c := range strings.Split(descriptor, ",") {
			c = strings.TrimSpace(c)
			id, val, hasEq := strings.Cut(c, "=")
			id, val = strings.TrimSpace(id), strings.TrimSpace(val)
			if !hasEq {
				// Loop turn marker without "=": "B2x2".
				if base, num, ok := strings.Cut(id, "x"); ok {
					if _, exists := f.FlowByID(base); exists {
						var turns int
						if _, err := fmt.Sscanf(num, "%d", &turns); err == nil && turns > 0 {
							tokens = append(tokens, token{id: base, loop: true, turns: turns})
							continue
						}
					}
				}
				return Walkthrough{}, fmt.Errorf("agenda: bad descriptor %q", descriptor)
			}
			tokens = append(tokens, token{id: id, val: val})
		}
	}
	queues := map[string][]string{}
	loopTurns := map[string]int{}
	for _, t := range tokens {
		if t.loop {
			loopTurns[t.id] = t.turns
			continue
		}
		queues[t.id] = append(queues[t.id], t.val)
	}
	var choices, visits []string
	var errs []string
	cur := f.Entry
	guard := 0
	loops := map[string]int{}
	for {
		guard++
		if guard > 1000 {
			return Walkthrough{}, fmt.Errorf("agenda: descriptor walk did not terminate")
		}
		n, ok := f.FlowByID(cur)
		if !ok {
			return Walkthrough{}, fmt.Errorf("agenda: unknown node %q", cur)
		}
		visits = append(visits, cur)
		if n.ErrorHere != "" {
			errs = append(errs, cur+": "+n.ErrorHere)
		}
		switch n.Kind {
		case FlowEnd:
			return Walkthrough{Choices: choices, Visits: visits, Errors: errs}, nil
		case FlowStep, FlowErrorKind:
			next, ok := n.Outputs["next"]
			if !ok {
				return Walkthrough{Choices: choices, Visits: visits, Errors: errs}, nil
			}
			cur = next
		case FlowDecision:
			q := queues[n.ID]
			if len(q) == 0 {
				return Walkthrough{}, fmt.Errorf("agenda: descriptor lacks %q", n.ID)
			}
			val := q[0]
			queues[n.ID] = q[1:]
			next, ok := n.Outputs[val]
			if !ok {
				return Walkthrough{}, fmt.Errorf("agenda: descriptor has bad outcome %q=%q", n.ID, val)
			}
			choices = append(choices, n.ID+"="+val)
			cur = next
		case FlowLoop:
			target, ok := loopTurns[n.ID]
			if !ok {
				target = 1
			}
			if target < 1 {
				target = 1
			}
			if target > n.Bound {
				return Walkthrough{}, fmt.Errorf("agenda: descriptor exceeds loop bound at %q", n.ID)
			}
			turns := 0
			for turns < target {
				turns++
				loops[n.ID]++
				for _, b := range n.Body {
					bn, ok := f.FlowByID(b)
					if !ok {
						continue
					}
					if bn.Kind == FlowDecision {
						q := queues[b]
						if len(q) == 0 {
							return Walkthrough{}, fmt.Errorf("agenda: descriptor lacks %q", b)
						}
						val := q[0]
						queues[b] = q[1:]
						if _, ok := bn.Outputs[val]; !ok {
							return Walkthrough{}, fmt.Errorf("agenda: descriptor has bad outcome %q=%q", b, val)
						}
						choices = append(choices, b+"="+val)
						visits = append(visits, b+"="+val)
						if bn.ErrorHere != "" {
							errs = append(errs, b+": "+bn.ErrorHere)
						}
					} else {
						visits = append(visits, b)
						if bn.ErrorHere != "" {
							errs = append(errs, b+": "+bn.ErrorHere)
						}
					}
				}
			}
			choices = append(choices, fmt.Sprintf("%sx%d", n.ID, turns))
			if to, ok := n.Outputs["done"]; ok {
				cur = to
			} else {
				return Walkthrough{Choices: choices, Visits: visits, Errors: errs}, nil
			}
		}
	}
}

func sortedMapKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func copyIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
