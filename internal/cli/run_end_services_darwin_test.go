//go:build darwin

package cli

import "fmt"

func init() {
	// Match the detached terminal fixture: HTTPServer's reverse lookup can
	// stall on macOS before listen. Only this fixture bypasses DNS.
	runEndTestServerCommand = func(port int) string {
		return fmt.Sprintf(`python3 -c 'import runpy, socket; socket.getfqdn = lambda host="": host; runpy.run_module("http.server", run_name="__main__")' %d --bind 127.0.0.1`, port)
	}
}
