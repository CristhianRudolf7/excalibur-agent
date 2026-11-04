//go:build !windows

package vault

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func assertLoosePassphraseRejected(t *testing.T, dir, loose string) {
	t.Helper()
	ctx := context.Background()
	if _, err := Open(ctx, filepath.Join(dir, "v2.db"), Options{PassphraseFile: loose}); err == nil {
		t.Fatal("0644 passphrase file must be rejected")
	} else if !strings.Contains(err.Error(), "0600") {
		t.Fatalf("wrong error: %v", err)
	}
}
