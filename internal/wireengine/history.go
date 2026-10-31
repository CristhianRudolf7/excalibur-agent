package wireengine

import (
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/workspace"
)

// SharedHistory is the single edit history the file tools and the
// workspace deferred tools (file_history, revert_edit) observe
// together (Sections 13.7, 16.4). Edits through write_file, edit_file
// and apply_patch record here; file_history lists from here and
// revert_edit restores from here, so no edit is invisible to the
// revert path.
type SharedHistory struct {
	Tools *tools.MemoryHistory
	Work  *workspace.History
}

// NewSharedHistory returns a history pair with both stores empty.
// Either side may be nil only in tests that exercise one side.
func NewSharedHistory() *SharedHistory {
	return &SharedHistory{
		Tools: tools.NewMemoryHistory(),
		Work:  workspace.NewHistory(),
	}
}

// PutBlob implements tools.FileHistoryStore.
func (h *SharedHistory) PutBlob(data []byte) string {
	if h.Tools != nil {
		return h.Tools.PutBlob(data)
	}
	if h.Work != nil {
		return h.Work.PutBlob(data)
	}
	return ""
}

// Blob implements tools.FileHistoryStore.
func (h *SharedHistory) Blob(ref string) ([]byte, bool) {
	if h.Tools != nil {
		if b, ok := h.Tools.Blob(ref); ok {
			return b, true
		}
	}
	if h.Work != nil {
		return h.Work.Blob(ref)
	}
	return nil, false
}

// Append implements tools.FileHistoryStore. The version is recorded
// in both stores under the same path and timestamps so file_history
// (workspace side) and get_tool_output (tools side) agree.
func (h *SharedHistory) Append(v tools.FileVersion) {
	if h.Tools != nil {
		h.Tools.Append(v)
	}
	if h.Work != nil {
		pre, _ := h.Blob(v.Preimage)
		post, _ := h.Blob(v.Postimage)
		h.Work.RecordWithID(v.ID, filepath.Clean(v.Path), pre, post, v.RunID, v.ToolCall, v.At)
	}
}

// Versions implements tools.FileHistoryStore.
func (h *SharedHistory) Versions(path string) []tools.FileVersion {
	if h.Tools != nil {
		return h.Tools.Versions(path)
	}
	if h.Work != nil {
		vers := h.Work.Versions(path)
		out := make([]tools.FileVersion, 0, len(vers))
		for _, v := range vers {
			out = append(out, tools.FileVersion{
				ID: v.ID, Path: v.Path, Preimage: v.Preimage,
				Postimage: v.Postimage, RunID: v.RunID,
				ToolCall: v.ToolCall, At: v.At,
			})
		}
		return out
	}
	return nil
}

var _ tools.FileHistoryStore = (*SharedHistory)(nil)
