//go:build darwin

package envsnap

import "testing"

func TestDarwinCgroupsUnsupported(t *testing.T) {
	if cgroupsSupported() {
		t.Fatal("Linux cgroups enabled on Darwin")
	}
	if locations := (ResourceReader{}).locations(); locations != nil {
		t.Fatalf("unexpected cgroups: %v", locations)
	}
}
