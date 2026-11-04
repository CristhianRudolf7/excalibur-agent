package vault

import (
	"fmt"
	"os"
	"strings"
)

func preparePrivateFile(string) error       { return nil }
func restrictPrivateFile(path string) error { return os.Chmod(path, 0o600) }
func checkPassphrasePermissions(path string, fi os.FileInfo) error {
	if fi.Mode().Perm() != 0o600 {
		return fmt.Errorf("vault: passphrase file %q must be 0600 (has %o)", path, fi.Mode().Perm())
	}
	return nil
}
func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}
