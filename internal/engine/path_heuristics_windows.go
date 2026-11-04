package engine

import (
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

func fileOperandPattern() string {
	return `[a-zA-Z]:[\\/][a-zA-Z0-9_.~\\/-]+|\\\\[a-zA-Z0-9_.-]+[\\/][a-zA-Z0-9_.~\\/-]+|[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+|/[a-zA-Z0-9_./-]+`
}

func executableProgram(path string, _ os.FileMode) bool {
	info, err := tasks.DefaultStat(path)
	return err == nil && info.Executable
}

func fileTextIdentity(text string) string { return strings.ToLower(strings.ReplaceAll(text, `\`, "/")) }
func sameArtifactPath(a, b string) bool {
	return platform.CanonicalPath(a) == platform.CanonicalPath(b)
}

func canonicalTrialIdentities(files trialFiles) trialFiles {
	out := make(trialFiles, len(files))
	for path, body := range files {
		out[platform.CanonicalPath(path)] = body
	}
	return out
}
