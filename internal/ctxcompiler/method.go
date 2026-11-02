package ctxcompiler

import (
	"strings"
)

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
	// PromptMuse covers Meta Muse models (Muse Spark, Muse Glimmer,
	// Muse Spark Open), served through the Meta Model API or
	// OpenAI-compatible third-party endpoints.
	PromptMuse PromptFamily = "muse"
	// PromptUnknown means the model id matched no known family.
	PromptUnknown PromptFamily = "unknown"
)

// DetectFamily maps a model id to its prompt family by substring
// and prefix match on the lowercased id. Empty or unrecognized ids
// report PromptUnknown; callers default them to PromptGPT. Muse is
// matched first on its id prefixes (`muse-`, `meta-models/`) so a
// Muse id that happens to contain another family's substring still
// selects the Muse variant.
func DetectFamily(model string) PromptFamily {
	m := strings.ToLower(model)
	switch {
	case m == "":
		return PromptUnknown
	case strings.HasPrefix(m, "muse-") || strings.HasPrefix(m, "meta-models/") ||
		strings.Contains(m, "muse-spark") || strings.Contains(m, "muse-glimmer") ||
		strings.Contains(m, "muse_spark"):
		return PromptMuse
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

// EnvMethodV2 is the switch for the evidence rules appended to the
// working method (default ON). EXCALIBUR_METHOD_V2=off restores the
// original eight-step block byte for byte, for ablations.
const EnvMethodV2 = "EXCALIBUR_METHOD_V2"

// MethodV2Enabled reports whether the evidence rules (steps 9 to 17)
// are part of the working method.
func MethodV2Enabled() bool { return envFlagDefaultOn(nil, EnvMethodV2) }

// workingMethodSteps is the original eight-step method, unchanged.
const workingMethodSteps = `working method:
1. Understand: restate the goal and the done criteria in your own words. If the request names files, paths or commands, use exactly those.
2. Explore first: list and read the relevant files before changing anything. Do not guess APIs, paths or behavior; check them with a tool.
3. Plan briefly: keep a short ordered todo (3-7 steps) of what you will do and how you will verify each step. Update it as you learn.
4. Implement in small steps: make one change at a time, then observe the result before the next change. Small steps are easier to verify and revert.
5. Test what you built: run the relevant checks (build, tests, scripts, probes) and read their full output. A step is done only when an executable check confirms it.
6. On any error: read the whole error message, find the exact failing line or parameter, fix that one thing, and retry once. Never retry an identical call blindly; if it fails twice, change strategy (different tool, different arguments, or read the relevant code first).
7. On a tool-parameter error (unknown name, missing required field, bad enum): fix exactly the named parameter, copying allowed values from the tool definition or the error message. Never invent enum values or target names.
8. Finish only when every requirement is verified: summarize what you verified and how. Never declare success without evidence, and never present partial work as done.
`

// workingMethodEvidence are the evidence rules added by
// EXCALIBUR_METHOD_V2. Each line is short and general: provided
// inputs, success paths, unseen instances, measurement, handoffs
// between steps, services, sources and the task's own tools.
const workingMethodEvidence = `9. Copy provided data first (cp -a) as originals. Change inputs/tests only if asked.
10. Test the success path too; include failures.
11. Never hard-code examples; vary inputs; use an independent reference.
12. Measure the way the instruction measures: same metric, repeats, margin; state it.
13. Pass values between steps through files or pipes; do not retype values.
14. Run a script you wrote on the real input after its last edit; compile/--help do not test it.
15. Keep required services running: detach, log, probe from a new shell.
16. If a source is unreachable, try mirrors or local copies. An impossible claim needs the failing command and output.
17. Use only the task's own files and tools; avoid harness logs and host internals.
`

// workingMethodRules closes the block for both variants.
const workingMethodRules = `Rules: be thorough and persistent; do not stop early. Prefer real tool calls over narration. Keep the context small: rely on the pinned working notes for paths, decisions and failures instead of re-reading large outputs.`

// WorkingMethod is the shared working method taught to every model:
// understand, explore, plan, implement in small steps, test, verify,
// finish, with explicit thoroughness and error-recovery rules. It is
// written for small models: short numbered steps, concrete verbs,
// and no assumed background beyond the tool definitions. With
// EXCALIBUR_METHOD_V2 on (default) the evidence rules follow step 8;
// off returns the original block exactly.
func WorkingMethod() string {
	if !MethodV2Enabled() {
		return workingMethodSteps + workingMethodRules
	}
	restriction := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_METHOD_NO_OUTSIDE_CODE") {
		restriction = "If the instruction forbids outside code or dependencies, do not download or copy them; build from the tools you have.\n"
	}
	dataCheck := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_METHOD_DATA_FORMAT") {
		dataCheck = "Before computing on a data file, check its format (separators, decimal commas, units, header) and print a few parsed values next to the raw text.\n"
	}
	broadCheck := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_BROAD_REVERIFICATION") {
		broadCheck = "After removing or replacing every occurrence, run a broad pattern search over all files.\n"
	}
	inputCare := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_INPUT_RESTORE") {
		inputCare = preservedInputGuidance
	}
	preflight := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_DEFAULT_PREFLIGHT") {
		preflight = defaultPreflightGuidance
	}
	visualCheck := ""
	if envFlagDefaultOn(nil, "EXCALIBUR_INSPECT_IMAGE") {
		visualCheck = visualInspectionGuidance
	}
	return workingMethodSteps + workingMethodEvidence + visualCheck + inputCare + preflight + dataCheck + broadCheck + restriction +
		"Rules: do not stop early; use tools. Use pinned working notes instead of re-reading large outputs."
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
	case PromptMuse:
		return "Style: keep tool arguments exact JSON with no invented fields or target names — copy allowed values from the definition or the error. Answer every parallel call with its own result in call order; never invent results. Finish only via the finish tool with evidence from executed checks."
	default:
		return "Style: batch independent read-only calls in one turn with exact JSON arguments. After acting, verify with an executable check before finishing."
	}
}

// AdaptToolDescription returns the model-visible description of one
// tool for a family. Every family except Muse receives the
// description unchanged, so the GPT reference bytes never move. For
// Muse the exec and finish tools carry the Meta Model API
// discipline: exact JSON arguments answered in call order with no
// invented results, and finish-only-via-tool (the API supports only
// tool_choice auto, so a text-only stop is never a finish). All
// other tools are identical across families; callers apply this only
// when family adaptation is on so adapt-off output stays
// family-independent.
func AdaptToolDescription(f PromptFamily, name, desc string) string {
	if f.Default() != PromptMuse {
		return desc
	}
	switch name {
	case "exec":
		return desc + " Pass arguments as exact JSON (no invented fields); answer every parallel call with its own result in call order; never invent output — run the command and report what it returned."
	case "finish":
		return desc + " Muse finishes ONLY through this tool with summary and evidence from executed checks; never stop with a text-only answer."
	default:
		return desc
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

const preservedInputGuidance = "Inspect disposable copies before opening or changing irreplaceable inputs. Validate against preserved originals, never a source rewritten from candidate output.\n"

const defaultPreflightGuidance = "Before expensive work, reuse fresh preflight evidence or exercise the requested default command on small input through final output files. Overrides, accepted API calls and progress prove neither the default path nor final serialization.\n"

// Local image evidence applies to assistant worker runs as well as exec/eval.
const visualInspectionGuidance = "For visual/interactive results, capture an image and use inspect_image; read screen_text/OCR before acting. Blank/unchanged screens leave the goal unmet; use screen errors/setup messages to change configuration before re-checking processes. For input changes, capture before/after and use compare_to.\n"
