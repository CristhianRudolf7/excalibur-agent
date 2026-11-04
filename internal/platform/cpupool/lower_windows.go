package cpupool

import "github.com/CristhianRudolf7/excalibur-agent/internal/platform"

// lowerCurrentThread lowers only this worker's Windows thread priority. Each
// worker holds runtime.LockOSThread while executing its tasks (Section 3.2).
func lowerCurrentThread() error { return (platform.WindowsProcessPriority{}).LowerCurrentThread() }
