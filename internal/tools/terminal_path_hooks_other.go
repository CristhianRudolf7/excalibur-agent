//go:build !linux && !darwin && !windows

package tools

import (
	"path"
	"strings"
)

func platformBoundaryPath(p string, harness []string) (bool, bool) { return false, false }
func boundaryCommandWords(command string) []string                 { return boundaryWords(command) }
func commandWordBase(word string) string                           { return path.Base(word) }

func commandStageWords(stage string) []string { return strings.Fields(stage) }
