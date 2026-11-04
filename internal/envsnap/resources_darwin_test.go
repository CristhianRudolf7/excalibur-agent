//go:build darwin

package envsnap

import (
	"strings"
	"testing"
)

func TestDarwinAvailableMemory(t *testing.T) {
	const sample = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               100.
Pages active:                             999.
Pages inactive:                           200.
Pages speculative:                         30.
Pages wired down:                         400.
Pages purgeable:                           50.
Pages occupied by compressor:             900.
`
	for _, tc := range []struct {
		name, output string
		fallback     int64
		want         int64
	}{
		{"header overrides sysctl", sample, 4096, 330 * 16384},
		{"sysctl fallback", strings.SplitN(sample, "\n", 2)[1], 4096, 330 * 4096},
		{"no page size", strings.SplitN(sample, "\n", 2)[1], 0, 0},
		{"empty", "", 4096, 0},
		{"missing queue", "Pages free: 1.\nPages inactive: 2.", 4096, 0},
		{"invalid count", strings.Replace(sample, "100.", "bad.", 1), 4096, 0},
		{"negative count", strings.Replace(sample, "100.", "-1.", 1), 4096, 0},
		{"overflow sum", strings.Replace(sample, "100.", "9223372036854775807.", 1), 4096, 0},
		{"overflow bytes", strings.Replace(sample, "100.", "922337203685477.", 1), 4096, 0},
		{"duplicate queue", sample + "Pages free: 1.\n", 4096, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseDarwinAvailableMemory(tc.output, tc.fallback); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDarwinExplicitResourceRoots(t *testing.T) {
	r := resourceFixture(t, nil)
	for _, reader := range []ResourceReader{r, {ProcRoot: r.ProcRoot}, {SysRoot: r.SysRoot}} {
		got := reader.hostResources()
		if got.AvailableMemory != 0 {
			t.Fatal("fixture read live vm_stat")
		}
		if reader.ProcRoot != "" && got.HostMemory != 32<<30 {
			t.Fatalf("fixture memory replaced: %+v", got)
		}
		if reader.SysRoot != "" && got.HostCPUs != 8 {
			t.Fatalf("fixture CPUs replaced: %+v", got)
		}
	}
}

func TestDarwinLiveHostResources(t *testing.T) {
	got := (ResourceReader{}).hostResources()
	if got.HostMemory <= 0 || got.HostCPUs <= 0 || got.AvailableMemory <= 0 || got.AvailableMemory > got.HostMemory {
		t.Fatalf("invalid live Darwin resources: %+v", got)
	}
}
