//go:build !windows

package tools

import "testing"

func prepareDetachedServerCleanup(*testing.T, string) {}
