//go:build !linux

package cpupool

// lowerCurrentThread is a no-op outside Linux: thread priorities are a
// future implementation behind the platform interfaces (Section 3.4).
func lowerCurrentThread() error { return nil }
