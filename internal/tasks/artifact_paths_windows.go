package tasks

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func absolutePathPattern() string {
	return `[A-Za-z]:[\\/][A-Za-z0-9_.~\\/-]+|\\\\[A-Za-z0-9_.-]+[\\/][A-Za-z0-9_.~\\/-]+|/[A-Za-z0-9_.\-]+(?:/[A-Za-z0-9_.\-]+)+`
}

func deliverableRoots() []string {
	roots := []string{"/app", "/home", "/tmp", "/root", "/srv", "/opt", "/data", "/workspace", "/output", "/var/www"}
	volume := filepath.VolumeName(os.TempDir())
	for _, root := range append([]string(nil), roots...) {
		roots = append(roots, volume+root)
	}
	roots = append(roots, os.TempDir())
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
	}
	return roots
}

func snapshotFilesystemRoot(root string) bool {
	return root == "" || filepath.Dir(filepath.Clean(root)) == filepath.Clean(root)
}

func draftPathBase(key string) string { return filepath.Base(key) }

// Quoting permits spaces in native paths without treating following prose as
// part of an unquoted path. Keep the original text offsets and request order.
func absolutePathIndices(text string) [][]int {
	var quoted [][]int
	for _, groups := range quotedRe.FindAllStringSubmatchIndex(text, -1) {
		for i := 2; i < len(groups); i += 2 {
			if groups[i] >= 0 && filepath.IsAbs(text[groups[i]:groups[i+1]]) {
				quoted = append(quoted, []int{groups[i], groups[i+1]})
			}
		}
	}
	out := append([][]int(nil), quoted...)
	for _, loc := range absPathRe.FindAllStringIndex(text, -1) {
		overlaps := false
		for _, q := range quoted {
			if loc[0] < q[1] && q[0] < loc[1] {
				overlaps = true
				break
			}
		}
		if !overlaps {
			out = append(out, loc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func dependencyPathPattern() string {
	return absolutePathPattern() + `|[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+`
}
func evidencePathIdentity(path string) string {
	if !filepath.IsAbs(path) {
		return strings.ToLower(filepath.Clean(path))
	}
	return platform.CanonicalPath(path)
}

func rootPathPrefix(path, root string) bool {
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(root)), strings.ToLower(filepath.Clean(path)))
	return err == nil && (rel == "." || filepath.IsLocal(rel))
}

func commandPathBase(program string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(program)), ".exe")
}

func nativeExecutableExercise(segment string) bool {
	words := ShellWords(segment)
	if len(words) == 0 {
		return false
	}
	return nativeExecutableProgram(words[0])
}

func nativeObservationProgram(program string) bool {
	switch program {
	case "type", "dir", "where", "get-content", "get-item", "get-childitem", "test-path", "write-output", "set-location":
		return true
	}
	return false
}

func nativeExecutableProgram(program string) bool {
	return windowsExecutableExtension(program, os.Getenv("PATHEXT"))
}
