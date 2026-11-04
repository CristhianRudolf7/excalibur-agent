package scopeguard

import (
	"os"
	"testing"
)

func TestFingerprintPermissionNormalization(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0644, 0666, 0700, 0755, 0777} {
		if got := normalizedFingerprintMode(mode, true); got != 0200 {
			t.Fatalf("writable mode %o became %o", mode, got)
		}
		if got := normalizedFingerprintMode(mode, false); got != mode {
			t.Fatalf("Unix mode %o became %o", mode, got)
		}
	}
	for _, mode := range []os.FileMode{0400, 0444, 0555} {
		if got := normalizedFingerprintMode(mode, true); got != 0 {
			t.Fatalf("read-only mode %o became %o", mode, got)
		}
	}
}
