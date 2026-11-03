package cli

import (
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/robotlab"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

// embeddedRobotLab owns a lazy runtime for the lifetime of one embedded run.
// It shares the installed environment with the daemon and never installs it.
func embeddedRobotLab(profile ProfileName, media *tools.MediaDeps) (*robotlab.Tool, func(), error) {
	if profile == ProfileEval || !robotlab.Enabled() {
		return nil, func() {}, nil
	}
	dir, err := browserDataDir()
	if err != nil {
		return nil, func() {}, err
	}
	if home := os.Getenv("EXCALIBUR_HOME"); home != "" {
		dir = filepath.Join(home, "state")
	}
	service := robotlab.NewRuntimeService(dir, nil)
	var asker robotlab.VisionAsker
	if media != nil {
		asker, _ = media.Vision.(robotlab.VisionAsker)
	}
	tool := robotlab.NewTool(service.Sidecar, service, asker, &service.VisionStep)
	return tool, func() { _ = service.Close() }, nil
}
