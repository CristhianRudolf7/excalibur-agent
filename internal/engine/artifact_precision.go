package engine

import (
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// File-name drafts inherit the same precision rule as filesystem checks.
// Otherwise the history-only verifier could hard-block an advisory prose name.
func (e *Engine) hardArtifactDraft(run *Run, request string, d tasks.DraftCriterion) bool {
	if !tasks.StrictArtifactGateEnabled() || d.Key == "" {
		return true
	}
	pathDraft := strings.HasPrefix(d.Source, "path ") || strings.HasPrefix(d.Source, "file ") || strings.HasPrefix(d.Source, "quoted ") || d.Source == "refined"
	if !pathDraft || strings.ContainsAny(d.Key, " \n") || (!strings.Contains(d.Key, "/") && filepath.Ext(d.Key) == "") {
		return true
	}
	for _, a := range tasks.ExtractArtifactRequirementsIn(request, e.artifactConfig().MaxArtifacts, run.Workdir) {
		if a.Path != "" && tasks.HardArtifactRequirement(a) && (strings.EqualFold(a.Path, d.Key) || strings.EqualFold(filepath.Base(a.Path), d.Key)) {
			return true
		}
	}
	e.noteAdvisory(run, "artifact mention (advisory): "+d.Key)
	return false
}
