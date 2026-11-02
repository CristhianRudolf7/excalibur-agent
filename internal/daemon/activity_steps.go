package daemon

import (
	"encoding/json"
	"strings"
)

func (b *DaemonBackend) activitySpanish(conv string) bool {
	if b.Master != nil {
		rows := b.Master.TurnHistory(conv, 0)
		for i := len(rows) - 1; i >= 0; i-- {
			if rows[i].Role == "user" || rows[i].Role == "" {
				return looksSpanish(rows[i].Text)
			}
		}
	}
	return false
}

// activityStep uses only the live call's descriptive arguments, never file
// contents or tool output, which may contain sensitive or unbounded text.
func activityStep(state, failure, tool, args string, spanish bool) string {
	local := func(en, es string) string {
		if spanish {
			return es
		}
		return en
	}
	switch state {
	case "completed":
		return local("Completed", "Completado")
	case "failed":
		if failure != "" {
			return failure
		}
		return local("Failed", "Error")
	case "cancelled":
		return local("Cancelled", "Cancelado")
	}
	var fields struct {
		Path      string          `json:"path"`
		Command   string          `json:"command"`
		Query     string          `json:"query"`
		Pattern   string          `json:"pattern"`
		Tool      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Ops       []struct {
			Path string `json:"path"`
		} `json:"ops"`
	}
	_ = json.Unmarshal([]byte(args), &fields)
	if tool == "call_tool" && fields.Tool != "" && fields.Tool != "call_tool" {
		return activityStep(state, failure, fields.Tool, string(fields.Arguments), spanish)
	}
	detail := func(prefix, value string) string {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			return prefix + "…"
		}
		return prefix + " " + boundedTurnText(value, 60)
	}
	switch tool {
	case "write_file", "edit_file", "apply_patch":
		path := fields.Path
		if path == "" && len(fields.Ops) > 0 {
			path = fields.Ops[0].Path
		}
		return detail(local("Editing", "Editando"), path)
	case "read_file":
		return detail(local("Reading", "Leyendo"), fields.Path)
	case "terminal", "exec", "exec_io", "exec_command", "terminal_exec", "terminal_start":
		return detail(local("Running", "Ejecutando"), fields.Command)
	case "grep", "glob", "search", "search_tools", "library_search", "library_grep", "memory_search", "history_search":
		query := fields.Query
		if query == "" {
			query = fields.Pattern
		}
		return detail(local("Searching", "Buscando"), query)
	}
	if tool == "source_search" || strings.Contains(tool, "web") || strings.Contains(tool, "browser") {
		return local("Browsing the web", "Consultando la web")
	}
	return local("Thinking…", "Pensando…")
}
