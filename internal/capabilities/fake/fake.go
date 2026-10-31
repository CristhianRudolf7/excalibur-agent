package fake

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// ScriptedCapability is a contract fake for a black-box capability
// (Sections 14.1, 14.13): scripted operation outcomes, idempotency-key
// dedup counting, and a durable-task progression for working,
// input_required, completed, failed and cancelled with restart-safe
// task ids.
type ScriptedCapability struct {
	ManifestValue capabilities.Manifest

	mu sync.Mutex
	// Results scripted per operation in call order.
	Results map[string][]capabilities.TaskResult
	// Errs forces Invoke to fail for an operation.
	Errs map[string]error
	// Calls counts Invoke calls per operation.
	Calls map[string]int
	// Effects counts first-effect executions per idempotency key:
	// repeats with the same key do not increment it.
	Effects map[string]int
	seenKEY map[string]bool
	// Tasks stores durable tasks by id for Poll/Cancel/Update.
	Tasks map[string][]capabilities.TaskResult
	// Next overrides the queued progression per task id.
	Next map[string][]capabilities.TaskResult
}

// New returns a fake with the given manifest.
func New(m capabilities.Manifest) *ScriptedCapability {
	return &ScriptedCapability{
		ManifestValue: m,
		Results:       map[string][]capabilities.TaskResult{},
		Errs:          map[string]error{},
		Calls:         map[string]int{},
		Effects:       map[string]int{},
		seenKEY:       map[string]bool{},
		Tasks:         map[string][]capabilities.TaskResult{},
		Next:          map[string][]capabilities.TaskResult{},
	}
}

// Manifest implements capabilities.Capability.
func (f *ScriptedCapability) Manifest() capabilities.Manifest { return f.ManifestValue }

// Invoke implements capabilities.Capability.
func (f *ScriptedCapability) Invoke(operation string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls[operation]++
	if err, ok := f.Errs[operation]; ok && err != nil {
		return capabilities.TaskResult{}, err
	}
	if key != "" {
		k := operation + "|" + key
		if !f.seenKEY[k] {
			f.seenKEY[k] = true
			f.Effects[operation]++
		}
	} else {
		f.Effects[operation]++
	}
	queue := f.Results[operation]
	var res capabilities.TaskResult
	if len(queue) == 0 {
		res = capabilities.TaskResult{TaskID: fmt.Sprintf("task-%s-%d", operation, f.Calls[operation]), State: capabilities.StateCompleted, Payload: json.RawMessage(`{"ok":true}`)}
	} else {
		res = queue[0]
		f.Results[operation] = queue[1:]
	}
	if res.TaskID == "" {
		res.TaskID = fmt.Sprintf("task-%s-%d", operation, f.Calls[operation])
	}
	if res.State == "" {
		res.State = capabilities.StateCompleted
	}
	if res.State == capabilities.StateWorking || res.State == capabilities.StateInputRequired {
		prog := append([]capabilities.TaskResult{res}, f.Next[res.TaskID]...)
		delete(f.Next, res.TaskID)
		f.Tasks[res.TaskID] = prog[1:]
		return prog[0], nil
	}
	return res, nil
}

// Poll implements capabilities.Capability.
func (f *ScriptedCapability) Poll(taskID string) (capabilities.TaskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prog, ok := f.Tasks[taskID]
	if !ok || len(prog) == 0 {
		return capabilities.TaskResult{TaskID: taskID, State: capabilities.StateCompleted, Payload: json.RawMessage(`{"ok":true}`)}, nil
	}
	next := prog[0]
	f.Tasks[taskID] = prog[1:]
	if next.TaskID == "" {
		next.TaskID = taskID
	}
	return next, nil
}

// Cancel implements capabilities.Capability.
func (f *ScriptedCapability) Cancel(taskID string) (capabilities.TaskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Cooperative cancel may not take effect: record the request and
	// report cancelled only when the scripted progression says so.
	if prog, ok := f.Tasks[taskID]; ok && len(prog) > 0 {
		next := prog[0]
		f.Tasks[taskID] = prog[1:]
		if next.TaskID == "" {
			next.TaskID = taskID
		}
		return next, nil
	}
	return capabilities.TaskResult{TaskID: taskID, State: capabilities.StateCancelled}, nil
}

// Update implements capabilities.Capability.
func (f *ScriptedCapability) Update(taskID string, _ json.RawMessage) (capabilities.TaskResult, error) {
	return f.Poll(taskID)
}

// InvokeContext adapts Invoke to a context-aware call for tests that
// thread cancellation through the harness pipeline.
func (f *ScriptedCapability) InvokeContext(_ context.Context, operation string, args json.RawMessage, key string) (capabilities.TaskResult, error) {
	return f.Invoke(operation, args, key)
}

// EffectCount returns first-effect executions for an operation.
func (f *ScriptedCapability) EffectCount(operation string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Effects[operation]
}

// CallCount returns Invoke calls for an operation.
func (f *ScriptedCapability) CallCount(operation string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls[operation]
}
