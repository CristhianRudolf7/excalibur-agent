//go:build !windows

package engine

import (
	"os"
	"path/filepath"
)

func fileOperandPattern() string {
	return `[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+|/[a-zA-Z0-9_./-]+`
}

func executableProgram(_ string, mode os.FileMode) bool { return mode.Perm()&0111 != 0 }

func fileTextIdentity(text string) string { return text }
func sameArtifactPath(a, b string) bool   { return filepath.Clean(a) == filepath.Clean(b) }

func canonicalTrialIdentities(files trialFiles) trialFiles { return files }
