package attempts

import (
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

// DeliverableRequirements returns the named outputs of the request that
// the deliverable checks evaluate: paths, executables and outputs. The
// extraction is the completion gate's own (tasks.ExtractArtifactRequirements),
// so an attempt is selected on the same outputs the gate asks for. Ports
// and URLs need the agent's tool history and stay with the engine gate.
func DeliverableRequirements(request string) []tasks.ArtifactRequirement {
	cfg := tasks.DefaultArtifactConfig()
	var out []tasks.ArtifactRequirement
	for _, r := range tasks.ExtractArtifactRequirements(request, cfg.MaxArtifacts) {
		switch r.Kind {
		case tasks.ArtifactPath, tasks.ArtifactExecutable, tasks.ArtifactOutput:
			out = append(out, r)
		}
	}
	return out
}

// CheckDeliverables evaluates the named outputs against one attempt's
// copy. Absolute paths under the real directory are mapped onto the
// copy through m; relative paths resolve under dir. It returns one
// pending message per missing or unfinished output, phrased with the
// path the request used.
func CheckDeliverables(reqs []tasks.ArtifactRequirement, m WorkdirMap, dir string) []string {
	var pendings []string
	for _, r := range reqs {
		mapped := r
		if filepath.IsAbs(r.Path) {
			mapped.Path = m.Path(r.Path)
		}
		for _, msg := range tasks.CheckArtifactFiles([]tasks.ArtifactRequirement{mapped}, nil, dir, nil, nil) {
			if mapped.Path != r.Path {
				msg = strings.ReplaceAll(msg, mapped.Path, r.Path)
			}
			pendings = append(pendings, msg)
		}
	}
	return pendings
}
