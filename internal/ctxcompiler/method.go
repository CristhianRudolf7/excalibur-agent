package ctxcompiler

import "strings"

// Working method and model-family adaptation (Sections 7.3, 11.1,
// 19.1-19.2).
//
// Small, cheap models do not enter a build-verify loop on their own:
// the most common failure pattern is writing a solution, re-reading
// it, and stopping without ever executing a check. The L0 working
// method below teaches the loop explicitly (understand, explore,
// plan, implement in small steps, test, verify, finish) and is tuned
// for the GPT family, which is the reference family. Per-family
// variants adjust prompting style and tool-format discipline without
// touching the provider wire protocol, which belongs to the model
// adapters. Unknown model ids fall back to the GPT default.
//
// The method block lives in L0, the stable prefix: it only changes
// with the harness version (and, when family adaptation is on, with
// the model family), so it never churns the input cache inside a
// run.
//
// PromptFamily is distinct from ProviderFamily in cache.go: the
// cache family describes provider cache behavior, while the prompt
// family selects the wording variant for a model id.

// PromptFamily selects one prompt/tool-format variant.
type PromptFamily string

// Supported prompt families (Section 19.1 model roles).
const (
	// PromptGPT is the default: GPT and other OpenAI-compatible
	// models, including the reference cheap model.
	PromptGPT PromptFamily = "gpt"
	// PromptClaude covers Anthropic Claude models.
	PromptClaude PromptFamily = "claude"
	// PromptGemini covers Google Gemini models.
	PromptGemini PromptFamily = "gemini"
	// PromptDeepSeek covers DeepSeek models.
	PromptDeepSeek PromptFamily = "deepseek"
	// PromptGrok covers xAI Grok models.
	PromptGrok PromptFamily = "Grok"
	// PromptQwen covers Alibaba Qwen models.
	PromptQwen PromptFamily = "qwen"
	// PromptUnknown means the model id matched no known family.
	PromptUnknown PromptFamily = "unknown"
)

// DetectFamily maps a model id to its prompt family by substring
// match on the lowercased id. Empty or unrecognized ids report
// PromptUnknown; callers default them to PromptGPT.
func DetectFamily(model string) PromptFamily {
	m := strings.ToLower(model)
	switch {
	case m == "":
		return PromptUnknown
	case strings.Contains(m, "claude"):
		return PromptClaude
	case strings.Contains(m, "gemini"):
		return PromptGemini
	case strings.Contains(m, "deepseek"):
		return PromptDeepSeek
	case strings.Contains(m, "grok"):
		return PromptGrok
	case strings.Contains(m, "qwen"):
		return PromptQwen
	case strings.Contains(m, "gpt") || strings.Contains(m, "openai") || strings.Contains(m, "o1") || strings.Contains(m, "o3") || strings.Contains(m, "o4"):
		return PromptGPT
	default:
		return PromptUnknown
	}
}

// Default maps PromptUnknown to the tuned GPT default.
func (f PromptFamily) Default() PromptFamily {
	if f == PromptUnknown {
		return PromptGPT
	}
	return f
}

// WorkingMethod is the shared working method taught to every model:
// understand, explore, plan, implement in small steps, test, verify,
// finish, with explicit thoroughness and error-recovery rules. It is
// written for small models: short numbered steps, concrete verbs,
// and no assumed background beyond the tool definitions.
func WorkingMethod() string {
	return `working method:
1. Understand: restate the goal and the done criteria in your own words. If the request names files, paths or commands, use exactly those.
2. Explore first: list and read the relevant files before changing anything. Do not guess APIs, paths or behavior; check them with a tool.
3. Plan briefly: keep a short ordered todo (3-7 steps) of what you will do and how you will verify each step. Update it as you learn.
4. Implement in small steps: make one change at a time, then observe the result before the next change. Small steps are easier to verify and revert.
5. Test what you built: run the relevant checks (build, tests, scripts, probes) and read their full output. A step is done only when an executable check confirms it.
6. On any error: read the whole error message, find the exact failing line or parameter, fix that one thing, and retry once. Never retry an identical call blindly; if it fails twice, change strategy (different tool, different arguments, or read the relevant code first).
7. On a tool-parameter error (unknown name, missing required field, bad enum): fix exactly the named parameter, copying allowed values from the tool definition or the error message. Never invent enum values or target names.
8. Finish only when every requirement is verified: summarize what you verified and how. Never declare success without evidence, and never present partial work as done.
Rules: be thorough and persistent; do not stop early. Prefer real tool calls over narration. Keep the context small: rely on the pinned working notes for paths, decisions and failures instead of re-reading large outputs.`
}

// FamilyNote is the per-family variant appended after the shared
// method: prompting style and tool-format discipline only. The
// provider wire protocol is owned by the model adapters; these notes
// only tell the model how to use it well.
func FamilyNote(f PromptFamily) string {
	switch f.Default() {
	case PromptClaude:
		return "Style: reason briefly before acting, then make all independent tool calls in the same block. Verify results with tools instead of narrating them back."
	case PromptGemini:
		return "Style: keep tool arguments strict and minimal, one objective per turn. Read each observation before choosing the next action."
	case PromptDeepSeek:
		return "Style: keep reasoning short and act. Prefer one tool call per turn unless the calls are independent reads. Re-read the error text before every retry."
	case PromptGrok:
		return "Style: follow tool schemas exactly (exact property names, no invented fields). Quote and escape shell commands carefully. Verify every change by executing it."
	case PromptQwen:
		return "Style: work in English, keep arguments minimal and exact, and verify each step by executing it before moving on."
	default:
		return "Style: batch independent read-only calls in one turn with exact JSON arguments. After acting, verify with an executable check before finishing."
	}
}

// MethodBlock renders the L0 method block for a family: the shared
// method plus, when adapt is true, the family style line. With adapt
// off the output is family-independent, so same-family and
// cross-family runs share the L0 prefix.
func MethodBlock(f PromptFamily, adapt bool) string {
	var b strings.Builder
	b.WriteString(WorkingMethod())
	b.WriteString("\n")
	if adapt {
		b.WriteString(FamilyNote(f))
		b.WriteString("\n")
	}
	return b.String()
}
