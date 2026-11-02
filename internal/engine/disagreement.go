package engine

import "github.com/CristhianRudolf7/excalibur-agent/internal/tasks"

type disagreementSnapshot struct {
	request string
	files   map[string]string
}

// Called only when independent review refuses an otherwise passing finish.
// Keep versions only of deliverables with successful exercising evidence.
func (e *Engine) rememberDisagreement(r *Run, history []tasks.HistoryTool) {
	if !tasks.DisagreementProtocolEnabled() {
		return
	}
	r.mu.Lock()
	request, report := r.verificationRequestLocked(), r.finishVerifierReport
	r.mu.Unlock()
	if !report.explicit || !report.failed {
		return
	}
	files := map[string]string{}
	for _, d := range e.deliverables(r, request, history) {
		lastEdit := lastEditOf(history, d)
		for i, h := range history {
			if i < lastEdit || (i == lastEdit && !tasks.EntryVerifiesChanges(h)) || !tasks.IsExercisingCall(h) || !mentionsFile(h.Args, d.Base) {
				continue
			}
			if version := fileVersion(d.Path); version != "missing" && version != "unreadable" && version != "broken-link" {
				files[d.Path] = version
			}
			break
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishDisagreement = disagreementSnapshot{request: request, files: files}
}

func (r *Run) disagreementRewriteNote() string {
	if !tasks.DisagreementProtocolEnabled() {
		return ""
	}
	r.mu.Lock()
	snapshot, request := r.finishDisagreement, r.verificationRequestLocked()
	r.mu.Unlock()
	if snapshot.request != request {
		return ""
	}
	for path, version := range snapshot.files {
		if fileVersion(path) != version {
			return "deliverable changed after a disagreement; confirm with concrete samples which version follows the request"
		}
	}
	return ""
}
