package agenda

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// NodeType identifies the workflow node types of Section 21.4.
type NodeType string

// Workflow node types (21.4).
const (
	NodeTool      NodeType = "tool"
	NodeAgent     NodeType = "agent"
	NodeScript    NodeType = "script"
	NodeCondition NodeType = "condition"
	NodeWait      NodeType = "wait"
	NodeParallel  NodeType = "parallel"
	NodeLoop      NodeType = "loop"
	NodeSubflow   NodeType = "subflow"
	NodeConfirm   NodeType = "confirm"
)

// WaitKind selects what a wait node waits for (21.4).
type WaitKind string

// Wait kinds.
const (
	WaitUntilTime  WaitKind = "time"
	WaitForEvent   WaitKind = "event"
	WaitForConfirm WaitKind = "user_confirm"
)

// Node is one typed node of a versioned workflow graph (21.4).
type Node struct {
	ID   string   `json:"id"`
	Type NodeType `json:"type"`
	Name string   `json:"name"`
	// Tool names the tool for NodeTool.
	Tool string `json:"tool,omitempty"`
	// Effects marks nodes with side effects (21.4 dry run).
	Effects bool `json:"effects,omitempty"`
	// DryRun declares the simulated behavior for dry runs.
	DryRun *DryRunSpec `json:"dry_run,omitempty"`
	// AgentBrief is the brief for NodeAgent (LLM with its brief).
	AgentBrief string `json:"agent_brief,omitempty"`
	// Script holds the script body for NodeScript (any language,
	// run as a low-priority child process by the executor).
	Script string `json:"script,omitempty"`
	// ScriptLang names the interpreter (sh, python3, ...).
	ScriptLang string `json:"script_lang,omitempty"`
	// Condition is the boolean expression for NodeCondition.
	Condition string `json:"condition,omitempty"`
	// Wait configures NodeWait.
	WaitKind WaitKind `json:"wait_kind,omitempty"`
	WaitFor  string   `json:"wait_for,omitempty"`
	// Branches lists parallel branch entry node IDs.
	Branches []string `json:"branches,omitempty"`
	// Body lists loop body node IDs; MaxIterations bounds the loop.
	Body          []string `json:"body,omitempty"`
	MaxIterations int      `json:"max_iterations,omitempty"`
	// Subflow names the nested workflow for NodeSubflow.
	Subflow string `json:"subflow,omitempty"`
	// SubflowVersion pins the nested workflow version (0 = latest).
	SubflowVersion int `json:"subflow_version,omitempty"`
	// ConfirmPrompt is shown at NodeConfirm user-confirmation nodes.
	ConfirmPrompt string `json:"confirm_prompt,omitempty"`
	// Compensation declares the undo step for saga abort (21.5).
	Compensation string `json:"compensation,omitempty"`
}

// DryRunSpec is the declared no-effect behavior of a tool (21.4).
type DryRunSpec struct {
	// Validate lists required input keys.
	Validate []string `json:"validate,omitempty"`
	// Simulated is the canned output returned in dry runs.
	Simulated map[string]any `json:"simulated,omitempty"`
}

// Edge is a typed connection with an optional condition (21.4). An
// empty When is the default transition.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	When string `json:"when,omitempty"`
}

// WorkflowDef is a versioned durable workflow definition (21.4,
// Appendix A: workflows). Instances pin the version they started on.
type WorkflowDef struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Version     int            `json:"version"`
	Description string         `json:"description,omitempty"`
	Nodes       []Node         `json:"nodes"`
	Edges       []Edge         `json:"edges"`
	Tests       []WorkflowTest `json:"tests,omitempty"`
	// Entry is the start node ID (default: the unique sourceless node).
	Entry string `json:"entry,omitempty"`
}

// WorkflowTest is a per-version test: inputs, mocked node responses
// and expected outputs (21.4). Executed on every version change.
type WorkflowTest struct {
	Name          string         `json:"name"`
	Inputs        map[string]any `json:"inputs,omitempty"`
	Mocks         map[string]any `json:"mocks,omitempty"`
	Expected      map[string]any `json:"expected,omitempty"`
	ExpectError   bool           `json:"expect_error,omitempty"`
	ExpectAborted bool           `json:"expect_aborted,omitempty"`
}

// NodeByID returns the node with the given ID.
func (d *WorkflowDef) NodeByID(id string) (*Node, bool) {
	for i := range d.Nodes {
		if d.Nodes[i].ID == id {
			return &d.Nodes[i], true
		}
	}
	return nil, false
}

// Outgoing lists the edges leaving a node.
func (d *WorkflowDef) Outgoing(id string) []Edge {
	var out []Edge
	for _, e := range d.Edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// EntryNode resolves the start node.
func (d *WorkflowDef) EntryNode() (*Node, error) {
	if d.Entry != "" {
		if n, ok := d.NodeByID(d.Entry); ok {
			return n, nil
		}
		return nil, fmt.Errorf("agenda: workflow %q: unknown entry %q", d.ID, d.Entry)
	}
	incoming := map[string]bool{}
	for _, e := range d.Edges {
		incoming[e.To] = true
	}
	// Children of composite nodes are entered through their parent,
	// not as graph roots.
	for _, n := range d.Nodes {
		for _, child := range d.childRefs(n) {
			incoming[child] = true
		}
	}
	var roots []string
	for _, n := range d.Nodes {
		if !incoming[n.ID] {
			roots = append(roots, n.ID)
		}
	}
	if len(roots) != 1 {
		return nil, fmt.Errorf("agenda: workflow %q: want exactly one entry node, have %d", d.ID, len(roots))
	}
	n, _ := d.NodeByID(roots[0])
	return n, nil
}

// Validate checks the definition: node types, edge references, single
// entry, reachability, bounded loops, conditioned branches (21.4).
func (d *WorkflowDef) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("agenda: workflow needs an id")
	}
	if d.Version < 1 {
		return fmt.Errorf("agenda: workflow %q: version must be >= 1", d.ID)
	}
	seen := map[string]bool{}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.ID == "" {
			return fmt.Errorf("agenda: workflow %q: node %d needs an id", d.ID, i)
		}
		if seen[n.ID] {
			return fmt.Errorf("agenda: workflow %q: duplicate node %q", d.ID, n.ID)
		}
		seen[n.ID] = true
		if err := validateNode(d, n); err != nil {
			return err
		}
	}
	for _, e := range d.Edges {
		if !seen[e.From] {
			return fmt.Errorf("agenda: workflow %q: edge from unknown node %q", d.ID, e.From)
		}
		if !seen[e.To] {
			return fmt.Errorf("agenda: workflow %q: edge to unknown node %q", d.ID, e.To)
		}
		if e.When != "" {
			if _, err := ParseCondition(e.When); err != nil {
				return fmt.Errorf("agenda: workflow %q: edge %s->%s: %w", d.ID, e.From, e.To, err)
			}
		}
	}
	if _, err := d.EntryNode(); err != nil {
		return err
	}
	if err := checkReachable(d); err != nil {
		return err
	}
	return nil
}

func validateNode(d *WorkflowDef, n *Node) error {
	switch n.Type {
	case NodeTool:
		if n.Tool == "" {
			return fmt.Errorf("agenda: workflow %q: tool node %q needs a tool", d.ID, n.ID)
		}
	case NodeAgent:
		if n.AgentBrief == "" {
			return fmt.Errorf("agenda: workflow %q: agent node %q needs a brief", d.ID, n.ID)
		}
	case NodeScript:
		if n.Script == "" {
			return fmt.Errorf("agenda: workflow %q: script node %q needs a body", d.ID, n.ID)
		}
	case NodeCondition:
		if n.Condition == "" {
			return fmt.Errorf("agenda: workflow %q: condition node %q needs an expression", d.ID, n.ID)
		}
		if _, err := ParseCondition(n.Condition); err != nil {
			return fmt.Errorf("agenda: workflow %q: node %q: %w", d.ID, n.ID, err)
		}
	case NodeWait:
		switch n.WaitKind {
		case WaitUntilTime, WaitForEvent, WaitForConfirm:
		default:
			return fmt.Errorf("agenda: workflow %q: wait node %q needs a wait kind", d.ID, n.ID)
		}
	case NodeParallel:
		if len(n.Branches) == 0 {
			return fmt.Errorf("agenda: workflow %q: parallel node %q needs branches", d.ID, n.ID)
		}
	case NodeLoop:
		if len(n.Body) == 0 {
			return fmt.Errorf("agenda: workflow %q: loop node %q needs a body", d.ID, n.ID)
		}
		if n.MaxIterations < 1 {
			return fmt.Errorf("agenda: workflow %q: loop node %q must be bounded (max_iterations >= 1)", d.ID, n.ID)
		}
	case NodeSubflow:
		if n.Subflow == "" {
			return fmt.Errorf("agenda: workflow %q: subflow node %q needs a workflow", d.ID, n.ID)
		}
	case NodeConfirm:
		if n.ConfirmPrompt == "" {
			return fmt.Errorf("agenda: workflow %q: confirm node %q needs a prompt", d.ID, n.ID)
		}
	default:
		return fmt.Errorf("agenda: workflow %q: node %q has unknown type %q", d.ID, n.ID, n.Type)
	}
	return nil
}

// checkReachable ensures every node is reachable from the entry
// following edges and composite-node children.
func checkReachable(d *WorkflowDef) error {
	entry, err := d.EntryNode()
	if err != nil {
		return err
	}
	adj := map[string][]string{}
	for _, e := range d.Edges {
		adj[e.From] = append(adj[e.From], e.To)
	}
	for _, n := range d.Nodes {
		switch n.Type {
		case NodeParallel:
			adj[n.ID] = append(adj[n.ID], n.Branches...)
		case NodeLoop:
			adj[n.ID] = append(adj[n.ID], n.Body...)
		}
	}
	seen := map[string]bool{entry.ID: true}
	queue := []string{entry.ID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nx := range adj[cur] {
			if !seen[nx] {
				seen[nx] = true
				queue = append(queue, nx)
			}
		}
	}
	for _, n := range d.Nodes {
		if !seen[n.ID] {
			return fmt.Errorf("agenda: workflow %q: node %q unreachable from entry", d.ID, n.ID)
		}
		// Children of composite nodes must exist.
		for _, child := range d.childRefs(n) {
			if _, ok := d.NodeByID(child); !ok {
				return fmt.Errorf("agenda: workflow %q: node %q references unknown node %q", d.ID, n.ID, child)
			}
		}
	}
	return nil
}

func (d *WorkflowDef) childRefs(n Node) []string {
	switch n.Type {
	case NodeParallel:
		return n.Branches
	case NodeLoop:
		return n.Body
	}
	return nil
}

// ExportGraph renders the definition as nodes+connections JSON-shape
// maps so a UI can draw it n8n-style (21.4).
func (d *WorkflowDef) ExportGraph() map[string]any {
	nodes := make([]any, 0, len(d.Nodes))
	for _, n := range d.Nodes {
		nodes = append(nodes, map[string]any{
			"id": n.ID, "type": string(n.Type), "name": n.Name,
		})
	}
	conns := make([]any, 0, len(d.Edges))
	for _, e := range d.Edges {
		m := map[string]any{"from": e.From, "to": e.To}
		if e.When != "" {
			m["when"] = e.When
		}
		conns = append(conns, m)
	}
	return map[string]any{
		"id": d.ID, "name": d.Name, "version": d.Version,
		"nodes": nodes, "connections": conns,
	}
}

// InstanceState tracks a workflow instance (21.4).
type InstanceState string

// Workflow instance states.
const (
	InstanceRunning        InstanceState = "running"
	InstanceWaitingConfirm InstanceState = "waiting_confirm"
	InstanceWaitingEvent   InstanceState = "waiting_event"
	InstanceDone           InstanceState = "done"
	InstanceAborted        InstanceState = "aborted"
	InstanceFailed         InstanceState = "failed"
)

// StepRecord is one durable node execution step: every node is a
// registered, resumable step (21.4), mirroring the journal pattern.
type StepRecord struct {
	NodeID         string         `json:"node_id"`
	Attempt        int            `json:"attempt"`
	Output         map[string]any `json:"output,omitempty"`
	Error          string         `json:"error,omitempty"`
	SkippedDryRun  bool           `json:"skipped_dry_run,omitempty"`
	Confirmed      *bool          `json:"confirmed,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	At             time.Time      `json:"at_utc"`
}

// WorkflowInstance is one running of a definition, pinned to the
// version it started on (21.4): publishing a new version never alters
// running instances except through an explicit migration.
type WorkflowInstance struct {
	ID            string          `json:"id"`
	WorkflowID    string          `json:"workflow_id"`
	VersionPinned int             `json:"version_pinned"`
	Inputs        map[string]any  `json:"inputs,omitempty"`
	State         InstanceState   `json:"state"`
	Records       []StepRecord    `json:"records,omitempty"`
	Completed     []string        `json:"completed,omitempty"`
	PendingNode   string          `json:"pending_node,omitempty"`
	Outputs       map[string]any  `json:"outputs,omitempty"`
	AbortedReason string          `json:"aborted_reason,omitempty"`
	Confirmations map[string]bool `json:"confirmations,omitempty"`
}

// Migration retargets an instance to a newer definition version (21.4:
// explicit migration only).
func (in *WorkflowInstance) Migration(target *WorkflowDef) error {
	if target.ID != in.WorkflowID {
		return fmt.Errorf("agenda: cannot migrate instance %q to another workflow %q", in.ID, target.ID)
	}
	if target.Version == in.VersionPinned {
		return nil
	}
	in.VersionPinned = target.Version
	return nil
}

// ---- Condition expressions ----

// condNode is a parsed boolean expression over dotted data paths.
type condNode struct {
	op      string
	left    *condNode
	right   *condNode
	path    string
	literal any
}

type condParser struct {
	toks []condTok
	pos  int
}

type condTok struct {
	kind string // ident, string, number, bool, op, paren, comma
	val  string
}

// ParseCondition parses a condition expression. Grammar:
//
//	expr   := or
//	or     := and ("||" and)*
//	and    := unary ("&&" unary)*
//	unary  := "!" unary | comparison | call | "(" expr ")"
//	comparison := path (==|!=|>|>=|<|<=) literal
//	call   := "contains" "(" path "," string ")"
//
// Paths are dotted lookups into the data map; literals are quoted
// strings, numbers or true/false.
func ParseCondition(expr string) (*condNode, error) {
	toks, err := lexCondition(expr)
	if err != nil {
		return nil, err
	}
	p := &condParser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.toks) {
		return nil, fmt.Errorf("agenda: bad condition %q: trailing %q", expr, p.toks[p.pos].val)
	}
	return n, nil
}

func lexCondition(s string) ([]condTok, error) {
	var out []condTok
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')' || c == ',':
			out = append(out, condTok{kind: "paren", val: string(c)})
			i++
		case c == '!':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: "op", val: "!="})
				i += 2
			} else {
				out = append(out, condTok{kind: "op", val: "!"})
				i++
			}
		case c == '&' && i+1 < len(s) && s[i+1] == '&':
			out = append(out, condTok{kind: "op", val: "&&"})
			i += 2
		case c == '|' && i+1 < len(s) && s[i+1] == '|':
			out = append(out, condTok{kind: "op", val: "||"})
			i += 2
		case c == '=' && i+1 < len(s) && s[i+1] == '=':
			out = append(out, condTok{kind: "op", val: "=="})
			i += 2
		case c == '>' || c == '<':
			if i+1 < len(s) && s[i+1] == '=' {
				out = append(out, condTok{kind: "op", val: string(c) + "="})
				i += 2
			} else {
				out = append(out, condTok{kind: "op", val: string(c)})
				i++
			}
		case c == '"' || c == '\'':
			quote := c
			i++
			var value strings.Builder
			closed := false
			for i < len(s) {
				if s[i] == quote {
					i++
					closed = true
					break
				}
				if s[i] == '\\' {
					i++
					if i >= len(s) {
						break
					}
					// Escaping the quote and backslash are unambiguous; preserve
					// other backslashes literally (e.g. Windows paths).
					if s[i] == quote || s[i] == '\\' {
						value.WriteByte(s[i])
					} else {
						value.WriteByte('\\')
						value.WriteByte(s[i])
					}
					i++
					continue
				}
				value.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, fmt.Errorf("agenda: bad condition: unterminated string")
			}
			out = append(out, condTok{kind: "string", val: value.String()})
		case isCondDigit(c) || (c == '-' && i+1 < len(s) && isCondDigit(s[i+1])):
			j := i + 1
			for j < len(s) && (isCondDigit(s[j]) || s[j] == '.') {
				j++
			}
			out = append(out, condTok{kind: "number", val: s[i:j]})
			i = j
		case isCondIdent(c):
			j := i + 1
			for j < len(s) && (isCondIdent(s[j]) || isCondDigit(s[j]) || s[j] == '.') {
				j++
			}
			w := s[i:j]
			if w == "true" || w == "false" {
				out = append(out, condTok{kind: "bool", val: w})
			} else {
				out = append(out, condTok{kind: "ident", val: w})
			}
			i = j
		default:
			return nil, fmt.Errorf("agenda: bad condition: unexpected %q", string(c))
		}
	}
	return out, nil
}

func isCondDigit(c byte) bool { return c >= '0' && c <= '9' }
func isCondIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func (p *condParser) peek() *condTok {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *condParser) next() *condTok {
	t := p.peek()
	if t != nil {
		p.pos++
	}
	return t
}

func (p *condParser) parseOr() (*condNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.val != "||" {
			return left, nil
		}
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &condNode{op: "||", left: left, right: right}
	}
}

func (p *condParser) parseAnd() (*condNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t == nil || t.val != "&&" {
			return left, nil
		}
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &condNode{op: "&&", left: left, right: right}
	}
}

func (p *condParser) parseUnary() (*condNode, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("agenda: bad condition: unexpected end")
	}
	if t.val == "!" {
		p.next()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &condNode{op: "!", left: inner}, nil
	}
	if t.val == "(" {
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		closing := p.next()
		if closing == nil || closing.val != ")" {
			return nil, fmt.Errorf("agenda: bad condition: missing )")
		}
		return inner, nil
	}
	return p.parseComparisonOrCall()
}

func (p *condParser) parseComparisonOrCall() (*condNode, error) {
	t := p.next()
	if t == nil {
		return nil, fmt.Errorf("agenda: bad condition: unexpected end")
	}
	if t.kind == "bool" {
		return &condNode{op: "lit", literal: t.val == "true"}, nil
	}
	if t.kind != "ident" {
		return nil, fmt.Errorf("agenda: bad condition: want path, have %q", t.val)
	}
	// Function call: contains(path, "x").
	if t.val == "contains" {
		open := p.next()
		if open == nil || open.val != "(" {
			return nil, fmt.Errorf("agenda: bad condition: contains needs (path, string)")
		}
		path := p.next()
		comma := p.next()
		sub := p.next()
		close := p.next()
		if path == nil || path.kind != "ident" || comma == nil || comma.val != "," || sub == nil || sub.kind != "string" || close == nil || close.val != ")" {
			return nil, fmt.Errorf("agenda: bad condition: contains needs (path, string)")
		}
		return &condNode{op: "contains", path: path.val, literal: sub.val}, nil
	}
	op := p.next()
	if op == nil || op.kind != "op" || (op.val != "==" && op.val != "!=" && op.val != ">" && op.val != ">=" && op.val != "<" && op.val != "<=") {
		got := ""
		if op != nil {
			got = op.val
		}
		return nil, fmt.Errorf("agenda: bad condition: want comparison after %q, have %q", t.val, got)
	}
	lit := p.next()
	if lit == nil {
		return nil, fmt.Errorf("agenda: bad condition: missing literal")
	}
	var v any
	switch lit.kind {
	case "string":
		v = lit.val
	case "number":
		f, err := strconv.ParseFloat(lit.val, 64)
		if err != nil {
			return nil, fmt.Errorf("agenda: bad condition: bad number %q", lit.val)
		}
		v = f
	case "bool":
		v = lit.val == "true"
	default:
		return nil, fmt.Errorf("agenda: bad condition: want literal, have %q", lit.val)
	}
	return &condNode{op: op.val, path: t.val, literal: v}, nil
}

// Eval evaluates the parsed condition against data with dotted-path
// lookups. Unknown paths evaluate to nil and compare unequal.
func (n *condNode) Eval(data map[string]any) (bool, error) {
	switch n.op {
	case "||":
		l, err := n.left.Eval(data)
		if err != nil || l {
			return l, err
		}
		return n.right.Eval(data)
	case "&&":
		l, err := n.left.Eval(data)
		if err != nil || !l {
			return l, err
		}
		return n.right.Eval(data)
	case "!":
		v, err := n.left.Eval(data)
		return !v, err
	case "lit":
		b, _ := n.literal.(bool)
		return b, nil
	case "contains":
		v := lookupPath(data, n.path)
		sub, _ := n.literal.(string)
		return strings.Contains(fmt.Sprintf("%v", v), sub), nil
	case "==", "!=", ">", ">=", "<", "<=":
		return compareCond(n.op, lookupPath(data, n.path), n.literal), nil
	}
	return false, fmt.Errorf("agenda: bad condition op %q", n.op)
}

// EvalCondition parses and evaluates expr against data.
func EvalCondition(expr string, data map[string]any) (bool, error) {
	n, err := ParseCondition(expr)
	if err != nil {
		return false, err
	}
	return n.Eval(data)
}

func lookupPath(data map[string]any, path string) any {
	var cur any = data
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[part]
		if !ok {
			return nil
		}
	}
	return cur
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func compareCond(op string, a, lit any) bool {
	af, aok := toFloat(a)
	bf, bok := toFloat(lit)
	if aok && bok {
		switch op {
		case "==":
			return af == bf
		case "!=":
			return af != bf
		case ">":
			return af > bf
		case ">=":
			return af >= bf
		case "<":
			return af < bf
		case "<=":
			return af <= bf
		}
		return false
	}
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", lit)
	switch op {
	case "==":
		return as == bs
	case "!=":
		return as != bs
	case ">":
		return as > bs
	case ">=":
		return as >= bs
	case "<":
		return as < bs
	case "<=":
		return as <= bs
	}
	return false
}
