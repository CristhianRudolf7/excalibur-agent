//go:build darwin

package pdf

import "testing"

func TestDarwinPDFGate(t *testing.T) {
	if !workerSupported() {
		t.Fatal("PDF worker disabled on Darwin")
	}
}
