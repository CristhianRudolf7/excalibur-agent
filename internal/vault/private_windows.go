package vault

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func preparePrivateFile(path string) error  { return platform.RestrictToOwner(path) }
func restrictPrivateFile(path string) error { return platform.RestrictToOwner(path) }
func checkPassphrasePermissions(path string, fi os.FileInfo) error {
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("vault: passphrase file must be a regular file")
	}
	ok, err := platform.IsOwnerOnly(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("vault: passphrase file %q must grant access only to its owner, current user, SYSTEM or Administrators; fix with: icacls \"%s\" /inheritance:r /grant:r %%USERNAME%%:F", path, path)
	}
	return nil
}
func dirOf(path string) string { return filepath.Dir(path) }
