//go:build !linux && !darwin && !windows

package envsnap

// listeningPorts is a stub off Linux (Section P10): service
// enumeration needs /proc, so other platforms report none.
func listeningPorts() []string {
	return nil
}
