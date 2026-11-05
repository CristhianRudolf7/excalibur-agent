//go:build !darwin

package robotlab

// Keep non-macOS installer commands and messages identical.
func prepareRuntimeRequirements(path string, data []byte) (string, []byte, error) {
	return path, data, nil
}
