package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// DeferredCatalog tracks which catalog entries are deferred (Section
// 13.3). Core tools stay in the model prefix in fixed order; deferred
// tools never enter the prefix and resolve through search_tools from
// the next step, so adding or removing them never breaks the input
// cache (Sections 11.6, 13.3, 14.12).
type DeferredCatalog struct {
	mu       sync.RWMutex
	reg      Registry
	core     map[string]bool
	deferred map[string]bool
}

// NewDeferredCatalog captures the current registry names as the core
// prefix. Later registrations via RegisterDeferred are deferred.
func NewDeferredCatalog(reg Registry) *DeferredCatalog {
	c := &DeferredCatalog{reg: reg, core: map[string]bool{}, deferred: map[string]bool{}}
	for _, n := range reg.Names() {
		c.core[n] = true
	}
	return c
}

// NewDeferredCatalogWithCore builds a catalog with an explicit core set.
// Names not in core but present in the registry count as deferred.
func NewDeferredCatalogWithCore(reg Registry, core []string) *DeferredCatalog {
	c := &DeferredCatalog{reg: reg, core: map[string]bool{}, deferred: map[string]bool{}}
	for _, n := range core {
		c.core[n] = true
	}
	for _, n := range reg.Names() {
		if !c.core[n] {
			c.deferred[n] = true
		}
	}
	return c
}

// RegisterDeferred adds a tool as deferred. It validates through the
// same Registry as built-ins: there is no privileged path.
func (c *DeferredCatalog) RegisterDeferred(t Tool) error {
	if err := c.reg.Register(t); err != nil {
		return err
	}
	def := t.Definition()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deferred[def.Name] = true
	return nil
}

// MarkDeferred records an already-registered name as deferred without
// re-registering it (for tools wired onto core stubs).
func (c *DeferredCatalog) MarkDeferred(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.core[name] {
		return
	}
	c.deferred[name] = true
}

// UnmarkDeferred drops the deferred mark of a name. It exists for
// the extension kit (Section 14.12): retiring an extension removes
// its entries from search_tools. Core marks are never touched.
func (c *DeferredCatalog) UnmarkDeferred(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.deferred, name)
}

// IsDeferred reports whether a name resolves as a deferred tool.
func (c *DeferredCatalog) IsDeferred(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deferred[name]
}

// IsCore reports whether a name belongs to the fixed prefix.
func (c *DeferredCatalog) IsCore(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.core[name]
}

// CoreNames returns the fixed prefix names in sorted order.
func (c *DeferredCatalog) CoreNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.core))
	for n := range c.core {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DeferredNames returns deferred names in sorted order.
func (c *DeferredCatalog) DeferredNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.deferred))
	for n := range c.deferred {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// PrefixHash hashes the core prefix definitions only. Deferred additions
// never change it (Sections 11.6, 13.3).
func (c *DeferredCatalog) PrefixHash() string {
	return PrefixHash(c.coreDefinitions())
}

func (c *DeferredCatalog) coreDefinitions() []Definition {
	c.mu.RLock()
	names := make([]string, 0, len(c.core))
	for n := range c.core {
		names = append(names, n)
	}
	c.mu.RUnlock()
	sort.Strings(names)
	defs := make([]Definition, 0, len(names))
	for _, n := range names {
		t, err := c.reg.Get(n)
		if err != nil {
			continue
		}
		defs = append(defs, t.Definition())
	}
	return defs
}

// PrefixHash hashes core definitions deterministically.
func PrefixHash(defs []Definition) string {
	cp := append([]Definition(nil), defs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Name < cp[j].Name })
	h := sha256.New()
	for _, d := range cp {
		fmt.Fprintf(h, "name=%s\ndesc=%s\nschema=%s\neffect=%s\nidem=%s\ncancel=%s\nfast=%v\n",
			d.Name, d.Description, string(d.InputSchema), d.Effect, d.Idempotency, d.Cancel, d.Fast)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ResolveSkillTools pre-resolves the deferred tools a skill declares
// (Section 13.3: loading a skill resolves its tools up front). It
// verifies every named tool exists as deferred and returns their
// definitions in sorted order.
func (c *DeferredCatalog) ResolveSkillTools(toolNames []string) ([]Definition, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Definition, 0, len(toolNames))
	for _, n := range toolNames {
		if !c.deferred[n] {
			return nil, fmt.Errorf("tools: skill tool %q is not a deferred tool", n)
		}
		t, err := c.reg.Get(n)
		if err != nil {
			return nil, fmt.Errorf("tools: skill tool %q: %w", n, err)
		}
		out = append(out, t.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ScoredTool is one search_tools hit with its retrieval score.
type ScoredTool struct {
	Definition Definition `json:"definition"`
	Score      float64    `json:"score"`
}

// Search ranks deferred tools for a query with a deterministic
// lexical BM25-style score over name, description, tags, keywords and
// group (Section 13.3: BM25 + vectors). The harness ships the lexical
// half; vector re-ranking arrives with the memory embeddings (10.8)
// and only reorders within the same deterministic tie-break.
func (c *DeferredCatalog) Search(query string, k int) []ScoredTool {
	if k <= 0 {
		k = 5
	}
	if k > 5 {
		k = 5
	}
	q := tokenize(query)
	if len(q) == 0 {
		return nil
	}
	c.mu.RLock()
	names := make([]string, 0, len(c.deferred))
	for n := range c.deferred {
		names = append(names, n)
	}
	c.mu.RUnlock()
	sort.Strings(names)

	type doc struct {
		def    Definition
		tokens map[string]int
		length int
	}
	docs := make([]doc, 0, len(names))
	df := map[string]int{}
	for _, n := range names {
		t, err := c.reg.Get(n)
		if err != nil {
			continue
		}
		def := t.Definition()
		text := def.Name + " " + def.Description + " " + strings.Join(def.Tags, " ") +
			" " + strings.Join(def.Keywords, " ") + " " + def.Group
		toks := tokenize(text)
		counts := map[string]int{}
		for _, tok := range toks {
			counts[tok]++
		}
		docs = append(docs, doc{def: def, tokens: counts, length: len(toks)})
		for tok := range counts {
			df[tok]++
		}
	}
	nDocs := float64(len(docs))
	if nDocs == 0 {
		return nil
	}
	var sumLen int
	for _, d := range docs {
		sumLen += d.length
	}
	avgLen := float64(sumLen) / nDocs
	if avgLen < 1 {
		avgLen = 1
	}

	const k1 = 1.2
	const b = 0.75
	scored := make([]ScoredTool, 0, len(docs))
	for _, d := range docs {
		score := 0.0
		nameLower := strings.ToLower(d.def.Name)
		descLower := strings.ToLower(d.def.Description)
		qLower := strings.ToLower(strings.Join(q, " "))
		if nameLower == qLower {
			score += 20
		}
		if strings.Contains(nameLower, qLower) && qLower != "" {
			score += 8
		}
		// Tag/keyword/group exact hits outrank description prose.
		qset := map[string]bool{}
		for _, tok := range q {
			qset[tok] = true
		}
		for _, tag := range d.def.Tags {
			if qset[strings.ToLower(tag)] {
				score += 6
			}
		}
		for _, kw := range d.def.Keywords {
			if qset[strings.ToLower(kw)] {
				score += 6
			}
		}
		if qset[strings.ToLower(d.def.Group)] {
			score += 4
		}
		if strings.Contains(descLower, qLower) && len(q) == 1 {
			score += 2
		}
		for _, tok := range q {
			tf := float64(d.tokens[tok])
			if tf == 0 {
				continue
			}
			idf := math.Log(1 + (nDocs-float64(df[tok])+0.5)/(float64(df[tok])+0.5))
			den := tf + k1*(1-b+b*float64(d.length)/avgLen)
			score += idf * (tf * (k1 + 1)) / den
		}
		scored = append(scored, ScoredTool{Definition: d.def, Score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Definition.Name < scored[j].Definition.Name
		}
		return scored[i].Score > scored[j].Score
	})
	// Drop zero-score tails so unrelated tools never fill the top 5.
	kept := scored[:0]
	for _, s := range scored {
		if s.Score > 0 {
			kept = append(kept, s)
		}
	}
	scored = kept
	if len(scored) > k {
		scored = scored[:k]
	}
	return scored
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, "_")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

var searchToolsSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"query": map[string]any{"type": "string", "description": "Verb object domain, e.g. 'merge pdf pages'; empty lists the deferred tools with one-line descriptions"},
		"k":     map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
	},
	"required":             []string{"query"},
	"additionalProperties": false,
})

var callToolSchema = schemaFor(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"name":      map[string]any{"type": "string", "minLength": 1},
		"arguments": map[string]any{"type": "object"},
	},
	"required":             []string{"name"},
	"additionalProperties": false,
})

// SearchToolsTool implements search_tools over the deferred catalog.
type SearchToolsTool struct {
	Catalog *DeferredCatalog
}

func (t *SearchToolsTool) Definition() Definition {
	return Definition{
		Name: "search_tools", Description: "Search deferred tools by verb, object and domain and return the 5 most relevant schemas",
		InputSchema: searchToolsSchema, Effect: "read_only", Idempotency: "pure",
		Cancel: "immediate", Timeout: 30_000_000_000, OutputBudget: DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "skills", Tags: []string{"tools"}, Keywords: []string{"search", "tools", "deferred", "catalog"},
	}
}

func (t *SearchToolsTool) Invoke(_ context.Context, inv Invocation) (Result, error) {
	var a struct {
		Query string `json:"query"`
		K     int    `json:"k"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	cat := t.Catalog
	if cat == nil {
		return Result{Status: StatusToolError, Output: "deferred catalog not wired"}, nil
	}
	if strings.TrimSpace(a.Query) == "" {
		if StubRepliesEnabled() {
			return Result{Status: StatusOK, Output: deferredCatalogueText(cat)}, nil
		}
		return Result{Status: StatusOK, Output: popularDeferred(cat, a.K)}, nil
	}
	hits := cat.Search(a.Query, a.K)
	if len(hits) == 0 {
		return Result{Status: StatusOK, Output: "no deferred tools match the query"}, nil
	}
	type schemaOut struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
		Score       float64         `json:"score"`
	}
	out := make([]schemaOut, 0, len(hits))
	for _, h := range hits {
		out = append(out, schemaOut{Name: h.Definition.Name, Description: h.Definition.Description, InputSchema: h.Definition.InputSchema, Score: h.Score})
	}
	raw, err := json.MarshalIndent(map[string]any{"tools": out}, "", "  ")
	if err != nil {
		return Result{Status: StatusToolError, Output: err.Error()}, nil
	}
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

// deferredCatalogueText lists every deferred tool with its one-line
// purpose, for an empty search_tools query. Schemas come from a query
// that names a verb, object and domain.
func deferredCatalogueText(cat *DeferredCatalog) string {
	var b strings.Builder
	b.WriteString("deferred tools (name: one-line purpose); search with a verb-object-domain query to get a tool's schema\n")
	for _, n := range cat.DeferredNames() {
		t, err := cat.reg.Get(n)
		if err != nil {
			continue
		}
		def := t.Definition()
		fmt.Fprintf(&b, "- %s: %s\n", def.Name, clipText(strings.TrimSpace(strings.SplitN(def.Description, "\n", 2)[0]), 120))
	}
	return b.String()
}

// popularDeferred lists the first deferred tools alphabetically when
// the model calls search_tools with an empty query (exploration)
// instead of failing: a small model can pick from the list and refine
// with verbs on the next call.
func popularDeferred(cat *DeferredCatalog, k int) string {
	if k <= 0 || k > 5 {
		k = 5
	}
	names := cat.DeferredNames()
	if len(names) > k {
		names = names[:k]
	}
	type schemaOut struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	out := make([]schemaOut, 0, len(names))
	for _, n := range names {
		t, err := cat.reg.Get(n)
		if err != nil {
			continue
		}
		def := t.Definition()
		out = append(out, schemaOut{Name: def.Name, Description: def.Description, InputSchema: def.InputSchema})
	}
	raw, _ := json.MarshalIndent(map[string]any{
		"note":  "empty query: showing deferred tools alphabetically; refine with a verb-object-domain query",
		"tools": out,
	}, "", "  ")
	return string(raw)
}

func (t *SearchToolsTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return nil, nil
}

// CallToolTool implements call_tool: validated invocation of one
// registered tool through the standard pipeline (Section 13.4).
type CallToolTool struct {
	Catalog  *DeferredCatalog
	Executor *Executor
}

func (t *CallToolTool) Definition() Definition {
	return Definition{
		Name: "call_tool", Description: "Invoke a tool by name with validated arguments, e.g. {\"name\":\"lsp\",\"arguments\":{\"path\":\"x\"}}; the name may also sit inside arguments",
		InputSchema: callToolSchema, Effect: "exec", Idempotency: "keyed",
		Cancel: "cooperative", Timeout: 120_000_000_000, OutputBudget: DefaultOutputBudget, Fast: false,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "skills", Tags: []string{"tools"}, Keywords: []string{"call", "tool", "deferred", "invoke"},
	}
}

func (t *CallToolTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	var a struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Name) == "" && callNameFlex(t) {
		a.Name, a.Arguments = promoteInvokedName(a.Arguments)
	}
	if a.Name == "" {
		return Result{Status: StatusInputError, Output: "name is required: call_tool {\"name\": \"<tool>\", \"arguments\": {...}}; the name may also sit inside arguments"}, nil
	}
	if a.Name == "call_tool" || (a.Name == "search_tools" && !FrictionRepairEnabled()) {
		return Result{Status: StatusInputError, Output: "call_tool cannot invoke itself or search_tools"}, nil
	}
	if t.Catalog != nil && !t.Catalog.IsDeferred(a.Name) && !FrictionRepairEnabled() {
		return Result{Status: StatusInputError, Output: fmt.Sprintf("tool %q is not deferred; call it directly", a.Name)}, nil
	}
	args := a.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if t.Executor == nil {
		return Result{Status: StatusToolError, Output: "tool executor not wired"}, nil
	}
	return t.Executor.Execute(ctx, a.Name, args, ExecuteOptions{IdempotencyKey: inv.IdempotencyKey})
}

func (t *CallToolTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	return []string{"tool:" + a.Name}, nil
}

// callNameFlex reports whether the invoked call_tool tolerates a
// nested or wrapped tool name: on unless the executor carries an
// explicit tuning that disables CallNameFlex. A nil executor (unit
// tests, direct invocation) keeps the tolerant default, matching
// the pipeline default.
func callNameFlex(t *CallToolTool) bool {
	if t == nil || t.Executor == nil || t.Executor.Tune == nil {
		return true
	}
	return t.Executor.Tune.CallNameFlex
}

// promoteInvokedName recovers the deferred tool name from inside the
// arguments object (name/tool/tool_name keys, case-insensitive) for
// direct invocations that bypass NormalizeArgs. It returns the name
// ("" when absent) with the consumed key removed from the arguments.
func promoteInvokedName(args json.RawMessage) (string, json.RawMessage) {
	var sub map[string]any
	if err := json.Unmarshal(args, &sub); err != nil || sub == nil {
		return "", args
	}
	for k, v := range sub {
		switch strings.ToLower(k) {
		case "name", "tool", "tool_name":
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				delete(sub, k)
				if FrictionRepairEnabled() && len(sub) == 1 {
					if nested, ok := sub["arguments"].(map[string]any); ok {
						sub = nested
					}
				}
				raw, _ := json.Marshal(sub)
				if raw == nil {
					raw = json.RawMessage(`{}`)
				}
				return strings.ToLower(strings.TrimSpace(s)), raw
			}
		}
	}
	return "", args
}

// WireSearchCall connects the real search_tools/call_tool providers to
// the core stubs registered by RegisterBuiltins, or registers them
// when the registry lacks them. The catalog prefix (core names and
// hash) never changes: wiring only fills providers.
func WireSearchCall(reg *MapRegistry, cat *DeferredCatalog, exec *Executor) {
	search := &SearchToolsTool{Catalog: cat}
	call := &CallToolTool{Catalog: cat, Executor: exec}
	for _, real := range []Tool{search, call} {
		existing, err := reg.Get(real.Definition().Name)
		if err != nil {
			_ = reg.Register(real)
			continue
		}
		if stub, ok := existing.(*StubTool); ok {
			provider := real
			stub.Provider = func(ctx context.Context, inv Invocation) (Result, error) {
				return provider.Invoke(ctx, inv)
			}
		}
	}
}
