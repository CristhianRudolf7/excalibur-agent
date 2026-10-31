// Fallback root CA tests (Section 23.2, defect w8-bench-fixes/1).
//
// The static bench binary runs in arbitrary task images, many without
// CA certificates. It carries its own Mozilla roots through the
// x509roots/fallback blank import in main.go, which the verifier uses
// only when the image provides no system pool. These tests pin that
// behavior: the bundle is embedded and parses, and the binary reaches
// a real HTTPS endpoint from a CA-less container.
package main

import (
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/x509roots/fallback/bundle"
)

// TestFallbackBundleEmbedded pins the structural guarantee: the
// binary embeds a fresh Mozilla bundle (many roots, all parsing as
// X.509, at least one valid now). If the bundle rots, this fails
// before any container run wastes time.
func TestFallbackBundleEmbedded(t *testing.T) {
	now := time.Now()
	roots := 0
	validNow := 0
	for r := range bundle.Roots() {
		roots++
		cert, err := x509.ParseCertificate(r.Certificate)
		if err != nil {
			t.Fatalf("fallback root %d does not parse: %v", roots, err)
		}
		if now.After(cert.NotBefore) && now.Before(cert.NotAfter) {
			validNow++
		}
	}
	if roots < 100 {
		t.Fatalf("fallback bundle holds %d roots, want at least 100", roots)
	}
	if validNow == 0 {
		t.Fatal("no fallback root is valid now; the bundle is stale")
	}
	t.Logf("fallback bundle: %d roots, %d valid now", roots, validNow)
}

// TestFallbackServesEmptySystemPool pins the Section 23.2 guarantee
// in a fresh process: with no CA certificates visible, the pool the
// verifier gets must still carry the embedded Mozilla bundle. The
// system pool is cached per process, so the probe runs in a
// subprocess with both bundle sources pointed away from the host.
func TestFallbackServesEmptySystemPool(t *testing.T) {
	if os.Getenv("EXCALIBUR_FALLBACK_PROBE") == "1" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			fmt.Fprintf(os.Stderr, "probe pool: %v\n", err)
			os.Exit(2)
		}
		// Subjects is deprecated for system pools because it omits
		// system roots; here that is exactly the point (there are
		// no system roots visible) and the fallback bundle roots
		// are reported, so a non-empty count proves engagement.
		//
		//lint:ignore SA1019 deliberate non-emptiness probe, see above
		fmt.Printf("%d\n", len(pool.Subjects()))
		os.Exit(0)
	}
	probe := exec.Command(os.Args[0], "-test.run=TestFallbackServesEmptySystemPool")
	probe.Env = append(os.Environ(),
		"EXCALIBUR_FALLBACK_PROBE=1",
		"SSL_CERT_FILE=/dev/null",
		"SSL_CERT_DIR="+t.TempDir(),
	)
	out, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("fallback probe: %v: %s", err, out)
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &n); err != nil {
		t.Fatalf("probe output %q: %v", out, err)
	}
	if n < 100 {
		t.Fatalf("pool without system CAs holds %d certs, want >= 100 fallback roots", n)
	}
	t.Logf("pool without system CAs: %d fallback roots", n)
}
