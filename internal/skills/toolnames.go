package skills

// KnownTool reports whether name is a tool the harness defines:
// an H3 built-in, a Section 13.2 core tool or a Section 13.3
// deferred tool. The install and index validators use it to
// cross-check metadata.excalibur.tools and allowed-tools.
func KnownTool(name string) bool {
	_, ok := knownTools[name]
	return ok
}

// KnownTools returns the sorted known tool names.
func KnownTools() []string {
	out := make([]string, 0, len(knownTools))
	for n := range knownTools {
		out = append(out, n)
	}
	return out
}

var knownTools = map[string]bool{
	// H3 built-ins (files, terminal, control).
	"read_file": true, "write_file": true, "edit_file": true, "apply_patch": true,
	"grep": true, "glob": true, "exec": true, "exec_io": true,
	"clock": true, "get_tool_output": true, "status": true,
	// Section 13.2 core tools.
	"send_message": true, "ask_user": true, "leave_continuation_note": true, "ack_directive": true,
	"settings": true, "web_search": true, "web_read": true, "task_db": true,
	"memory_search": true, "memory_write": true, "memory_propose": true,
	"history_search": true, "library_search": true, "doc_read": true, "doc_edit": true,
	"pin_observation": true, "search_skills": true, "load_skill": true,
	"search_tools": true, "call_tool": true, "subagent": true, "reminders": true, "automations": true,
	// Section 13.3 deferred tools: code and workspace.
	"lsp": true, "code_graph": true, "code_diagram": true, "file_history": true, "revert_edit": true,
	"workspace_snapshot": true, "worktree": true, "integration_queue": true, "github": true,
	// Personal-agent appearance.
	"design_avatar": true,
	// Browser (contract 14.2).
	"robot_lab":            true,
	"whiteboard":           true,
	"browser_open_session": true, "browser_close_session": true, "browser_suspend_session": true,
	"browser_tabs": true, "browser_navigate": true, "browser_wait_ready": true,
	"browser_snapshot": true, "browser_act": true, "browser_download": true,
	"browser_devtools": true, "browser_emulate": true, "browser_status": true,
	"browser_routine": true,
	// Information sources.
	"source_search": true,
	// Documents, files and media.
	"document_read": true, "document_create": true, "document_edit": true,
	"pdf_ops": true, "file_convert": true, "library_ingest": true,
	"vision_extract": true, "audio_transcribe": true,
	"diagram_render": true, "chart_render": true, "image_generate": true, "image_edit": true,
	// User databases.
	"userdb": true,
	// Communication and services.
	"email": true, "calendar": true, "contact_message": true, "meeting": true,
	// Device control (14.6).
	"device_screen_capture": true, "device_ui_tree": true, "device_input": true, "device_app": true,
	// Apps with contextual agents (14.8).
	"app_state": true, "spreadsheet_ops": true, "model3d_ops": true,
	"video_timeline_ops": true, "design_canvas_ops": true, "whiteboard_ops": true,
	"classroom": true, "course_ops": true, "job_tracker_ops": true, "monitor_ops": true,
	"simulation_ops": true, "bi_ops": true, "gpu_training": true,
	// Agenda and automation (Section 21).
	"schedule": true, "trigger_rules": true, "workflow": true, "flow_simulate": true,
	// Administration and extensions.
	"secrets": true, "metrics_query": true, "nodes": true,
	"skill_manage": true, "tool_package": true,
	"package_install": true, "package_publish": true,
}
