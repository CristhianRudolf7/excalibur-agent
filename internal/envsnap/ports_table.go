package envsnap

import (
	"encoding/binary"
	"strconv"
)

// parseWindowsPortTable decodes the DWORD-aligned IP Helper OWNER_PID tables.
// Keeping the wire parser portable lets Linux tests cover both address families.
func parseWindowsPortTable(raw []byte, ipv6, tcp bool) []string {
	size, port, state := 12, 4, -1
	protocol := "udp:"
	if ipv6 {
		size, port = 28, 20
	}
	if tcp {
		protocol = "tcp:"
		size, port, state = 24, 8, 0
		if ipv6 {
			size, port, state = 56, 20, 48
		}
	}
	if len(raw) < 4 {
		return nil
	}
	count := uint64(binary.LittleEndian.Uint32(raw[:4]))
	if count > uint64((len(raw)-4)/size) {
		return nil
	}
	var ports []string
	for i := 0; i < int(count); i++ {
		row := raw[4+i*size : 4+(i+1)*size]
		if state >= 0 && binary.LittleEndian.Uint32(row[state:state+4]) != 2 {
			continue
		}
		// The port is a network-order WORD stored in the low half of a DWORD.
		value := binary.BigEndian.Uint16(row[port : port+2])
		if value != 0 {
			ports = append(ports, protocol+strconv.Itoa(int(value)))
		}
	}
	return ports
}
