package tasks

import (
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// These fixtures use a simulated filesystem; Windows still needs a volume
// for the hard gate's absolute-path contract.
func nativeFixtureText(text string) string {
	if runtime.GOOS != "windows" {
		return text
	}
	return regexp.MustCompile(`(^|[\s"'=:])(/(?:app|tmp|usr|opt|work))`).ReplaceAllString(text, "${1}C:${2}")
}

func nativeFixtureRequirements(reqs []ArtifactRequirement) []ArtifactRequirement {
	for i := range reqs {
		reqs[i].Path = nativeFixtureText(reqs[i].Path)
		reqs[i].Source = nativeFixtureText(reqs[i].Source)
		reqs[i].sentence = nativeFixtureText(reqs[i].sentence)
	}
	return reqs
}

func fixturePOSIXPath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.TrimPrefix(filepath.ToSlash(path), filepath.VolumeName(path))
	}
	return path
}
