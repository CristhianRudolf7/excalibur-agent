// Package fake holds the test double for the skill index: other
// milestones depend on the Catalog interface and use the fake in
// their tests instead of a populated index.
package fake

import (
	"fmt"
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
)

// Catalog is the skill surface other packages consume: search,
// suggest, load and the grouped L1 catalog. *skills.Index
// implements it; FakeCatalog is the test double.
type Catalog interface {
	Search(query, group string) []skills.Hit
	Suggest(query, model string, max int) []skills.Suggestion
	Load(name string) (skills.Loaded, error)
	RenderCatalog(cfg skills.CatalogConfig) string
}

// FakeCatalog is an in-memory Catalog for tests.
type FakeCatalog struct {
	Hits        []skills.Hit
	Suggestions []skills.Suggestion
	Loaded      map[string]skills.Loaded
	CatalogText string
	SearchCalls [][2]string
	LoadCalls   []string
}

// Search implements Catalog.
func (f *FakeCatalog) Search(query, group string) []skills.Hit {
	f.SearchCalls = append(f.SearchCalls, [2]string{query, group})
	out := make([]skills.Hit, 0, len(f.Hits))
	for _, h := range f.Hits {
		if group != "" && h.Group != group {
			continue
		}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Suggest implements Catalog.
func (f *FakeCatalog) Suggest(_, _ string, _ int) []skills.Suggestion {
	return append([]skills.Suggestion(nil), f.Suggestions...)
}

// Load implements Catalog.
func (f *FakeCatalog) Load(name string) (skills.Loaded, error) {
	f.LoadCalls = append(f.LoadCalls, name)
	if l, ok := f.Loaded[name]; ok {
		return l, nil
	}
	return skills.Loaded{}, fmt.Errorf("fake: unknown skill %q", name)
}

// RenderCatalog implements Catalog.
func (f *FakeCatalog) RenderCatalog(_ skills.CatalogConfig) string {
	return f.CatalogText
}
