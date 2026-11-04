package platform

import "os"

// RestrictToOwner applies private POSIX permissions.
func RestrictToOwner(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if st.IsDir() {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}

// IsOwnerOnly checks private POSIX permissions.
func IsOwnerOnly(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	mode := os.FileMode(0o600)
	if st.IsDir() {
		mode = 0o700
	}
	return st.Mode().Perm() == mode, nil
}
