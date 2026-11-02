package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// Tool names owned by the memory milestone (Section 10). Seven match
// the H3 core stubs one-to-one; library_ingest is new in H11
// (Section 10.9 step 4 names the ingest call while the H3 stub list
// predates it).
const (
	ToolMemoryWrite   = "memory_write"
	ToolMemoryPropose = "memory_propose"
	ToolMemorySearch  = "memory_search"
	ToolHistorySearch = "history_search"
	ToolLibraryIngest = "library_ingest"
	ToolLibrarySearch = "library_search"
	ToolDocRead       = "doc_read"
	ToolDocEdit       = "doc_edit"
)

func schemaFor(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

var (
	memoryWriteSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"statement":    map[string]any{"type": "string", "minLength": 1},
			"scope":        strProp("Scope id, e.g. general or project:abc"),
			"kind":         map[string]any{"type": "string", "enum": []string{"fact", "preference", "event", "commitment", "belief", "relation", "procedure"}},
			"entities":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"valid_from":   strProp("RFC3339 or epoch ms; when the fact was true"),
			"valid_to":     strProp("RFC3339 or epoch ms; until when it was true"),
			"expires_at":   strProp("RFC3339 or epoch ms; optional expiry"),
			"confidence":   map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"importance":   map[string]any{"type": "number", "minimum": 0, "maximum": 1},
			"sources":      map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"trust":        map[string]any{"type": "string", "enum": []string{"user", "agent", "external"}},
			"conversation": strProp("Proposing conversation for candidate visibility"),
		},
		"required":             []string{"statement", "scope"},
		"additionalProperties": false,
	})
	memorySearchSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":        map[string]any{"type": "string", "minLength": 1},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1},
			"kinds":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"from":         strProp("Validity window start, RFC3339 or epoch ms"),
			"to":           strProp("Validity window end, RFC3339 or epoch ms"),
			"known_at":     strProp("Record-time bound for 'what did we know at T', RFC3339 or epoch ms"),
			"entities":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"k":            map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
			"historical":   map[string]any{"type": "boolean"},
			"conversation": strProp("Include candidates proposed in this conversation"),
			"max_tokens":   map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	})
	historySearchSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text":         map[string]any{"type": "string"},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"conversation": map[string]any{"type": "string"},
			"from":         map[string]any{"type": "integer"},
			"to":           map[string]any{"type": "integer"},
			"tool":         map[string]any{"type": "string"},
			"state":        map[string]any{"type": "string"},
			"run":          map[string]any{"type": "string"},
			"k":            map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		},
		"additionalProperties": false,
	})
	libraryIngestSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":        strProp("Local document path copied to the blob store"),
			"blob_digest": strProp("SHA-256 digest of an uploaded blob"),
			"name":        strProp("Original file name"),
			"scope":       map[string]any{"type": "string", "minLength": 1},
			"title":       map[string]any{"type": "string", "minLength": 1},
			"mime":        map[string]any{"type": "string", "minLength": 1},
			"origin":      map[string]any{"type": "string"},
			"text":        map[string]any{"type": "string"},
			"pages":       map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			"paginated":   map[string]any{"type": "boolean"},
			"library":     map[string]any{"type": "boolean"},
		},
		"required":             []string{},
		"additionalProperties": false,
	})
	librarySearchSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":        map[string]any{"type": "string", "minLength": 1},
			"scopes":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"run_id":       map[string]any{"type": "string"},
			"document_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 100},
			"k":            map[string]any{"type": "integer", "minimum": 1, "maximum": 50},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	})
	docReadSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope":    map[string]any{"type": "string"},
			"doc":      map[string]any{"type": "string", "minLength": 1},
			"selector": map[string]any{"type": "string"},
		},
		"required":             []string{"doc"},
		"additionalProperties": false,
	})
	docEditSchema = schemaFor(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scope":         map[string]any{"type": "string"},
			"doc":           map[string]any{"type": "string", "minLength": 1},
			"fragment_id":   map[string]any{"type": "string", "minLength": 1},
			"expected_hash": map[string]any{"type": "string", "minLength": 1},
			"op":            map[string]any{"type": "string", "enum": []string{"replace", "insert", "delete"}},
			"text":          map[string]any{"type": "string"},
			"heading_path":  map[string]any{"type": "string"},
			"position":      map[string]any{"type": "integer", "minimum": 0},
			"provenance":    map[string]any{"type": "string"},
			"explicit":      map[string]any{"type": "boolean"},
		},
		"required":             []string{"doc", "fragment_id", "expected_hash", "op"},
		"additionalProperties": false,
	})
)

// ---- tool structs ----

type memoryWriteTool struct{ svc *Service }

func (t *memoryWriteTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolMemoryWrite,
		Description: "Store a fact, preference or event with admission: dedupes, refines, disputes or replaces",
		InputSchema: memoryWriteSchema,
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemKeyed, Cancel: types.CancelAfterCompletion,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"memory"}, Keywords: []string{"memory", "write", "remember"}}
}

func (t *memoryWriteTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	c, err := parseCandidate(inv.Arguments)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	m, outcome, err := t.svc.Write(ctx, c)
	if err != nil {
		return toolErr(err)
	}
	return ok(map[string]any{"action": outcome.Action, "memory_id": m.ID, "short_code": m.ShortCode, "status": string(m.Status)})
}

func (t *memoryWriteTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type memoryProposeTool struct{ svc *Service }

func (t *memoryProposeTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolMemoryPropose,
		Description: "Propose a candidate fact; stays pending for consolidation in its own conversation or task",
		InputSchema: memoryWriteSchema,
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemIdempotent, Cancel: types.CancelAfterCompletion,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"memory"}, Keywords: []string{"memory", "propose", "fact"}}
}

func (t *memoryProposeTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	c, err := parseCandidate(inv.Arguments)
	if err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	m, err := t.svc.Propose(ctx, c)
	if err != nil {
		return toolErr(err)
	}
	return ok(map[string]any{"action": ActionProposed, "memory_id": m.ID, "status": string(m.Status)})
}

func (t *memoryProposeTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type memorySearchTool struct{ svc *Service }

func (t *memorySearchTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolMemorySearch,
		Description: "Retrieve memories by lexical, semantic, temporal and relational search with uncertainty notices",
		InputSchema: memorySearchSchema,
		Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"memory"}, Keywords: []string{"memory", "search", "recall"}}
}

func (t *memorySearchTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Query        string   `json:"query"`
		Scopes       []string `json:"scopes"`
		Kinds        []string `json:"kinds"`
		From         any      `json:"from"`
		To           any      `json:"to"`
		Entities     []string `json:"entities"`
		K            int      `json:"k"`
		Historical   bool     `json:"historical"`
		KnownAt      any      `json:"known_at"`
		Conversation string   `json:"conversation"`
		MaxTokens    int      `json:"max_tokens"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "query is required"}, nil
	}
	q := Query{Text: a.Query, Scopes: a.Scopes, Entities: a.Entities, K: a.K, Historical: a.Historical, Conversation: a.Conversation, MaxTokens: a.MaxTokens}
	for _, k := range a.Kinds {
		q.Kinds = append(q.Kinds, Kind(k))
	}
	var err error
	if q.From, err = parseTime(a.From); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: "from: " + err.Error()}, nil
	}
	if q.To, err = parseTime(a.To); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: "to: " + err.Error()}, nil
	}
	if q.KnownAt, err = parseTime(a.KnownAt); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: "known_at: " + err.Error()}, nil
	}
	ret, err := t.svc.Retrieve(ctx, q)
	if err != nil {
		return toolErr(err)
	}
	return ok(ret)
}

func (t *memorySearchTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type historySearchTool struct{ svc *Service }

func (t *historySearchTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolHistorySearch,
		Description: "Search past messages and actions by text, date, conversation, tool or state",
		InputSchema: historySearchSchema,
		Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"history"}, Keywords: []string{"history", "search", "actions"}}
}

func (t *historySearchTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Text         string   `json:"text"`
		Scopes       []string `json:"scopes"`
		Conversation string   `json:"conversation"`
		From         int64    `json:"from"`
		To           int64    `json:"to"`
		Tool         string   `json:"tool"`
		State        string   `json:"state"`
		Run          string   `json:"run"`
		K            int      `json:"k"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	items, err := t.svc.History(ctx, a.Scopes, HistoryQuery{
		Text: a.Text, Conversation: a.Conversation, From: a.From, To: a.To,
		Tool: a.Tool, State: a.State, Run: a.Run, K: a.K,
	})
	if err != nil {
		return toolErr(err)
	}
	return ok(map[string]any{"items": items})
}

func (t *historySearchTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type libraryIngestTool struct{ svc *Service }

func (t *libraryIngestTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolLibraryIngest,
		Description: "Ingest a document into the user library with versioned original and citable fragments",
		InputSchema: libraryIngestSchema,
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemKeyed, Cancel: types.CancelAfterCompletion,
		Timeout: 120_000_000_000, OutputBudget: tools.DefaultOutputBudget,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "memory", Tags: []string{"library"}, Keywords: []string{"library", "ingest", "document"}}
}

func (t *libraryIngestTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	if t.svc == nil {
		return tools.Result{Status: tools.StatusInputError, Output: "library memory service is not wired"}, nil
	}
	var a struct {
		Path       string     `json:"path"`
		BlobDigest string     `json:"blob_digest"`
		Name       string     `json:"name"`
		Scope      string     `json:"scope"`
		Title      string     `json:"title"`
		MIME       string     `json:"mime"`
		Origin     string     `json:"origin"`
		Text       string     `json:"text"`
		Pages      [][]string `json:"pages"`
		Paginated  bool       `json:"paginated"`
		Library    bool       `json:"library"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}

	if a.Scope == "" && len(t.svc.scopes) == 1 {
		a.Scope = t.svc.scopes[0]
	}
	if _, err := t.svc.resolveScopes([]string{a.Scope}); err != nil {
		return toolErr(err)
	}
	if a.Path != "" || a.BlobDigest != "" {
		if a.Text != "" || len(a.Pages) > 0 {
			return tools.Result{Status: tools.StatusInputError, Output: "text/pages cannot be combined with path or blob_digest"}, nil
		}
		binding := t.svc.librarySubmitter.Load()
		if binding == nil || binding.submitter == nil {
			return tools.Result{Status: tools.StatusInputError, Output: "background library ingester is not wired"}, nil
		}
		doc, existing, err := binding.submitter.SubmitLibrary(ctx, a.Scope, DocMeta{Title: a.Title, SourceName: a.Name, MIME: a.MIME, Origin: a.Origin, BlobDigest: a.BlobDigest}, a.Path)
		if err != nil {
			return toolErr(err)
		}
		return ok(map[string]any{"document": doc, "existing": existing, "document_id": doc.ID, "version": doc.Version})
	}
	if a.Scope == "" || a.Title == "" || a.MIME == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "scope, title and mime are required"}, nil
	}
	doc, err := t.svc.Ingest(ctx, []string{a.Scope}, a.Title, a.MIME, []byte(a.Text),
		IngestOptions{Title: a.Title, Origin: a.Origin, Pages: a.Pages, Paginated: a.Paginated, Library: a.Library})
	if err != nil {
		return toolErr(err)
	}
	return ok(map[string]any{"document_id": doc.ID, "version": doc.Version})
}

func (t *libraryIngestTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	keys, err := scopeKeys(args)
	if err != nil || len(keys) > 0 {
		return keys, err
	}
	if t.svc != nil && len(t.svc.scopes) == 1 {
		return []string{"scope:" + t.svc.scopes[0]}, nil
	}
	return []string{"library:ingest"}, nil
}

type librarySearchTool struct{ svc *Service }

func (t *librarySearchTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolLibrarySearch,
		Description: "Search the user document library and return citable fragments with page and paragraph",
		InputSchema: librarySearchSchema,
		Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate"},
		Group: "memory", Tags: []string{"library"}, Keywords: []string{"library", "search", "documents"}}
}

func (t *librarySearchTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	if !libraryEnabled() {
		return toolErr(&inputError{"memory: library disabled"})
	}
	if t.svc == nil {
		return toolErr(fmt.Errorf("memory: library service not wired"))
	}
	var a struct {
		DocumentIDs []string `json:"document_ids"`
		Query       string   `json:"query"`
		Scopes      []string `json:"scopes"`
		RunID       string   `json:"run_id"`
		K           int      `json:"k"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if strings.TrimSpace(a.Query) == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "query is required"}, nil
	}
	hits, semantic, err := t.svc.LibrarySearch(ctx, t.svc.libraryDefaultScopes(a.Scopes), a.RunID, a.Query, a.K, a.DocumentIDs...)
	if err != nil {
		return toolErr(err)
	}
	type wireHit struct {
		Text     string      `json:"text"`
		Context  ExpandedHit `json:"context"`
		Ref      string      `json:"ref"`
		Citation string      `json:"citation"`
		Score    float64     `json:"score"`
	}
	out := make([]wireHit, 0, len(hits))
	for _, h := range hits {
		expanded, err := t.svc.ExpandHit(ctx, t.svc.libraryDefaultScopes(a.Scopes), h)
		if err != nil {
			return toolErr(err)
		}
		out = append(out, wireHit{Text: h.Chunk.Text, Context: expanded,
			Ref:      fmt.Sprintf("[[doc:%s#%s]]", h.Doc.ID, h.Chunk.ID),
			Citation: h.Citation.Verbatim, Score: h.Score,
		})
	}
	return ok(map[string]any{"hits": out, "semantic": semantic, "notice": untrustedLibraryNotice})
}

func (t *librarySearchTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type docReadTool struct{ svc *Service }

func (t *docReadTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolDocRead,
		Description: "Read the agent profile, user profile or notebook by index, section or search without loading it whole",
		InputSchema: docReadSchema,
		Effect:      types.EffectReadOnly, Idempotency: types.IdemPure, Cancel: types.CancelImmediate,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"doc"}, Keywords: []string{"doc", "read", "profile", "notebook"}}
}

func (t *docReadTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Scope    string `json:"scope"`
		Doc      string `json:"doc"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if a.Doc == "" {
		return tools.Result{Status: tools.StatusInputError, Output: "doc is required"}, nil
	}
	id, kind, err := t.svc.resolveDocRef(ctx, a.Scope, a.Doc)
	if err != nil {
		return toolErr(err)
	}
	_ = kind
	doc, err := t.svc.ReadDoc(ctx, docScopes(a.Scope, t.svc), id, a.Selector)
	if err != nil {
		return toolErr(err)
	}
	return ok(doc)
}

func (t *docReadTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

type docEditTool struct{ svc *Service }

func (t *docEditTool) Definition() tools.Definition {
	return tools.Definition{Name: ToolDocEdit,
		Description: "Edit one fragment of the agent profile, user profile or notebook checking its hash first",
		InputSchema: docEditSchema,
		Effect:      types.EffectWriteLocal, Idempotency: types.IdemIdempotent, Cancel: types.CancelAfterCompletion,
		Timeout: 60_000_000_000, OutputBudget: tools.DefaultOutputBudget, Fast: true,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: "memory", Tags: []string{"doc"}, Keywords: []string{"doc", "edit", "profile"}}
}

func (t *docEditTool) Invoke(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
	var a struct {
		Scope        string `json:"scope"`
		Doc          string `json:"doc"`
		FragmentID   string `json:"fragment_id"`
		ExpectedHash string `json:"expected_hash"`
		Op           string `json:"op"`
		Text         string `json:"text"`
		HeadingPath  string `json:"heading_path"`
		Position     int    `json:"position"`
		Provenance   string `json:"provenance"`
		Explicit     bool   `json:"explicit"`
	}
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	id, _, err := t.svc.resolveDocRef(ctx, a.Scope, a.Doc)
	if err != nil {
		return toolErr(err)
	}
	frag, err := t.svc.EditFragment(ctx, docScopes(a.Scope, t.svc), DocEdit{
		Doc: id, FragmentID: a.FragmentID, ExpectedHash: a.ExpectedHash, Op: FragmentOp(a.Op),
		Text: a.Text, Heading: a.HeadingPath, Position: a.Position, Provenance: a.Provenance, Explicit: a.Explicit,
	})
	if err != nil {
		var hm *HashMismatchError
		if asErr(err, &hm) {
			raw, _ := json.Marshal(map[string]any{"error": "hash mismatch; redo the edit on the current text", "current": hm.Current})
			return tools.Result{Status: tools.StatusInputError, Output: string(raw)}, nil
		}
		return toolErr(err)
	}
	return ok(frag)
}

func (t *docEditTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	return scopeKeys(args)
}

// asErr unwraps to *HashMismatchError without importing errors
// helpers at every site.
func asErr(err error, target **HashMismatchError) bool {
	for err != nil {
		if hm, ok := err.(*HashMismatchError); ok {
			*target = hm
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// resolveDocRef accepts a document id or a kind (agent, user,
// notebook, persona:<name>) and returns the id, creating it.
func (s *Service) resolveDocRef(ctx context.Context, scope, ref string) (string, string, error) {
	if scope == "" {
		if len(s.scopes) == 1 {
			scope = s.scopes[0]
		} else {
			return "", "", &inputError{"memory: scope is required to address a document by kind"}
		}
	}
	if strings.HasPrefix(ref, "idd_") {
		return ref, "", nil
	}
	id, err := s.ResolveDoc(ctx, scope, ref)
	return id, ref, err
}

func docScopes(scope string, svc *Service) []string {
	if scope != "" {
		return []string{scope}
	}
	return svc.Scopes()
}

func scopeKeys(args json.RawMessage) ([]string, error) {
	var a struct {
		Scope  string   `json:"scope"`
		Scopes []string `json:"scopes"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var out []string
	if a.Scope != "" {
		out = append(out, "scope:"+a.Scope)
	}
	for _, sc := range a.Scopes {
		out = append(out, "scope:"+sc)
	}
	return out, nil
}

func parseCandidate(args json.RawMessage) (Candidate, error) {
	var a struct {
		Statement    string   `json:"statement"`
		Scope        string   `json:"scope"`
		Kind         string   `json:"kind"`
		Entities     []string `json:"entities"`
		ValidFrom    any      `json:"valid_from"`
		ValidTo      any      `json:"valid_to"`
		ExpiresAt    any      `json:"expires_at"`
		Confidence   float64  `json:"confidence"`
		Importance   float64  `json:"importance"`
		Sources      []Source `json:"sources"`
		Trust        string   `json:"trust"`
		Conversation string   `json:"conversation"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return Candidate{}, err
	}
	if strings.TrimSpace(a.Statement) == "" {
		return Candidate{}, fmt.Errorf("statement is required")
	}
	if strings.TrimSpace(a.Scope) == "" {
		return Candidate{}, fmt.Errorf("scope is required")
	}
	c := Candidate{
		Statement: a.Statement, Scope: a.Scope, Kind: Kind(a.Kind),
		Entities: a.Entities, Confidence: a.Confidence, Importance: a.Importance,
		Sources: a.Sources, Trust: Trust(a.Trust), Conversation: a.Conversation,
	}
	var err error
	if c.ValidFrom, err = parseTime(a.ValidFrom); err != nil {
		return Candidate{}, fmt.Errorf("valid_from: %w", err)
	}
	if c.ValidTo, err = parseTime(a.ValidTo); err != nil {
		return Candidate{}, fmt.Errorf("valid_to: %w", err)
	}
	if c.ExpiresAt, err = parseTime(a.ExpiresAt); err != nil {
		return Candidate{}, fmt.Errorf("expires_at: %w", err)
	}
	return c, nil
}

// parseTime accepts RFC3339 strings or epoch milliseconds.
func parseTime(v any) (time.Time, error) {
	if v == nil {
		return time.Time{}, nil
	}
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) == "" {
			return time.Time{}, nil
		}
		if tm, err := time.Parse(time.RFC3339, strings.TrimSpace(t)); err == nil {
			return tm.UTC(), nil
		}
		var ms int64
		if _, err := fmt.Sscanf(strings.TrimSpace(t), "%d", &ms); err == nil {
			return fromMS(ms), nil
		}
		return time.Time{}, fmt.Errorf("unparseable time %q", t)
	case float64:
		if t == 0 {
			return time.Time{}, nil
		}
		return fromMS(int64(t)), nil
	case json.Number:
		ms, err := t.Int64()
		if err != nil {
			return time.Time{}, err
		}
		return fromMS(ms), nil
	default:
		return time.Time{}, fmt.Errorf("unparseable time %v", v)
	}
}

func ok(v any) (tools.Result, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusOK, Output: string(raw)}, nil
}

func toolErr(err error) (tools.Result, error) {
	if IsInputError(err) {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	if isScopeErr(err) || isNotFound(err) {
		return tools.Result{Status: tools.StatusInputError, Output: err.Error()}, nil
	}
	return tools.Result{Status: tools.StatusToolError, Output: err.Error()}, nil
}

func isScopeErr(err error) bool {
	return errors.Is(err, ErrNoScope) || errors.Is(err, ErrScopeDenied)
}

func isNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, ErrHashMismatch) || errors.Is(err, ErrEscalated)
}

// MemoryTools returns every H11 tool bound to svc.
func MemoryTools(svc *Service) []tools.Tool {
	return []tools.Tool{
		&memoryWriteTool{svc: svc},
		&memoryProposeTool{svc: svc},
		&memorySearchTool{svc: svc},
		&historySearchTool{svc: svc},
		&libraryIngestTool{svc: svc},
		&librarySearchTool{svc: svc},
		&docReadTool{svc: svc},
		&docEditTool{svc: svc},
		&libraryBrowseTool{name: ToolLibraryList, svc: svc},
		&libraryBrowseTool{name: ToolLibraryView, svc: svc},
		&libraryBrowseTool{name: ToolLibraryGrep, svc: svc},
		&libraryBrowseTool{name: ToolLibraryStatus, svc: svc},
	}
}

// WireMemoryTools connects the real memory providers to the core
// stubs registered by tools.RegisterBuiltins, or registers the real
// tools when the registry lacks them (the WireSearchCall pattern,
// Section 14.12: wiring never changes the catalog prefix).
func WireMemoryTools(reg *tools.MapRegistry, svc *Service) {
	for _, real := range MemoryTools(svc) {
		if _, browse := real.(*libraryBrowseTool); browse && !libraryEnabled() {
			continue
		}
		if real.Definition().Name == ToolLibraryIngest {
			WireLibraryIngestTool(reg, svc)
			continue
		}
		existing, err := reg.Get(real.Definition().Name)
		if err != nil {
			_ = reg.Register(real)
			continue
		}
		if browse, ok := existing.(*libraryBrowseTool); ok {
			browse.svc = svc
		}
		if stub, ok := existing.(*tools.StubTool); ok {
			provider := real
			stub.Provider = func(ctx context.Context, inv tools.Invocation) (tools.Result, error) {
				return provider.Invoke(ctx, inv)
			}
		}
	}
}

// VisionToolAdapter plugs the vision_extract tool into library
// ingest for scanned PDFs and images (Section 10.9 step 2).
type VisionToolAdapter struct {
	Tool tools.Tool
}

// ExtractText implements VisionExtractor via the vision tool.
func (a *VisionToolAdapter) ExtractText(ctx context.Context, input string, pages []int) ([][]string, error) {
	args, _ := json.Marshal(map[string]any{"input": input, "mode": "text", "pages": pages})
	res, err := a.Tool.Invoke(ctx, tools.Invocation{Tool: "vision_extract", Arguments: args})
	if err != nil {
		return nil, err
	}
	if res.Status != tools.StatusOK {
		return nil, fmt.Errorf("memory: vision_extract: %s", res.Output)
	}
	var parsed struct {
		Pages []struct {
			Page int    `json:"page"`
			Text string `json:"text"`
		} `json:"pages"`
	}
	if err := json.Unmarshal([]byte(res.Output), &parsed); err != nil {
		return nil, err
	}
	var out [][]string
	for _, p := range parsed.Pages {
		out = append(out, splitParagraphs(p.Text))
	}
	return out, nil
}

// WireLibraryIngestTool replaces the deferred simulated capability in the shared
// catalog. It never changes the core prefix; callers mark the name deferred.
func WireLibraryIngestTool(reg *tools.MapRegistry, svc *Service) {
	reg.Unregister(ToolLibraryIngest)
	_ = reg.Register(&libraryIngestTool{svc: svc})
}
