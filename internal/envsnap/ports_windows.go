package envsnap

import (
	"runtime"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ipHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var extendedTCP = ipHelper.NewProc("GetExtendedTcpTable")
var extendedUDP = ipHelper.NewProc("GetExtendedUdpTable")

func listeningPorts() []string {
	seen := map[string]bool{}
	for _, tcp := range []bool{true, false} {
		for _, ipv6 := range []bool{false, true} {
			for _, port := range parseWindowsPortTable(windowsPortTable(ipv6, tcp), ipv6, tcp) {
				seen[port] = true
			}
		}
	}
	var out []string
	for port := range seen {
		out = append(out, port)
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// Tables can grow between calls. Bound retries and allocation for best-effort probes.
func windowsPortTable(ipv6, tcp bool) []byte {
	proc, class := extendedUDP, uintptr(1) // UDP_TABLE_OWNER_PID
	if tcp {
		proc, class = extendedTCP, 3
	} // TCP_TABLE_OWNER_PID_LISTENER
	if err := proc.Find(); err != nil {
		return nil
	}
	family := uintptr(windows.AF_INET)
	if ipv6 {
		family = windows.AF_INET6
	}
	var size uint32
	var buffer []byte
	for attempt := 0; attempt < 4; attempt++ {
		var ptr uintptr
		if len(buffer) > 0 {
			ptr = uintptr(unsafe.Pointer(&buffer[0]))
		}
		result, _, _ := proc.Call(ptr, uintptr(unsafe.Pointer(&size)), 0, family, class, 0)
		runtime.KeepAlive(buffer)
		if result == 0 {
			if int(size) <= len(buffer) {
				return buffer[:size]
			}
			return nil
		}
		if result != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) || size < 4 || size > 16<<20 {
			return nil
		}
		buffer = make([]byte, size)
	}
	return nil
}
