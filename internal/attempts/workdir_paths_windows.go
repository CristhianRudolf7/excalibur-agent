package attempts

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func sameWorkdir(real, attempt string) bool {
	return strings.EqualFold(filepath.Clean(real), filepath.Clean(attempt))
}

func commandPathIndex(command, real string) int {
	pattern := regexp.QuoteMeta(filepath.ToSlash(real))
	pattern = strings.ReplaceAll(pattern, "/", `[\\/]`)
	match := regexp.MustCompile("(?i)" + pattern).FindStringIndex(command)
	if match == nil {
		return -1
	}
	return match[0]
}

func resolveAttemptPath(abs string) string { return platform.CanonicalPath(abs) }
