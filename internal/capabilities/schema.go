package capabilities

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ObjSchema builds a minimal JSON Schema 2020-12 object schema from
// property name to type. It keeps capability manifests readable
// without a schema builder dependency.
func ObjSchema(required []string, props map[string]string) json.RawMessage {
	properties := map[string]any{}
	for name, typ := range props {
		properties[name] = map[string]any{"type": typ}
	}
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return json.RawMessage(`{"type":"object"}`)
	}
	return raw
}

// AnySchema is the permissive object schema for free-form arguments.
func AnySchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

// Op builds an Operation with the discovery hints search_tools needs
// (verb, object, domain) and the Section 13.1 execution fields.
func Op(name, desc, effect, idem, cancel string, fast bool, group string, keywords []string, schema json.RawMessage) Operation {
	return Operation{
		Name: name, Description: desc,
		InputSchema: schema,
		Effect:      effect, Idempotency: idem, Cancel: cancel, Fast: fast,
		Platforms: []string{"linux"}, Profiles: []string{"default", "delegate", "eval"},
		Group: group, Tags: []string{group}, Keywords: keywords,
	}
}

// KeyDedup records first-effect executions per idempotency key so
// repeats with the same key never repeat effects (Section 14.1:
// the harness always sends a key for operations with effects).
type KeyDedup struct {
	mu   sync.Mutex
	seen map[string]bool
}

// First reports whether key is new. Empty keys always count as new:
// callers must supply a key for effectful operations.
func (d *KeyDedup) First(key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if key == "" {
		return true
	}
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	if d.seen[key] {
		return false
	}
	d.seen[key] = true
	return true
}

// Completed builds a terminal completed task result with a JSON payload.
func Completed(taskID string, payload any) TaskResult {
	raw, err := json.Marshal(payload)
	if err != nil {
		return TaskResult{TaskID: taskID, State: StateCompleted, Payload: json.RawMessage(`{"ok":true}`)}
	}
	return TaskResult{TaskID: taskID, State: StateCompleted, Payload: raw}
}

// Failed builds a terminal failed task result with an error class for
// the Section 17.7 retry policy.
func Failed(taskID, text string, class ErrorClass) TaskResult {
	return TaskResult{TaskID: taskID, State: StateFailed, ErrorText: text, ErrClass: class}
}

// argString reads one string argument.
func argString(args json.RawMessage, name string) string {
	var decoded map[string]any
	if err := json.Unmarshal(args, &decoded); err != nil {
		return ""
	}
	s, _ := decoded[name].(string)
	return s
}

// nextID mints deterministic task ids per operation for restart-safe
// polling (Section 14.1: the harness persists ids and resumes by id).
func nextID(prefix string, n *int) string {
	*n++
	return fmt.Sprintf("%s-%d", prefix, *n)
}
