//go:build darwin

package engine

// macOS has no inotify-style file read notifications. FSEvents reports
// changes, not reads, so it cannot provide complete dynamic dependencies.
// Keep tracking inert rather than claim that an empty receipt is complete.
type dependencyMonitor struct{}

func startDependencyMonitor(string) *dependencyMonitor { return nil }
func (*dependencyMonitor) finish() ([]string, bool)    { return nil, false }
