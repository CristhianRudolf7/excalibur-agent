package ctxcompiler

import "context"

// Sources are the small seams through which the compiler reads
// memory, skills and tools without importing their packages. Section
// 11.1 names the producers (Retriever with budget, skill index with
// at most 3 suggestions, tool catalog); each producer stays behind
// its interface here and tests use the fakes in the fake subpackage.

// MemoryHit is one retrieved memory rendered into L3.
type MemoryHit struct {
	// Text is the rendered recall line.
	Text string
	// Tokens is the precomputed cost; 0 means the compiler estimates.
	Tokens int
}

// MemorySource recalls memories for a task within a token budget.
type MemorySource interface {
	// Recall returns at most budgetTokens worth of memories for
	// query inside scopes, most relevant first.
	Recall(ctx context.Context, query string, scopes []string, budgetTokens int) ([]MemoryHit, error)
}

// SkillSuggestion is one indexed skill suggested for a task.
type SkillSuggestion struct {
	// Name is the skill name.
	Name string
	// Why is the one-line reason it was suggested.
	Why string
}

// SkillSource serves the grouped L1 catalog and the L3 suggestions.
type SkillSource interface {
	// CatalogSnippet renders the grouped catalog for role within
	// budgetTokens: pertinent groups in full, the rest as headers
	// with counts (12.3).
	CatalogSnippet(ctx context.Context, role string, budgetTokens int) (string, error)
	// Suggest returns at most max suggestions for the task
	// description, best first.
	Suggest(ctx context.Context, taskDescription string, max int) ([]SkillSuggestion, error)
}

// CoreTool is the minimal tool shape the compiler serializes into
// L0. Full definitions live in internal/tools; adapters translate
// them to provider tool definitions (11.6 keeps domain tools out of
// the prefix, 13.3 loads them on demand).
type CoreTool struct {
	// Name is the snake_case tool name.
	Name string
	// Description is the short tool description.
	Description string
}

// ToolSource serves the core tool catalog of a role in a stable
// order. The compiler sorts by name again so sources need not
// bother.
type ToolSource interface {
	// CoreTools returns the core tool definitions of role.
	CoreTools(ctx context.Context, role string) ([]CoreTool, error)
}
