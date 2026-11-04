//go:build !windows

package tasks

import (
	"path/filepath"
	"strings"
)

func absolutePathPattern() string {
	return `/[A-Za-z0-9_.\-]+(?:/[A-Za-z0-9_.\-]+)+`
}

func deliverableRoots() []string {
	return []string{"/app", "/home", "/tmp", "/root", "/srv", "/opt", "/data", "/workspace", "/output", "/var/www"}
}

func snapshotFilesystemRoot(root string) bool { return root == "" || filepath.Dir(root) == root }

func draftPathBase(key string) string {
	base := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		base = key[i+1:]
	}
	return base
}

func absolutePathIndices(text string) [][]int { return absPathRe.FindAllStringIndex(text, -1) }

func dependencyPathPattern() string           { return `[a-zA-Z0-9_./-]+\.[a-zA-Z0-9_.-]+|/[a-zA-Z0-9_./-]+` }
func evidencePathIdentity(path string) string { return filepath.Clean(path) }

func rootPathPrefix(path, root string) bool { return path == root || strings.HasPrefix(path, root+"/") }

func nativeExecutableExercise(string) bool { return false }
func nativeObservationProgram(string) bool { return false }
func commandPathBase(program string) string {
	prog := program
	if i := strings.LastIndex(prog, "/"); i >= 0 {
		prog = prog[i+1:]
	}
	return prog
}

func nativeExecutableProgram(string) bool { return false }
