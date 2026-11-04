package scopeguard

import "os"

// On Windows chmod maps only the owner-write bit to FILE_ATTRIBUTE_READONLY.
func normalizedFingerprintMode(mode os.FileMode, windows bool) os.FileMode {
	if windows {
		return mode.Perm() & 0200
	}
	return mode.Perm()
}
