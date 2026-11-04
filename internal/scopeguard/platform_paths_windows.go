package scopeguard

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func originalsDir() string                         { return filepath.Join(os.TempDir(), "excalibur-originals") }
func fingerprintMode(mode os.FileMode) os.FileMode { return normalizedFingerprintMode(mode, true) }
func inputExecutable(os.FileMode) bool             { return false }
func backupRelativePath(path string) string {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	relative := strings.TrimLeft(strings.TrimPrefix(clean, volume), `\/`)
	// Drive letters and UNC shares are not valid relative directory names.
	// Keep distinct volumes separate without introducing ':' into backup paths.
	sum := sha256.Sum256([]byte(strings.ToLower(volume)))
	return filepath.Join(fmt.Sprintf("volume-%x", sum[:8]), relative)
}

func init() {
	// Instructions may spell paths with either Windows or slash separators,
	// and 8.3 short names (RUNNER~1) contain a tilde.
	// Keep the shared POSIX expressions unchanged on other platforms.
	paths = regexp.MustCompile(`(?:[A-Za-z]:[/\x5c]|\x5c\x5c|[/\x5c]|\.{1,2}[/\x5c])[A-Za-z0-9_@.*~-]+(?:[/\x5c][A-Za-z0-9_@.*~-]+)*|[A-Za-z0-9_@*~-]+(?:[/\x5c][A-Za-z0-9_@.*~-]+)+|[A-Za-z0-9_@*~-]+\.[A-Za-z0-9_@.*~-]+`)
	bareLocation = regexp.MustCompile("(?i)(?:\\b(?:modify|delete|change|edit|touch|write|create)(?:\\s+only)?|\\bleave)\\s+(?:the\\s+(?:file|directory)\\s+)?[`\"']?([A-Za-z0-9_./*:~\\\\-]+)")
	checkPath = regexp.MustCompile(`(?i)(?:^|[/\x5c_.-])(?:tests?|checks?|verify|verification|spec)(?:[/\x5c_.-]|$)`)
}
