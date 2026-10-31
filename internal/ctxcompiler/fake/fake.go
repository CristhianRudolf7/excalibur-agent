package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ctxcompiler "github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
)

// FakeMemorySource serves canned memories up to the token budget.
type FakeMemorySource struct {
	Memories []ctxcompiler.MemoryHit
	Err      error

	Queries []string
	Budgets []int
}

// Recall implements ctxcompiler.MemorySource.
func (f *FakeMemorySource) Recall(_ context.Context, query string, _ []string, budget int) ([]ctxcompiler.MemoryHit, error) {
	f.Queries = append(f.Queries, query)
	f.Budgets = append(f.Budgets, budget)
	if f.Err != nil {
		return nil, f.Err
	}
	var out []ctxcompiler.MemoryHit
	total := 0
	for _, m := range f.Memories {
		cost := m.Tokens
		if cost <= 0 {
			cost = len(m.Text) / 4
		}
		if total+cost > budget {
			break
		}
		total += cost
		out = append(out, m)
	}
	return out, nil
}

// FakeSkillSource serves a canned catalog snippet and suggestions.
type FakeSkillSource struct {
	Catalog     string
	Suggestions []ctxcompiler.SkillSuggestion
	Err         error

	Roles   []string
	Queries []string
}

// CatalogSnippet implements ctxcompiler.SkillSource.
func (f *FakeSkillSource) CatalogSnippet(_ context.Context, role string, _ int) (string, error) {
	f.Roles = append(f.Roles, role)
	if f.Err != nil {
		return "", f.Err
	}
	return f.Catalog, nil
}

// Suggest implements ctxcompiler.SkillSource.
func (f *FakeSkillSource) Suggest(_ context.Context, task string, max int) ([]ctxcompiler.SkillSuggestion, error) {
	f.Queries = append(f.Queries, task)
	if f.Err != nil {
		return nil, f.Err
	}
	if len(f.Suggestions) > max {
		return append([]ctxcompiler.SkillSuggestion(nil), f.Suggestions[:max]...), nil
	}
	return append([]ctxcompiler.SkillSuggestion(nil), f.Suggestions...), nil
}

// FakeToolSource serves a canned core tool list.
type FakeToolSource struct {
	Tools []ctxcompiler.CoreTool
	Err   error

	Roles []string
}

// CoreTools implements ctxcompiler.ToolSource.
func (f *FakeToolSource) CoreTools(_ context.Context, role string) ([]ctxcompiler.CoreTool, error) {
	f.Roles = append(f.Roles, role)
	if f.Err != nil {
		return nil, f.Err
	}
	return append([]ctxcompiler.CoreTool(nil), f.Tools...), nil
}

// FakeSummarizer records its inputs and returns canned summaries.
// When Fail is set every call returns Fail. When Summaries run out
// it echoes the previous summary.
type FakeSummarizer struct {
	mu        sync.Mutex
	Summaries []string
	Fail      error

	Segments []string
	Prev     []string
}

// Summarize implements ctxcompiler.Summarizer.
func (f *FakeSummarizer) Summarize(_ context.Context, discarded, prev string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Segments = append(f.Segments, discarded)
	f.Prev = append(f.Prev, prev)
	if f.Fail != nil {
		return "", f.Fail
	}
	if len(f.Summaries) == 0 {
		return prev, nil
	}
	s := f.Summaries[0]
	f.Summaries = f.Summaries[1:]
	if prev != "" {
		return prev + "\n" + s, nil
	}
	return s, nil
}

// ErrNotFound is returned by MemContentStore for unknown call IDs.
var ErrNotFound = fmt.Errorf("fake: content not found")

// MemContentStore is an in-memory ctxcompiler.ContentStore: exact
// bytes in, exact bytes out.
type MemContentStore struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMemContentStore returns an empty MemContentStore.
func NewMemContentStore() *MemContentStore {
	return &MemContentStore{data: make(map[string]string)}
}

// Store implements ctxcompiler.ContentStore.
func (m *MemContentStore) Store(callID, output string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[callID] = output
}

// Load implements ctxcompiler.ContentStore.
func (m *MemContentStore) Load(callID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out, ok := m.data[callID]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotFound, callID)
	}
	return out, nil
}

// MemHistoryStore is an in-memory ctxcompiler.HistoryStore with
// substring search, newest first.
type MemHistoryStore struct {
	mu       sync.RWMutex
	turns    []ctxcompiler.ActivityEvent
	Searches []string
}

// Append implements ctxcompiler.HistoryStore.
func (m *MemHistoryStore) Append(turns []ctxcompiler.ActivityEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns = append(m.turns, turns...)
}

// Search implements ctxcompiler.HistoryStore.
func (m *MemHistoryStore) Search(query string) []ctxcompiler.ActivityEvent {
	m.mu.Lock()
	m.Searches = append(m.Searches, query)
	cp := append([]ctxcompiler.ActivityEvent(nil), m.turns...)
	m.mu.Unlock()
	var out []ctxcompiler.ActivityEvent
	for i := len(cp) - 1; i >= 0; i-- {
		if query == "" || strings.Contains(cp[i].Content, query) {
			out = append(out, cp[i])
		}
	}
	return out
}

// Stored returns every archived turn in append order.
func (m *MemHistoryStore) Stored() []ctxcompiler.ActivityEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]ctxcompiler.ActivityEvent(nil), m.turns...)
}
