package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
)

func controlEffect(kind string, check deliveryControlCheck) bool {
	var before, after any
	if json.Unmarshal(check.Before, &before) != nil || json.Unmarshal(check.After, &after) != nil || reflect.DeepEqual(before, after) {
		return false
	}
	// Restrict effects by the control's purpose: changing an input value is not
	// evidence that a search filters rows or that a detail view renders a record.
	switch kind {
	case "search", "filter":
		previous, okBefore := before.([]any)
		current, okAfter := after.([]any)
		return check.Effect == "rows" && okBefore && okAfter && len(current) < len(previous)
	case "detail":
		return check.Effect == "detail" && renderedDetail(after)
	case "add-item", "remove-item":
		previous, okBefore := before.([]any)
		current, okAfter := after.([]any)
		return check.Effect == "items" && okBefore && okAfter &&
			(kind == "add-item" && len(current) > len(previous) || kind == "remove-item" && len(current) < len(previous))
	case "field", "form":
		return check.Effect == "form" && formState(before) && formState(after) ||
			kind == "form" && check.Effect == "records" && recordState(before) && recordState(after)
	case "link":
		return check.Effect == "route" && routeState(before) && routeState(after)
	case "button", "submit":
		switch check.Effect {
		case "rows", "items":
			_, okBefore := before.([]any)
			_, okAfter := after.([]any)
			return okBefore && okAfter
		case "records":
			return recordState(before) && recordState(after)
		case "form":
			return formState(before) && formState(after)
		case "detail":
			return renderedDetail(after)
		case "route":
			return routeState(before) && routeState(after)
		case "dialog":
			_, okBefore := before.(bool)
			_, okAfter := after.(bool)
			return okBefore && okAfter
		}
	}
	return false
}

func routeState(value any) bool {
	route, ok := value.(string)
	return ok && (strings.HasPrefix(route, "/") || strings.HasPrefix(route, "http://") || strings.HasPrefix(route, "https://"))
}

func recordState(value any) bool {
	switch state := value.(type) {
	case []any:
		return true
	case map[string]any:
		return len(state) > 0 && state["notice"] == nil && state["message"] == nil && state["toast"] == nil
	}
	return false
}

func formState(value any) bool {
	state, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, values := state["values"].(map[string]any)
	_, errors := state["errors"].(map[string]any)
	return (values || errors) && state["notice"] == nil && state["message"] == nil && state["toast"] == nil
}

func renderedDetail(value any) bool {
	state, ok := value.(map[string]any)
	if !ok || state["visible"] != true {
		return false
	}
	record, ok := state["record"].(map[string]any)
	return ok && len(record) > 0
}
