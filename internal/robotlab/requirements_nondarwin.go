//go:build !darwin

package robotlab

// Keep non-macOS installer commands and messages identical.
func prepareRuntimeRequirements(path string, data []byte) (string, []byte, error) {
	return path, data, nil
}
func torchInstallArgs(pin string) []string {
	return []string{"-m", "pip", "install", "--index-url", "https://download.pytorch.org/whl/cpu", pin}
}
func runtimeUnavailableOutput() string {
	return `{"status":"unavailable","detail":"Robot Lab is unavailable. Enable EXCALIBUR_ROBOTLAB and use the Robot Lab Install card to install Python dependencies and OSMesa; installation requires an explicit user action."}`
}
