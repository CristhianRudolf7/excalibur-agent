package engine

import (
	"regexp"
	"runtime"
)

// Synthetic filesystem histories use explicit drive-qualified paths on Windows.
func nativeFixtureText(text string) string {
	if runtime.GOOS != "windows" {
		return text
	}
	return regexp.MustCompile(`(^|[\s"'=:])(/(?:app|tmp|work))`).ReplaceAllString(text, "${1}C:${2}")
}
