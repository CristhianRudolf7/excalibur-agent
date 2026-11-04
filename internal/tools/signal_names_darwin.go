package tools

// macOS numbers several signals differently from Linux. Correct the
// shared Linux table so 128+n exit statuses name the right signal.
func init() {
	for n, name := range darwinSignalNames {
		signalNames[n] = name
	}
}

// darwinSignalNames holds the BSD numbering that differs from Linux.
var darwinSignalNames = map[int]string{
	7: "SIGEMT", 10: "SIGBUS", 12: "SIGSYS", 30: "SIGUSR1", 31: "SIGUSR2",
}
