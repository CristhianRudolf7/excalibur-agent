//go:build !linux

package envsnap

// listeningPorts is a stub off Linux (Section P10): service
// enumeration needs /proc, so other platforms report none.
func listeningPorts() []string {
	return nil
}
