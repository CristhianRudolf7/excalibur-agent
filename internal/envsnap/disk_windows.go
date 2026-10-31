//go:build windows

package envsnap

// detectDiskFree is unsupported on Windows (Section P10: Linux only,
// other OSes via stubs); absence, not an error.
func detectDiskFree(dir string) (int64, bool) {
	return 0, false
}
