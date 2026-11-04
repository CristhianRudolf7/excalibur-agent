//go:build darwin

package tools

import "fmt"

func init() {
	// Python's HTTPServer resolves numeric loopback before listen, which can
	// stall for 35s on macOS. Exercise the real detached launch without DNS.
	detachedTestServerCommand = func(port int) string {
		return fmt.Sprintf(`python3 -c 'import runpy, socket; socket.getfqdn = lambda host="": host; runpy.run_module("http.server", run_name="__main__")' %d --bind 127.0.0.1`, port)
	}
}
