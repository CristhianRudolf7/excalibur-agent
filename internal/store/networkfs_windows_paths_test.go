package store

import "testing"

func TestWindowsNetworkVolume(t *testing.T) {
	for _, tt := range []struct {
		volume string
		drive  uint32
		want   bool
	}{
		{`C:`, 3, false}, {`Z:`, 4, true}, {`Z:`, 1, false}, {"", 4, false},
		{`\\server\share`, 0, true}, {`//server/share`, 0, true},
		{`\\?\UNC\server\share`, 0, true},
		{`\\?\C:`, 3, false}, {`\\?\Z:`, 4, true},
		{`\\?\Volume{123}`, 3, false}, {`\\.\C:`, 3, false},
	} {
		t.Run(tt.volume, func(t *testing.T) {
			got := windowsNetworkVolume(tt.volume, func(root string) uint32 {
				if root != tt.volume+`\` {
					t.Fatalf("drive root %q", root)
				}
				return tt.drive
			})
			if got != tt.want {
				t.Fatalf("volume %q: got %v, want %v", tt.volume, got, tt.want)
			}
		})
	}
}
