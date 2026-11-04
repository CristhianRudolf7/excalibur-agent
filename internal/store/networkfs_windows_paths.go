package store

import "strings"

// windowsNetworkVolume accepts a Win32 volume name and an injected drive probe
// so UNC, extended-length and mapped-drive handling can be tested on any OS.
func windowsNetworkVolume(volume string, driveType func(string) uint32) bool {
	name := strings.ToLower(strings.ReplaceAll(volume, "/", `\`))
	if strings.HasPrefix(name, `\\?\unc\`) || (strings.HasPrefix(name, `\\`) && !strings.HasPrefix(name, `\\?\`) && !strings.HasPrefix(name, `\\.\`)) {
		return true
	}
	if name == "" {
		return false
	}
	const driveRemote = 4 // Win32 DRIVE_REMOTE.
	return driveType(volume+`\`) == driveRemote
}
