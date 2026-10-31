package master

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// FakeControls is a scriptable ControlRunner for fast-lane and master
// tests.
type FakeControls struct {
	mu       sync.Mutex
	Runs     map[string]api.RunStatusResult
	ByName   map[string][]string
	Paused   []string
	Resumed  []string
	Canceled []string
	All      []string
	ScopeSet []string
}

func newFakeControls() *FakeControls {
	return &FakeControls{Runs: map[string]api.RunStatusResult{}, ByName: map[string][]string{}}
}

func (f *FakeControls) seed(runID, agent string, state types.RunState) {
	f.Runs[runID] = api.RunStatusResult{RunID: runID, State: state, ToolInFlight: agent}
	f.ByName[agent] = append(f.ByName[agent], runID)
}

func (f *FakeControls) Snapshot(runID string) (api.RunStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.Runs[runID]
	if !ok {
		return api.RunStatusResult{}, fmt.Errorf("unknown run %q", runID)
	}
	return s, nil
}

func (f *FakeControls) ListRuns() []api.RunStatusResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.RunStatusResult
	for _, s := range f.Runs {
		out = append(out, s)
	}
	return out
}

func (f *FakeControls) FindRunsByAgentName(name string) []api.RunStatusResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.RunStatusResult
	for _, id := range f.ByName[name] {
		if s, ok := f.Runs[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func (f *FakeControls) Pause(runID string) (api.RunStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.Runs[runID]
	if !ok {
		return api.RunStatusResult{}, fmt.Errorf("unknown run %q", runID)
	}
	f.Paused = append(f.Paused, runID)
	return s, nil
}

func (f *FakeControls) Resume(runID string) (api.RunStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.Runs[runID]
	if !ok {
		return api.RunStatusResult{}, fmt.Errorf("unknown run %q", runID)
	}
	f.Resumed = append(f.Resumed, runID)
	return s, nil
}

func (f *FakeControls) Cancel(runID string) (api.RunStatusResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.Runs[runID]
	if !ok {
		return api.RunStatusResult{}, fmt.Errorf("unknown run %q", runID)
	}
	f.Canceled = append(f.Canceled, runID)
	return s, nil
}

func (f *FakeControls) Interrupt(runID string, _ bool) (api.RunStatusResult, error) {
	return f.Pause(runID)
}

func (f *FakeControls) Redirect(runID, _ string, _ bool) (api.RunStatusResult, error) {
	return f.Pause(runID)
}

func (f *FakeControls) PauseAll() []api.RunStatusResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []api.RunStatusResult
	for id, s := range f.Runs {
		f.All = append(f.All, id)
		out = append(out, s)
	}
	return out
}

func (f *FakeControls) AnswerQuestion(_, _ string) error { return nil }
func (f *FakeControls) ApprovePlan(_ string) error       { return nil }
func (f *FakeControls) DenyPlan(_, _ string) error       { return nil }
func (f *FakeControls) SetScope(_, scope string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ScopeSet = append(f.ScopeSet, scope)
	return nil
}

var _ = context.Background
var _ = testing.T{}
