// Scripted decider for tests and for deterministic wiring checks.
package models

import (
	"context"
	"fmt"
	"sync"
)

// FakeDecider answers scripted probabilities in order. It never
// touches the network and is the seam the reception (Section 6) and
// memory admission (Section 10.5) tests use.
type FakeDecider struct {
	mu    sync.Mutex
	probs []float64
	calls []Question
}

// NewFakeDecider returns a decider answering probs in order,
// repeating the last one when questions outnumber probs.
func NewFakeDecider(probs ...float64) *FakeDecider {
	return &FakeDecider{probs: append([]float64(nil), probs...)}
}

// Ask records the question and answers the scripted probability for
// its kind.
func (f *FakeDecider) Ask(_ context.Context, q Question) (Answer, error) {
	if err := q.Validate(); err != nil {
		return Answer{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, q)
	p := 0.5
	if len(f.probs) > 0 {
		i := len(f.calls) - 1
		if i >= len(f.probs) {
			i = len(f.probs) - 1
		}
		p = f.probs[i]
	}
	ans := Answer{Kind: q.Kind, Prob: p}
	switch q.Kind {
	case KindChoice:
		if len(q.Options) > 0 {
			ans.Choice = q.Options[0]
		}
	case KindScore:
		ans.Score = len(q.Options) - 1
	}
	return ans.Normalize(q.Kind, q.Options)
}

// Close releases nothing.
func (f *FakeDecider) Close() error { return nil }

// Calls returns the questions asked so far.
func (f *FakeDecider) Calls() []Question {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Question(nil), f.calls...)
}

// CallsCount returns the number of questions asked.
func (f *FakeDecider) CallsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var _ Decider = (*FakeDecider)(nil)
var _ Decider = (*LLMDecider)(nil)
var _ Decider = (*HTTPDecider)(nil)

// MustAsk is a test helper that fails the test on error.
func MustAsk(t interface {
	Helper()
	Fatalf(string, ...any)
}, d Decider, ctx context.Context, q Question) Answer {
	t.Helper()
	ans, err := d.Ask(ctx, q)
	if err != nil {
		t.Fatalf("decider ask: %v", err)
	}
	return ans
}

// Ensure FakeDecider errors surface with context.
var _ = fmt.Errorf
