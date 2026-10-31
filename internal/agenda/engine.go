package agenda

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

// NodeContext is what the executor receives for one node execution.
type NodeContext struct {
	Instance       *WorkflowInstance
	Def            *WorkflowDef
	Node           Node
	Inputs         map[string]any
	Attempt        int
	DryRun         bool
	IdempotencyKey string
}

// NodeOutput is the executor result for one node.
type NodeOutput struct {
	Data map[string]any
}

// StepExecutor runs the effectful node kinds (tool, agent, script).
// Condition, wait, parallel, loop, subflow and confirm are handled by
// the engine itself. Test fakes implement this interface.
type StepExecutor interface {
	ExecuteNode(ctx context.Context, nc NodeContext) (NodeOutput, error)
}

// Confirmer resolves user-confirmation nodes (21.4). The daemon wires
// this to the question flow; tests use a scripted fake.
type Confirmer interface {
	// WaitConfirm blocks until the user answers. False declines.
	WaitConfirm(ctx context.Context, instanceID, nodeID, prompt string) (bool, error)
}

// EventWaiter resolves event-wait nodes.
type EventWaiter interface {
	WaitEvent(ctx context.Context, instanceID, nodeID, event string) (map[string]any, error)
}

// Engine runs workflow instances with durable per-node steps (21.4):
// every node is a registered, resumable step keyed by node and
// instance (21.5).
type Engine struct {
	executor  StepExecutor
	confirmer Confirmer
	waiter    EventWaiter
	subflows  map[string]*WorkflowDef
	maxDepth  int
	// recMu guards instance mutation during parallel fan-out.
	recMu sync.Mutex
	// sleep waits between transient retries; overridden in tests.
	sleep func(time.Duration) <-chan time.Time
}

// NewEngine returns an engine. A nil confirmer declines confirmations;
// a nil waiter fails event waits.
func NewEngine(exec StepExecutor) *Engine {
	return &Engine{
		executor: exec,
		subflows: map[string]*WorkflowDef{},
		maxDepth: 8,
		sleep:    func(d time.Duration) <-chan time.Time { return time.After(d) },
	}
}

// WithConfirmer sets the user-confirmation resolver.
func (e *Engine) WithConfirmer(c Confirmer) *Engine { e.confirmer = c; return e }

// WithEventWaiter sets the event-wait resolver.
func (e *Engine) WithEventWaiter(w EventWaiter) *Engine { e.waiter = w; return e }

// RegisterSubflow makes a workflow available to subflow nodes.
func (e *Engine) RegisterSubflow(d *WorkflowDef) error {
	if err := d.Validate(); err != nil {
		return err
	}
	e.subflows[d.ID] = d
	return nil
}

// Start begins an instance pinned to the definition version (21.4).
func (e *Engine) Start(def *WorkflowDef, inputs map[string]any) (*WorkflowInstance, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	cp := map[string]any{}
	for k, v := range inputs {
		cp[k] = v
	}
	return &WorkflowInstance{
		ID:            newFireID(),
		WorkflowID:    def.ID,
		VersionPinned: def.Version,
		Inputs:        cp,
		State:         InstanceRunning,
		Outputs:       map[string]any{},
		Confirmations: map[string]bool{},
	}, nil
}

// Run executes the instance to completion, waiting, abort or failure.
// It is resumable: already-recorded nodes are skipped on re-entry, so
// a crash mid-run replays no completed node (21.4).
func (e *Engine) Run(ctx context.Context, def *WorkflowDef, in *WorkflowInstance) error {
	return e.run(ctx, def, in, 0)
}

func (e *Engine) run(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, depth int) error {
	if depth > e.maxDepth {
		return fmt.Errorf("agenda: subflow depth exceeded in %q", def.ID)
	}
	if in.VersionPinned != def.Version {
		return fmt.Errorf("agenda: instance %q pinned to version %d, definition is %d (migrate explicitly)", in.ID, in.VersionPinned, def.Version)
	}
	entry, err := def.EntryNode()
	if err != nil {
		return err
	}
	start := entry.ID
	scope := copyData(in.Inputs)
	if in.PendingNode != "" {
		// Resume: rebuild the data scope from durable records so the
		// pending node sees the outputs of already-completed nodes.
		start = in.PendingNode
		for _, r := range in.Records {
			if r.Error == "" && !r.SkippedDryRun {
				mergeData(scope, map[string]any{r.NodeID: copyData(r.Output)})
			}
		}
	}
	return e.runFrom(ctx, def, in, start, scope, depth)
}

// runFrom executes the graph from a node with the given data scope.
func (e *Engine) runFrom(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, nodeID string, data map[string]any, depth int) error {
	current := nodeID
	steps := 0
	for current != "" {
		if err := ctx.Err(); err != nil {
			in.State = InstanceWaitingEvent
			in.PendingNode = current
			return err
		}
		steps++
		if steps > 10000 {
			in.State = InstanceFailed
			return fmt.Errorf("agenda: instance %q exceeded step bound (possible unbounded walk)", in.ID)
		}
		n, ok := def.NodeByID(current)
		if !ok {
			in.State = InstanceFailed
			return fmt.Errorf("agenda: unknown node %q", current)
		}
		next, err := e.execNode(ctx, def, in, n, data, depth)
		if err != nil {
			return err
		}
		if in.State == InstanceWaitingConfirm || in.State == InstanceWaitingEvent || in.State == InstanceAborted || in.State == InstanceFailed {
			return nil
		}
		current = next
	}
	in.State = InstanceDone
	in.PendingNode = ""
	return nil
}

// completedSet reports recorded node IDs for resume.
func completedSet(in *WorkflowInstance) map[string]bool {
	done := map[string]bool{}
	for _, r := range in.Records {
		if r.Error == "" || r.SkippedDryRun {
			done[r.NodeID] = true
		}
	}
	return done
}

// execNode executes one node; it returns the next node ID ("" ends).
func (e *Engine) execNode(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any, depth int) (string, error) {
	if completedSet(in)[n.ID] {
		// Resume: replay the recorded output into scope and continue.
		for _, r := range in.Records {
			if r.NodeID == n.ID && r.Error == "" {
				mergeData(data, map[string]any{n.ID: r.Output})
				break
			}
		}
		return e.followEdges(def, n, data)
	}
	// A resumed wait/confirm continues as a live run again.
	if in.State == InstanceWaitingConfirm || in.State == InstanceWaitingEvent {
		in.State = InstanceRunning
	}
	switch n.Type {
	case NodeCondition:
		ok, err := EvalCondition(n.Condition, data)
		if err != nil {
			return e.failNode(in, n, err)
		}
		e.record(in, n.ID, map[string]any{"result": ok}, "", false, nil)
		return e.followEdges(def, n, data)
	case NodeConfirm:
		return e.execConfirm(ctx, def, in, n, data)
	case NodeWait:
		return e.execWait(ctx, def, in, n, data)
	case NodeParallel:
		return e.execParallel(ctx, def, in, n, data, depth)
	case NodeLoop:
		return e.execLoop(ctx, def, in, n, data, depth)
	case NodeSubflow:
		return e.execSubflow(ctx, def, in, n, data, depth)
	default: // NodeTool, NodeAgent, NodeScript
		return e.execEffect(ctx, def, in, n, data)
	}
}

// execEffect runs tool/agent/script nodes with retry, dry-run and
// version-pinned idempotency semantics (21.4, 21.5, 17.7).
func (e *Engine) execEffect(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any) (string, error) {
	key := IdempotencyKeyFor(in.ID, n.ID)
	if dry, skip := dryRunOutcome(n, data); dry {
		if skip {
			// Effects without a declared dry_run never run in a test (21.4).
			e.recordSkipped(in, n.ID, key)
			return e.followEdges(def, n, data)
		}
		out, err := dryRunTool(n, data)
		if err != nil {
			return e.failNode(in, n, err)
		}
		e.record(in, n.ID, out, "", true, nil)
		mergeData(data, map[string]any{n.ID: out})
		return e.followEdges(def, n, data)
	}
	var lastErr error
	for attempt := 1; ; attempt++ {
		nc := NodeContext{Instance: in, Def: def, Node: *n, Inputs: copyData(data), Attempt: attempt, IdempotencyKey: key}
		out, err := e.executor.ExecuteNode(ctx, nc)
		if err == nil {
			e.recordAttempt(in, n.ID, attempt, out.Data, "", key, nil)
			mergeData(data, map[string]any{n.ID: out.Data})
			e.markCompleted(in, n.ID)
			return e.followEdges(def, n, data)
		}
		lastErr = err
		class := ClassifyFailure(err)
		dec := DecideRetry(class, attempt, time.Duration(0))
		e.recordAttempt(in, n.ID, attempt, nil, err.Error()+" ["+string(class)+"]", key, nil)
		if !dec.Retry {
			if class == FailurePermanent || class == FailureInput {
				return e.abortWithCompensations(ctx, def, in, fmt.Sprintf("node %q %s failure: %v", n.ID, class, err))
			}
			return e.failNode(in, n, lastErr)
		}
		select {
		case <-ctx.Done():
			return e.failNode(in, n, ctx.Err())
		case <-e.sleep(dec.Wait):
		}
	}
}

// dryRunOutcome reports whether the engine runs in dry-run mode for
// this node (DryRunInputs marker in scope) and whether the node must
// be skipped.
func dryRunOutcome(n *Node, data map[string]any) (dry, skip bool) {
	if _, ok := data[dryRunMarker]; !ok {
		return false, false
	}
	if (n.Type == NodeTool || n.Type == NodeScript) && n.Effects && n.DryRun == nil {
		return true, true
	}
	return true, false
}

// dryRunMarker enables dry-run interpretation in tests.
const dryRunMarker = "__dry_run__"

// DryRunScope returns a copy of inputs with dry-run mode enabled.
func DryRunScope(inputs map[string]any) map[string]any {
	cp := copyData(inputs)
	cp[dryRunMarker] = true
	return cp
}

func dryRunTool(n *Node, data map[string]any) (map[string]any, error) {
	if n.DryRun != nil {
		for _, k := range n.DryRun.Validate {
			if _, ok := lookupPath(data, k).(map[string]any); !ok {
				if lookupPath(data, k) == nil {
					return nil, fmt.Errorf("agenda: dry run of %q: missing input %q", n.ID, k)
				}
			}
		}
		return copyData(n.DryRun.Simulated), nil
	}
	// Effect-free nodes and agent/script summaries produce placeholders.
	return map[string]any{"dry_run": true, "node": n.ID}, nil
}

func (e *Engine) execConfirm(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any) (string, error) {
	if v, ok := in.Confirmations[n.ID]; ok {
		confirmed := v
		e.record(in, n.ID, map[string]any{"confirmed": confirmed}, "", false, &confirmed)
		if !confirmed {
			return e.abortWithCompensations(ctx, def, in, fmt.Sprintf("user declined at %q", n.ID))
		}
		return e.followEdges(def, n, data)
	}
	if e.confirmer == nil {
		in.State = InstanceWaitingConfirm
		in.PendingNode = n.ID
		return "", nil
	}
	confirmed, err := e.confirmer.WaitConfirm(ctx, in.ID, n.ID, n.ConfirmPrompt)
	if err != nil {
		in.State = InstanceWaitingConfirm
		in.PendingNode = n.ID
		return "", nil
	}
	e.recMu.Lock()
	in.Confirmations[n.ID] = confirmed
	e.recMu.Unlock()
	e.record(in, n.ID, map[string]any{"confirmed": confirmed}, "", false, &confirmed)
	if !confirmed {
		return e.abortWithCompensations(ctx, def, in, fmt.Sprintf("user declined at %q", n.ID))
	}
	return e.followEdges(def, n, data)
}

// execWait handles time/event/user waits (21.4).
func (e *Engine) execWait(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any) (string, error) {
	switch n.WaitKind {
	case WaitUntilTime:
		d, err := time.ParseDuration(n.WaitFor)
		if err != nil {
			if t, terr := time.Parse(time.RFC3339, n.WaitFor); terr == nil {
				d = time.Until(t)
				if d < 0 {
					d = 0
				}
			} else {
				return e.failNode(in, n, fmt.Errorf("agenda: bad wait %q: %w", n.WaitFor, err))
			}
		}
		select {
		case <-ctx.Done():
			in.State = InstanceWaitingEvent
			in.PendingNode = n.ID
			return "", nil
		case <-e.sleep(d):
		}
		e.record(in, n.ID, map[string]any{"waited": n.WaitFor}, "", false, nil)
		return e.followEdges(def, n, data)
	case WaitForEvent:
		if e.waiter == nil {
			in.State = InstanceWaitingEvent
			in.PendingNode = n.ID
			return "", nil
		}
		out, err := e.waiter.WaitEvent(ctx, in.ID, n.ID, n.WaitFor)
		if err != nil {
			in.State = InstanceWaitingEvent
			in.PendingNode = n.ID
			return "", nil
		}
		e.record(in, n.ID, out, "", false, nil)
		mergeData(data, map[string]any{n.ID: out})
		return e.followEdges(def, n, data)
	case WaitForConfirm:
		in.State = InstanceWaitingConfirm
		in.PendingNode = n.ID
		return "", nil
	}
	return e.failNode(in, n, fmt.Errorf("agenda: unknown wait kind"))
}

// execParallel fans branches out concurrently (21.4). Branches must
// be disjoint subgraphs that join at the parallel node's outgoing
// edges; each branch runs against a scratch instance so records never
// race, then merges under lock in branch order, keeping the join
// deterministic.
func (e *Engine) execParallel(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any, depth int) (string, error) {
	type branchOut struct {
		idx     int
		records []StepRecord
		done    []string
		outputs map[string]any
		data    map[string]any
		state   InstanceState
		err     error
	}
	outs := make([]branchOut, len(n.Branches))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, b := range n.Branches {
		i, b := i, b
		wg.Add(1)
		safego.Go("agenda-parallel-"+n.ID, nil, func() {
			defer wg.Done()
			scratch := &WorkflowInstance{
				ID:            fmt.Sprintf("%s/p%d", in.ID, i),
				WorkflowID:    in.WorkflowID,
				VersionPinned: in.VersionPinned,
				Inputs:        copyData(data),
				State:         InstanceRunning,
				Outputs:       map[string]any{},
				Confirmations: map[string]bool{},
			}
			scope := copyData(data)
			err := e.runFrom(ctx, def, scratch, b, scope, depth)
			mu.Lock()
			outs[i] = branchOut{idx: i, records: scratch.Records, done: scratch.Completed, outputs: scratch.Outputs, data: scope, state: scratch.State, err: err}
			mu.Unlock()
		})
	}
	done := make(chan struct{})
	// The join bridge itself goes through safego: no bare goroutines.
	safego.Go("agenda-parallel-join-"+n.ID, nil, func() {
		wg.Wait()
		close(done)
	})
	select {
	case <-ctx.Done():
		// The executor contract requires ctx awareness, so branches
		// converge; only then is it safe to touch the instance.
		<-done
		return e.failNode(in, n, ctx.Err())
	case <-done:
	}
	for _, o := range outs {
		if o.err != nil {
			return e.failNode(in, n, o.err)
		}
		if o.state != InstanceDone {
			return e.failNode(in, n, fmt.Errorf("agenda: parallel branch %d ended in %s", o.idx, o.state))
		}
	}
	for _, o := range outs {
		e.mergeScratch(in, &WorkflowInstance{Records: o.records, Completed: o.done})
	}
	merged := map[string]any{}
	for _, o := range outs {
		for k, v := range o.data {
			if _, exists := data[k]; !exists {
				merged[k] = v
			}
		}
	}
	out := map[string]any{"branches": len(n.Branches)}
	e.record(in, n.ID, out, "", false, nil)
	mergeData(data, merged)
	e.markCompleted(in, n.ID)
	return e.followEdges(def, n, data)
}

// execLoop runs a bounded body (21.4: bucle acotado). Each iteration
// runs in a deterministic scratch child instance, so re-executing a
// body node is never confused with crash resume; records merge into
// the parent in order. A crash mid-loop replays the loop, and effects
// dedupe by the stable per-iteration idempotency keys (17.7).
func (e *Engine) execLoop(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any, depth int) (string, error) {
	iters := 0
	for {
		if iters >= n.MaxIterations {
			break
		}
		scope := copyData(data)
		scratch := &WorkflowInstance{
			ID:            fmt.Sprintf("%s/%s#%d", in.ID, n.ID, iters),
			WorkflowID:    in.WorkflowID,
			VersionPinned: in.VersionPinned,
			Inputs:        scope,
			State:         InstanceRunning,
			Outputs:       map[string]any{},
			Confirmations: map[string]bool{},
		}
		for _, b := range n.Body {
			if err := e.runFrom(ctx, def, scratch, b, scope, depth); err != nil {
				e.mergeScratch(in, scratch)
				return e.failNode(in, n, err)
			}
			// A finished body chain lands the scratch in Done;
			// waiting, aborted or failed states propagate outward.
			switch scratch.State {
			case InstanceDone, InstanceRunning:
			default:
				e.mergeScratch(in, scratch)
				in.State = scratch.State
				in.PendingNode = n.ID
				return "", nil
			}
		}
		e.mergeScratch(in, scratch)
		iters++
		mergeData(data, scope)
		if n.Condition != "" {
			ok, err := EvalCondition(n.Condition, data)

			if err != nil {
				return e.failNode(in, n, err)
			}
			if !ok {
				break
			}
		} else {
			break
		}
	}
	e.record(in, n.ID, map[string]any{"iterations": iters}, "", false, nil)
	e.markCompleted(in, n.ID)
	return e.followEdges(def, n, data)
}

// mergeScratch folds a branch/iteration scratch instance into its parent.
func (e *Engine) mergeScratch(in, scratch *WorkflowInstance) {
	e.recMu.Lock()
	defer e.recMu.Unlock()
	in.Records = append(in.Records, scratch.Records...)
	in.Completed = append(in.Completed, scratch.Completed...)
}

// execSubflow runs a nested workflow with pinned version (21.4).
func (e *Engine) execSubflow(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, n *Node, data map[string]any, depth int) (string, error) {
	sub, ok := e.subflows[n.Subflow]
	if !ok {
		return e.failNode(in, n, fmt.Errorf("agenda: unknown subflow %q", n.Subflow))
	}
	if n.SubflowVersion > 0 && sub.Version != n.SubflowVersion {
		return e.failNode(in, n, fmt.Errorf("agenda: subflow %q version %d not registered (have %d)", n.Subflow, n.SubflowVersion, sub.Version))
	}
	child, err := e.Start(sub, copyData(data))
	if err != nil {
		return e.failNode(in, n, err)
	}
	if err := e.run(ctx, sub, child, depth+1); err != nil {
		return e.failNode(in, n, err)
	}
	if child.State != InstanceDone {
		in.State = child.State
		in.PendingNode = n.ID
		return "", nil
	}
	e.record(in, n.ID, copyData(child.Outputs), "", false, nil)
	mergeData(data, map[string]any{n.ID: copyData(child.Outputs)})
	e.markCompleted(in, n.ID)
	return e.followEdges(def, n, data)
}

func (e *Engine) followEdges(def *WorkflowDef, n *Node, data map[string]any) (string, error) {
	edges := def.Outgoing(n.ID)
	if len(edges) == 0 {
		return "", nil
	}
	var defaults []Edge
	for _, ed := range edges {
		if ed.When == "" {
			defaults = append(defaults, ed)
			continue
		}
		ok, err := EvalCondition(ed.When, data)
		if err != nil {
			return "", err
		}
		if ok {
			return ed.To, nil
		}
	}
	if len(defaults) == 1 {
		return defaults[0].To, nil
	}
	if len(defaults) > 1 {
		return "", fmt.Errorf("agenda: node %q has %d default edges", n.ID, len(defaults))
	}
	return "", fmt.Errorf("agenda: node %q: no edge condition matched", n.ID)
}

func (e *Engine) failNode(in *WorkflowInstance, n *Node, err error) (string, error) {
	e.recordAttempt(in, n.ID, 1, nil, err.Error(), IdempotencyKeyFor(in.ID, n.ID), nil)
	in.State = InstanceFailed
	in.PendingNode = n.ID
	return "", err
}

func (e *Engine) abortInstance(in *WorkflowInstance, reason string) (string, error) {
	in.State = InstanceAborted
	in.AbortedReason = reason
	in.PendingNode = ""
	return "", nil
}

// abortWithCompensations aborts the instance and runs declared
// compensations in reverse completion order (21.5 saga).
func (e *Engine) abortWithCompensations(ctx context.Context, def *WorkflowDef, in *WorkflowInstance, reason string) (string, error) {
	byNode := map[string]Compensation{}
	for _, n := range def.Nodes {
		if n.Compensation != "" {
			byNode[n.ID] = Compensation{NodeID: n.ID, Undo: n.Compensation, UndoKey: "undo_" + IdempotencyKeyFor(in.ID, n.ID)}
		}
	}
	ordered := CompensationOrder(in.Completed, byNode)
	var undone []string
	for _, c := range ordered {
		if e.executor != nil {
			nc := NodeContext{Instance: in, Def: def, Inputs: map[string]any{"compensation": c.Undo}, Attempt: 1, IdempotencyKey: c.UndoKey}
			nc.Node = Node{ID: "compensate_" + c.NodeID, Type: NodeTool, Tool: c.Undo}
			if _, err := e.executor.ExecuteNode(ctx, nc); err != nil {
				e.record(in, "compensate_"+c.NodeID, nil, "compensation failed: "+err.Error(), false, nil)
				continue
			}
		}
		e.record(in, "compensate_"+c.NodeID, map[string]any{"undone": c.NodeID}, "", false, nil)
		undone = append(undone, c.NodeID)
	}
	mergeOutputs(in, map[string]any{"compensations": undone})
	return e.abortInstance(in, reason)
}

func (e *Engine) record(in *WorkflowInstance, nodeID string, out map[string]any, errStr string, dry bool, confirmed *bool) {
	e.recordAttempt(in, nodeID, 1, out, errStr, IdempotencyKeyFor(in.ID, nodeID), confirmed)
	if dry {
		in.Records[len(in.Records)-1].SkippedDryRun = false
	}
}

func (e *Engine) recordAttempt(in *WorkflowInstance, nodeID string, attempt int, out map[string]any, errStr, key string, confirmed *bool) {
	e.recMu.Lock()
	defer e.recMu.Unlock()
	in.Records = append(in.Records, StepRecord{
		NodeID: nodeID, Attempt: attempt, Output: copyData(out),
		Error: errStr, Confirmed: confirmed,
		IdempotencyKey: key, At: time.Now().UTC(),
	})
}

func (e *Engine) recordSkipped(in *WorkflowInstance, nodeID, key string) {
	e.recMu.Lock()
	defer e.recMu.Unlock()
	in.Records = append(in.Records, StepRecord{
		NodeID: nodeID, Attempt: 0, SkippedDryRun: true,
		IdempotencyKey: key, At: time.Now().UTC(),
	})
}

// markCompleted records a compensated-orderable completion (21.5).
func (e *Engine) markCompleted(in *WorkflowInstance, nodeID string) {
	e.recMu.Lock()
	defer e.recMu.Unlock()
	in.Completed = append(in.Completed, nodeID)
}

func mergeData(dst, src map[string]any) {
	for k, v := range src {
		if k == dryRunMarker {
			continue
		}
		dst[k] = v
	}
}

func mergeOutputs(in *WorkflowInstance, src map[string]any) {
	if in.Outputs == nil {
		in.Outputs = map[string]any{}
	}
	for k, v := range src {
		in.Outputs[k] = v
	}
}

func copyData(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// RunTests executes the definition tests with a mock executor (21.4:
// tests run on every version change). Mocks map node IDs to outputs
// (map[string]any) or errors (error or string).
func RunTests(def *WorkflowDef, tests []WorkflowTest) []TestResult {
	if tests == nil {
		tests = def.Tests
	}
	results := make([]TestResult, 0, len(tests))
	for _, t := range tests {
		results = append(results, runOneTest(def, t))
	}
	return results
}

// TestResult is the outcome of one workflow test.
type TestResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Failure string `json:"failure,omitempty"`
}

func runOneTest(def *WorkflowDef, t WorkflowTest) TestResult {
	exec := &mockExecutor{mocks: t.Mocks}
	eng := NewEngine(exec).WithConfirmer(&mockConfirmer{confirm: true})
	in, err := eng.Start(def, copyData(t.Inputs))
	if err != nil {
		return TestResult{Name: t.Name, Failure: err.Error()}
	}
	if err := eng.Run(context.Background(), def, in); err != nil {
		if t.ExpectError || t.ExpectAborted {
			if t.ExpectAborted && in.State != InstanceAborted {
				return TestResult{Name: t.Name, Failure: fmt.Sprintf("want aborted, have %s: %v", in.State, err)}
			}
			return TestResult{Name: t.Name, Passed: true}
		}
		return TestResult{Name: t.Name, Failure: err.Error()}
	}
	if t.ExpectError {
		return TestResult{Name: t.Name, Failure: "want error, got success"}
	}
	if t.ExpectAborted {
		if in.State != InstanceAborted {
			return TestResult{Name: t.Name, Failure: fmt.Sprintf("want aborted, have %s", in.State)}
		}
		return TestResult{Name: t.Name, Passed: true}
	}
	rec := lastOutputs(in)
	for k, want := range t.Expected {
		got, ok := rec[k]
		if !ok {
			return TestResult{Name: t.Name, Failure: fmt.Sprintf("missing output %q", k)}
		}
		if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
			return TestResult{Name: t.Name, Failure: fmt.Sprintf("output %q: want %v, have %v", k, want, got)}
		}
	}
	return TestResult{Name: t.Name, Passed: true}
}

func lastOutputs(in *WorkflowInstance) map[string]any {
	out := map[string]any{}
	for _, r := range in.Records {
		if r.Error == "" {
			for k, v := range r.Output {
				out[r.NodeID+"."+k] = v
				out[k] = v
			}
		}
	}
	for k, v := range in.Outputs {
		out[k] = v
	}
	return out
}

// mockExecutor serves canned node outputs for workflow tests.
type mockExecutor struct {
	mu    sync.Mutex
	mocks map[string]any
	calls []string
}

func (m *mockExecutor) ExecuteNode(_ context.Context, nc NodeContext) (NodeOutput, error) {
	m.mu.Lock()
	m.calls = append(m.calls, nc.Node.ID)
	m.mu.Unlock()
	if v, ok := m.mocks[nc.Node.ID]; ok {
		switch t := v.(type) {
		case error:
			return NodeOutput{}, t
		case string:
			if t == "error" {
				return NodeOutput{}, fmt.Errorf("mock failure at %s", nc.Node.ID)
			}
			return NodeOutput{Data: map[string]any{"value": t}}, nil
		case map[string]any:
			return NodeOutput{Data: t}, nil
		default:
			return NodeOutput{Data: map[string]any{"value": t}}, nil
		}
	}
	// Unmocked effect-free nodes get placeholders; unmocked effect
	// nodes fail the test rather than inventing effects.
	if nc.Node.Effects {
		return NodeOutput{}, fmt.Errorf("mock has no response for effect node %q", nc.Node.ID)
	}
	return NodeOutput{Data: map[string]any{"mock": nc.Node.ID}}, nil
}

// mockConfirmer auto-confirms for tests.
type mockConfirmer struct {
	confirm bool
	calls   []string
}

func (m *mockConfirmer) WaitConfirm(_ context.Context, _, nodeID, _ string) (bool, error) {
	m.calls = append(m.calls, nodeID)
	return m.confirm, nil
}
