package engine

type dependencyMonitor struct{}

func startDependencyMonitor(string) *dependencyMonitor { return nil }
func (*dependencyMonitor) finish() ([]string, bool)    { return nil, false }
