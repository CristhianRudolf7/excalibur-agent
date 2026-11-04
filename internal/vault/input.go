package vault

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"
)

// randRead fills b with crypto randomness.
func randRead(b []byte) (int, error) { return rand.Read(b) }

// writePrivateFile writes data with 0600, creating parents with 0700.
func writePrivateFile(path string, data []byte) error {
	if dir := dirOf(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := preparePrivateFile(path); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Enforce 0600 against umask surprises and pre-existing files.
	return restrictPrivateFile(path)
}

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

// LoadPassphraseFile reads an unattended-unlock passphrase file. The
// file must have 0600 permissions and explicit config opt-in (the
// caller passes Options.PassphraseFile); the content is zeroed by the
// caller after Unlock. Values are never CLI arguments, so they never
// land in shell history (Section 20.1).
func LoadPassphraseFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := checkPassphrasePermissions(path, fi); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pp := []byte(strings.TrimRight(string(raw), "\r\n"))
	zero(raw)
	if len(pp) == 0 {
		return nil, fmt.Errorf("vault: empty passphrase file %q", path)
	}
	return pp, nil
}

// ReadValueStdin reads a secret value from standard input (the
// --stdin path of `vault set`): never a CLI argument.
func ReadValueStdin(data []byte) ([]byte, error) {
	cp := append([]byte(nil), data...)
	// Strip one trailing newline; values are otherwise literal.
	cp = []byte(strings.TrimSuffix(string(cp), "\n"))
	cp = []byte(strings.TrimSuffix(string(cp), "\r"))
	if len(cp) == 0 {
		return nil, fmt.Errorf("vault: empty value on stdin")
	}
	return cp, nil
}
