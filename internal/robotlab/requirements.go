package robotlab

import "path/filepath"

func detectOSMesa() bool {
	if !needsOSMesa() {
		return true
	}
	for _, pattern := range []string{"/lib/libOSMesa.so*", "/lib64/libOSMesa.so*", "/usr/lib/libOSMesa.so*", "/usr/lib64/libOSMesa.so*", "/lib/*-linux-gnu/libOSMesa.so*", "/usr/lib/*-linux-gnu/libOSMesa.so*", "/usr/local/lib/libOSMesa.so*"} {
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return true
		}
	}
	// No dlopen or execution of an untrusted library is needed.
	return false
}
