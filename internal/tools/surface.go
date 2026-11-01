package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Small-model tool surface tuning (agent-quality workstream). Every
// mechanism in this file is gated by a SurfaceTuning switch so it can
// be measured on and off; all switches default ON. Environment
// overrides use EXCALIBUR_TOOL_<NAME>=0|false|off to disable (any
// other value, including unset, keeps the default ON).
type SurfaceTuning struct {
	// TolerantArgs accepts common argument aliases and scalar types
	// (cmd for command, limit for k, input for arguments, "5" for 5)
	// and strips model-visible-internal keys before validation.
	TolerantArgs bool
	// SimpleExec keeps the model-visible exec schema to command,
	// workdir, timeout and session handling; legacy target/owner are
	// ignored (embedded runs locally) instead of erroring.
	SimpleExec bool
	// ActionableErrors appends repair hints: command-not-found
	// package-manager guidance, missing-file next actions, valid
	// value enumerations and the received-keys summary.
	ActionableErrors bool
	// HeadTailResults keeps the pipeline head+tail truncation but
	// names the retrieval tool and blob ref explicitly so a small
	// model can fetch the rest instead of re-running the command.
	HeadTailResults bool
	// WireEmbedded fills the exec/eval stub providers (task_db,
	// subagent, ack_directive, history_search, pin_observation,
	// leave_continuation_note, search_skills, load_skill) with
	// task-scoped in-memory implementations instead of "not wired"
	// errors. Owned by the wireengine assembly; the flag lives here
	// so the whole surface reads one tuning source.
	WireEmbedded bool
	// CallNameFlex lets call_tool take the deferred tool name from
	// inside the arguments object (name/tool/tool_name keys) or as
	// the single top-level key, instead of failing with
	// "name is required". Small models often nest the name or wrap
	// the call as {"toolname": {...}}.
	CallNameFlex bool
	// ShellFirst rewrites tool descriptions toward chained Unix
	// commands (one exec with && and pipes beats five bespoke calls)
	// while keeping the file tools that reject broken edits.
	ShellFirst bool
}

// DefaultSurfaceTuning returns the all-ON tuning.
func DefaultSurfaceTuning() SurfaceTuning {
	return SurfaceTuning{
		TolerantArgs: true, SimpleExec: true, ActionableErrors: true,
		HeadTailResults: true, WireEmbedded: true, ShellFirst: true,
		CallNameFlex: true,
	}
}

// TuningFromEnv applies EXCALIBUR_TOOL_* overrides over def.
func TuningFromEnv(def SurfaceTuning) SurfaceTuning {
	if off("EXCALIBUR_TOOL_TOLERANT_ARGS") {
		def.TolerantArgs = false
	}
	if off("EXCALIBUR_TOOL_SIMPLE_EXEC") {
		def.SimpleExec = false
	}
	if off("EXCALIBUR_TOOL_ACTIONABLE_ERRORS") {
		def.ActionableErrors = false
	}
	if off("EXCALIBUR_TOOL_HEAD_TAIL") {
		def.HeadTailResults = false
	}
	if off("EXCALIBUR_TOOL_WIRE_EMBEDDED") {
		def.WireEmbedded = false
	}
	if off("EXCALIBUR_TOOL_SHELL_FIRST") {
		def.ShellFirst = false
	}
	if off("EXCALIBUR_TOOL_CALL_NAME") {
		def.CallNameFlex = false
	}
	return def
}

func off(key string) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// NormalizeArgs rewrites raw model arguments into canonical form
// before schema validation: string/number coercions, common aliases
// and legacy internal keys. Unknown tools pass through unchanged.
// It never fails; unparseable input returns the repaired bytes as-is
// so validation still reports the schema error with a repair hint.
func NormalizeArgs(name string, raw json.RawMessage, t SurfaceTuning) json.RawMessage {
	if !t.TolerantArgs && !t.SimpleExec {
		return raw
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		trimmed = "{}"
	}
	// A bare JSON string is the single most likely argument.
	if len(trimmed) >= 2 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal([]byte(trimmed), &s); err == nil {
			return wrapBareString(name, s)
		}
		return json.RawMessage(trimmed)
	}
	// A single-element string array behaves like the bare string.
	if strings.HasPrefix(trimmed, "[") {
		var arr []any
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil && len(arr) == 1 {
			if s, ok := arr[0].(string); ok {
				return wrapBareString(name, s)
			}
		}
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return json.RawMessage(trimmed)
	}
	if obj == nil {
		obj = map[string]any{}
	}
	lower := map[string]string{}
	for k := range obj {
		lower[strings.ToLower(k)] = k
	}
	// take moves an alias value onto key, first hit wins.
	take := func(key string, aliases ...string) (any, bool) {
		if v, ok := obj[key]; ok {
			return v, true
		}
		for _, a := range aliases {
			if orig, ok := lower[strings.ToLower(a)]; ok {
				v := obj[orig]
				delete(obj, orig)
				obj[key] = v
				return v, true
			}
		}
		return nil, false
	}
	getStr := func(v any) (string, bool) {
		switch t := v.(type) {
		case string:
			return t, true
		case float64:
			if t == float64(int64(t)) {
				return strconv.FormatInt(int64(t), 10), true
			}
			return strconv.FormatFloat(t, 'f', -1, 64), true
		case bool:
			return strconv.FormatBool(t), true
		case nil:
			return "", false
		}
		return "", false
	}
	setStr := func(key string, v any) {
		if s, ok := getStr(v); ok {
			obj[key] = s
		}
	}
	setInt := func(key string, v any) {
		switch n := v.(type) {
		case float64:
			obj[key] = int(n)
		case string:
			s := strings.TrimSpace(n)
			if i, err := strconv.Atoi(s); err == nil {
				obj[key] = i
			} else if f, err := strconv.ParseFloat(s, 64); err == nil {
				obj[key] = int(f)
			}
		case bool:
			if n {
				obj[key] = 1
			} else {
				obj[key] = 0
			}
		case int:
			obj[key] = n
		}
	}
	setNum := func(key string, v any) {
		switch n := v.(type) {
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				obj[key] = f
			}
		case bool:
			if n {
				obj[key] = 1.0
			} else {
				obj[key] = 0.0
			}
		default:
			obj[key] = v
		}
	}
	setBool := func(key string, v any) {
		switch b := v.(type) {
		case string:
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "1", "true", "yes", "on", "pty", "interactive":
				obj[key] = true
			case "0", "false", "no", "off", "":
				obj[key] = false
			default:
				obj[key] = true
			}
		case float64:
			obj[key] = b != 0
		case int:
			obj[key] = b != 0
		default:
			obj[key] = v
		}
	}

	switch name {
	case "exec":
		if v, ok := take("command", "cmd", "command_line", "script", "run", "shell", "code"); ok {
			setStr("command", v)
		}
		if v, ok := take("workdir", "cwd", "dir", "directory", "working_dir", "workingdir", "path"); ok {
			setStr("workdir", v)
		}
		if v, ok := take("yield_ms", "yield", "wait_ms"); ok {
			setInt("yield_ms", v)
		}
		// timeout_s, timeout_secs, timeout, time_limit and duration
		// all name the same knob; bare numbers <= 300 name seconds,
		// larger ones milliseconds (model habit from yield_ms).
		if v, ok := take("timeout_s", "timeout_secs", "timeout", "time_limit", "duration", "timeout_ms"); ok {
			orig, _ := lower["timeout_ms"]
			_ = orig
			switch n := v.(type) {
			case string:
				s := strings.TrimSpace(n)
				l := strings.ToLower(s)
				switch {
				case strings.HasSuffix(l, "ms"):
					if f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-2]), 64); err == nil {
						obj["yield_ms"] = int(f)
					}
					delete(obj, "timeout_s")
				case strings.HasSuffix(l, "s"):
					if f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-1]), 64); err == nil {
						obj["timeout_s"] = f
					}
				default:
					setNum("timeout_s", v)
					if f, ok := obj["timeout_s"].(float64); ok && f > 300 {
						obj["yield_ms"] = int(f)
						delete(obj, "timeout_s")
					}
				}
			case float64:
				if n > 300 {
					obj["yield_ms"] = int(n)
					delete(obj, "timeout_s")
				} else {
					obj["timeout_s"] = n
				}
			case int:
				if float64(n) > 300 {
					obj["yield_ms"] = n
					delete(obj, "timeout_s")
				} else {
					obj["timeout_s"] = float64(n)
				}
			default:
				obj["timeout_s"] = v
			}
		}
		if v, ok := take("tty", "pty", "interactive", "terminal"); ok {
			setBool("tty", v)
		}
		if v, ok := take("background", "background_run", "async", "detach"); ok {
			setBool("background", v)
		}
		if t.SimpleExec {
			// Target/owner stay internal defaults in embedded runs:
			// drop them instead of failing the call. Env is kept
			// when valid (vault secret resolution depends on it)
			// but single strings are split into entries.
			delete(obj, "target")
			delete(obj, "owner")
			for _, k := range []string{"node", "host", "container", "cwd_first"} {
				delete(obj, k)
			}
		}
		if v, ok := obj["env"]; ok {
			if s, isStr := v.(string); isStr {
				obj["env"] = splitEnvString(s)
			}
		}
	case "exec_io":
		if v, ok := take("action", "op", "cmd", "operation"); ok {
			setStr("action", v)
			obj["action"] = strings.ToLower(strings.TrimSpace(asString(obj["action"])))
		}
		if v, ok := take("session", "sid", "session_id", "id", "ref"); ok {
			setStr("session", v)
		}
		if v, ok := take("text", "input", "content", "data", "stdin"); ok {
			setStr("text", v)
		}
		if v, ok := take("pattern", "regex", "match", "expect"); ok {
			setStr("pattern", v)
		}
		if v, ok := take("signal", "sig", "signum"); ok {
			setStr("signal", v)
		}
		if v, ok := take("keys", "key"); ok {
			switch k := v.(type) {
			case string:
				obj["keys"] = []any{k}
			case []any:
				obj["keys"] = k
			}
		}
		if v, ok := take("timeout_s", "timeout", "timeout_secs", "wait_s"); ok {
			setNum("timeout_s", v)
		}
		if v, ok := take("offset", "from", "next_offset"); ok {
			setInt("offset", v)
		}
	case "search_tools":
		if v, ok := take("query", "q", "text", "search", "question", "ask"); ok {
			setStr("query", v)
		} else if _, ok := obj["query"]; !ok {
			obj["query"] = ""
		}
		if v, ok := take("k", "limit", "n", "top_k", "count", "max_results"); ok {
			setInt("k", v)
		}
	case "call_tool":
		if v, ok := take("name", "tool", "tool_name"); ok {
			setStr("name", v)
			obj["name"] = strings.ToLower(strings.TrimSpace(asString(obj["name"])))
		}
		if v, ok := take("arguments", "args", "input", "parameters", "params"); ok {
			switch a := v.(type) {
			case string:
				s := strings.TrimSpace(a)
				if s == "" {
					obj["arguments"] = map[string]any{}
				} else {
					var m map[string]any
					if err := json.Unmarshal([]byte(s), &m); err == nil {
						obj["arguments"] = m
					} else {
						obj["arguments"] = map[string]any{"input": a}
					}
				}
			default:
				obj["arguments"] = v
			}
		} else if _, ok := obj["arguments"]; !ok {
			obj["arguments"] = map[string]any{}
		}
		if t.CallNameFlex {
			promoteCallToolName(obj, trimmed)
		}
	case "search_skills":
		if v, ok := take("query", "q", "text", "search", "question"); ok {
			setStr("query", v)
		} else if _, ok := obj["query"]; !ok {
			obj["query"] = ""
		}
		if v, ok := take("group", "skill_group", "category", "filter"); ok {
			setStr("group", v)
		}
	case "load_skill":
		if v, ok := take("name", "skill", "skill_name", "id"); ok {
			setStr("name", v)
		}
	case "read_file", "glob":
		if name == "read_file" {
			if v, ok := take("path", "file", "filename", "filepath", "pattern"); ok {
				setStr("path", v)
			}
			if v, ok := take("offset", "start", "line", "from", "lineno"); ok {
				setInt("offset", v)
			}
			if v, ok := take("limit", "n", "lines", "count", "max_lines"); ok {
				setInt("limit", v)
			}
		} else {
			if v, ok := take("pattern", "glob", "match", "query", "path"); ok {
				setStr("pattern", v)
			}
			if v, ok := take("dir", "cwd", "path", "directory", "workdir"); ok {
				// glob uses dir; a path alias may have set pattern
				// above, so only fill dir when still absent.
				if _, has := obj["dir"]; !has {
					setStr("dir", v)
				}
			}
			if v, ok := take("max_results", "limit", "n", "count"); ok {
				setInt("max_results", v)
			}
		}
	case "write_file", "edit_file":
		if v, ok := take("path", "file", "filename", "filepath"); ok {
			setStr("path", v)
		}
		if v, ok := take("expected_fp", "fingerprint", "fp", "hash", "etag"); ok {
			setStr("expected_fp", v)
		}
		if name == "edit_file" {
			if v, ok := take("old_text", "old", "find", "search", "before"); ok {
				setStr("old_text", v)
			}
			if v, ok := take("new_text", "new", "replace", "replacement", "after"); ok {
				setStr("new_text", v)
			}
		} else {
			if v, ok := take("content", "text", "body", "data"); ok {
				setStr("content", v)
			}
		}
	case "grep":
		if v, ok := take("pattern", "query", "regex", "text", "search"); ok {
			setStr("pattern", v)
		}
		if v, ok := take("paths", "path", "files", "dirs", "roots"); ok {
			switch p := v.(type) {
			case string:
				obj["paths"] = []any{p}
			case []any:
				obj["paths"] = p
			default:
				obj["paths"] = v
			}
		}
		if v, ok := take("max_results", "limit", "n", "count"); ok {
			setInt("max_results", v)
		}
	case "get_tool_output":
		if v, ok := take("ref", "blob", "blob_ref", "id"); ok {
			setStr("ref", v)
		}
		if v, ok := take("offset", "from", "next_offset"); ok {
			setInt("offset", v)
		}
		if v, ok := take("limit", "n", "count"); ok {
			setInt("limit", v)
		}
	case "task_db":
		if v, ok := take("ops", "operations", "calls", "batch"); ok {
			switch o := v.(type) {
			case []any:
				obj["ops"] = o
			case map[string]any:
				obj["ops"] = []any{o}
			default:
				obj["ops"] = v
			}
		} else {
			// A single op object without the ops envelope.
			if opv, ok := obj["op"]; ok {
				if s, isStr := opv.(string); isStr {
					single := map[string]any{"op": s}
					for k, v := range obj {
						if k != "ops" && k != "op" {
							single[k] = v
						}
					}
					obj = map[string]any{"ops": []any{single}}
				}
			}
		}
	case "subagent":
		if v, ok := take("action", "op", "cmd", "operation"); ok {
			setStr("action", v)
			obj["action"] = strings.ToLower(strings.TrimSpace(asString(obj["action"])))
		}
		if v, ok := take("caller", "from", "agent", "agent_id"); ok {
			setStr("caller", v)
		}
		if v, ok := take("run", "run_id"); ok {
			setStr("run", v)
		}
		if v, ok := take("to", "target_agent", "recipient"); ok {
			setStr("to", v)
		}
		if v, ok := take("text", "message", "content", "instruction"); ok {
			setStr("text", v)
		}
		if v, ok := take("card", "delegation", "spec"); ok {
			obj["card"] = v
		}
	case "ack_directive":
		if v, ok := take("directive", "directive_id", "id", "message_id"); ok {
			setStr("directive", v)
		}
		if v, ok := take("note", "text", "message", "how"); ok {
			setStr("note", v)
		}
	case "clock":
		if v, ok := take("action", "op"); ok {
			setStr("action", v)
		}
		if v, ok := take("zone", "timezone", "tz"); ok {
			setStr("zone", v)
		}
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return json.RawMessage(trimmed)
	}
	return out
}

// callToolTopParams are the known top-level call_tool keys: the
// first-key wrap rule skips them and treats any other leading
// object-valued key as the tool name.
var callToolTopParams = map[string]bool{
	"name": true, "tool": true, "tool_name": true,
	"arguments": true, "args": true, "input": true,
	"parameters": true, "params": true,
}

// promoteCallToolName recovers the deferred tool name when the
// model did not pass a top-level name: first from name/tool/tool_name
// keys nested inside the arguments object (case-insensitive), then
// from a single-key wrap ({"toolname": {...}} reads the leading key
// in wire order as the name and its object as the arguments). The
// consumed key is removed so schema validation sees the canonical
// shape. It runs only under CallNameFlex; otherwise the schema
// error plus repair hint stand.
func promoteCallToolName(obj map[string]any, trimmed string) {
	if s, _ := obj["name"].(string); strings.TrimSpace(s) != "" {
		obj["name"] = strings.ToLower(strings.TrimSpace(s))
		return
	}
	delete(obj, "name")
	if sub, ok := obj["arguments"].(map[string]any); ok {
		for k, v := range sub {
			switch strings.ToLower(k) {
			case "name", "tool", "tool_name":
				if s, ok := getStrTolerant(v); ok && strings.TrimSpace(s) != "" {
					obj["name"] = strings.ToLower(strings.TrimSpace(s))
					delete(sub, k)
					if FrictionRepairEnabled() && len(sub) == 1 {
						if nested, ok := sub["arguments"].(map[string]any); ok {
							obj["arguments"] = nested
						}
					}
					return
				}
			}
		}
	}
	if _, ok := obj["name"]; ok {
		return
	}
	k, v, ok := firstTopKey(trimmed)
	if !ok || callToolTopParams[strings.ToLower(k)] {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(v, &m); err != nil || m == nil {
		return
	}
	obj["name"] = strings.ToLower(strings.TrimSpace(k))
	obj["arguments"] = m
	delete(obj, k)
}

// getStrTolerant renders scalar JSON values as strings for name
// promotion (numbers and booleans stringify, nil and composites do
// not).
func getStrTolerant(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

// firstTopKey returns the first top-level key of a JSON object in
// wire order with its raw value. Ok is false for non-objects,
// empty objects and malformed input.
func firstTopKey(raw string) (string, json.RawMessage, bool) {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	t, err := dec.Token()
	if err != nil {
		return "", nil, false
	}
	d, ok := t.(json.Delim)
	if !ok || d != '{' {
		return "", nil, false
	}
	t, err = dec.Token()
	if err != nil {
		return "", nil, false
	}
	k, ok := t.(string)
	if !ok {
		return "", nil, false
	}
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return "", nil, false
	}
	return k, v, true
}

func wrapBareString(name, s string) json.RawMessage {
	var obj map[string]any
	switch name {
	case "exec":
		obj = map[string]any{"command": s}
	case "search_tools", "search_skills":
		obj = map[string]any{"query": s}
	case "load_skill", "call_tool":
		obj = map[string]any{"name": s}
	case "read_file":
		obj = map[string]any{"path": s}
	case "glob", "grep":
		obj = map[string]any{"pattern": s}
	case "get_tool_output":
		obj = map[string]any{"ref": s}
	case "history_search":
		obj = map[string]any{"text": s}
	default:
		obj = map[string]any{}
	}
	raw, _ := json.Marshal(obj)
	return raw
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func splitEnvString(s string) []any {
	var out []any
	for _, part := range strings.Fields(s) {
		if strings.Contains(part, "=") {
			out = append(out, part)
		}
	}
	return out
}

// RepairHint describes the expected shape of a tool call after a
// validation failure: required properties, received keys and the
// aliases the tolerant parser accepts. It is appended to the
// "invalid arguments" output so a small model can repair the call
// instead of retrying it blindly.
func RepairHint(name string, raw json.RawMessage) string {
	shapes := map[string]string{
		"exec":                    `exec needs {"command": "<shell>"}; optional workdir, timeout_s (or yield_ms), tty, background. Chain with && and pipes in one call.`,
		"exec_io":                 `exec_io needs {"action": "<list|read|write|keys|wait_for|signal|close>", "session": "sess-N", ...}. Read live output with {"action":"read","session":"sess-N","offset":0}.`,
		"read_file":               `read_file needs {"path": "<file>"}; optional offset (1-based), limit.`,
		"write_file":              `write_file needs {"path": "<file>", "content": "<text>"}. For existing files pass the fp from read_file as expected_fp (or "*" to overwrite).`,
		"edit_file":               `edit_file needs {"path": "<file>", "old_text": "<exact fragment>", "new_text": "<replacement>"}. Copy old_text verbatim from read_file; on fingerprint mismatch re-read first.`,
		"apply_patch":             `apply_patch needs {"ops": [{"path": "<file>", "op": "<add|delete|update>", ...}]}.`,
		"grep":                    `grep needs {"pattern": "<regex>"}; optional paths[], literal:true for fixed strings.`,
		"glob":                    `glob needs {"pattern": "<**/*.go>"}; optional dir.`,
		"search_tools":            `search_tools needs {"query": "<verb object domain>"}; an empty query lists popular deferred tools.`,
		"call_tool":               `call_tool needs {"name": "<registered tool>", "arguments": {...}}, e.g. {"name":"lsp","arguments":{"path":"x"}}. The name may also sit inside arguments, or the call may wrap as {"toolname": {...}}. Core tools are directly available; discover deferred names with search_tools (an empty query lists some).`,
		"search_skills":           `search_skills needs {"query": "<text>"}; an empty query lists all skills.`,
		"load_skill":              `load_skill needs {"name": "<skill>"}; list names with search_skills first.`,
		"task_db":                 `task_db needs {"ops": [{"op": "<create_task|add_requirement|add_criterion|record_evidence|query|...>", ...}]}. Query first: {"ops":[{"op":"query","entity":"criteria"}]}.`,
		"subagent":                `subagent needs {"action": "<spawn|status|message|redirect|pause|resume|interrupt|cancel>", ...}; caller defaults to this run. Spawn runs record-only in single-run scope: prefer doing the work directly.`,
		"ack_directive":           `ack_directive needs nothing when no directives are pending; otherwise {"directive": "<msg-id>", "note": "<how it applies>"}.`,
		"get_tool_output":         `get_tool_output needs {"ref": "<blob-sha256:... or sess-N>", "offset":0, "limit":8000}.`,
		"history_search":          `history_search accepts {"text": "<substring>", "tool": "<name>", "k": 20}.`,
		"pin_observation":         `pin_observation needs {"observation": "<text>"} (id also accepted for observation).`,
		"leave_continuation_note": `leave_continuation_note needs {"note": "<next step>"} (text also accepted).`,
		"clock":                   `clock accepts {"action": "<now|convert|add>", "zone": "UTC"}.`,
		"status":                  `status takes no arguments.`,
	}
	shape, ok := shapes[name]
	if !ok {
		shape = name + ` called with invalid arguments; check the tool description.`
	}
	keys := receivedKeys(raw)
	if len(keys) == 0 {
		return "hint: " + shape
	}
	return "hint: " + shape + " received keys: " + strings.Join(keys, ", ") + "."
}

func receivedKeys(raw json.RawMessage) []string {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	return keys
}

// RetrievalHintForRef tells a model where a reference resolves: blob
// refs page through get_tool_output, sess-N ids through exec_io.
func RetrievalHintForRef(ref string) string {
	if strings.HasPrefix(ref, "sess-") {
		return fmt.Sprintf("unknown blob ref %q: that is a live terminal session, not a stored blob; read it with exec_io {\"action\":\"read\",\"session\":%q,\"offset\":0}", ref, ref)
	}
	return fmt.Sprintf("unknown blob ref %q: list recent blobs with get_tool_output paging or re-run the producing command", ref)
}

// FrictionRepairEnabled controls compatible tool routing repairs.
func FrictionRepairEnabled() bool { return !off("EXCALIBUR_TOOL_FRICTION_REPAIR") }
