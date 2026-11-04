package envsnap

import (
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"testing"
)

func TestParseWindowsPortTables(t *testing.T) {
	// Native DWORD layouts: TCP4 listeners :80/:443 plus a non-listening row.
	raw, err := hex.DecodeString("03000000" +
		"020000007f0000010050000000000000000000007b000000" +
		"020000000000000001bb000000000000000000007c000000" +
		"050000007f0000011f90000000000000000000007d000000")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := parseWindowsPortTable(raw, false, true), []string{"tcp:80", "tcp:443"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v != %v", got, want)
	}
	for _, tt := range []struct {
		name              string
		ipv6, tcp         bool
		size, port, state int
		want              string
	}{
		{"tcp6", true, true, 56, 20, 48, "tcp:12345"},
		{"udp4", false, false, 12, 4, -1, "udp:12345"},
		{"udp6", true, false, 28, 20, -1, "udp:12345"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			table := make([]byte, 4+tt.size)
			binary.LittleEndian.PutUint32(table, 1)
			binary.BigEndian.PutUint16(table[4+tt.port:], 12345)
			if tt.state >= 0 {
				binary.LittleEndian.PutUint32(table[4+tt.state:], 2)
			}
			if got := parseWindowsPortTable(table, tt.ipv6, tt.tcp); !reflect.DeepEqual(got, []string{tt.want}) {
				t.Fatal(got)
			}
			if got := parseWindowsPortTable(table[:len(table)-1], tt.ipv6, tt.tcp); len(got) != 0 {
				t.Fatal("accepted truncated row")
			}
		})
	}
	for _, raw := range [][]byte{nil, {1}, {255, 255, 255, 255}, {1, 0, 0, 0}} {
		if got := parseWindowsPortTable(raw, false, true); len(got) != 0 {
			t.Fatal(got)
		}
	}
}
