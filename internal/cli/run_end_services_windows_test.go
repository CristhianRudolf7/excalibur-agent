package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func init() {
	runEndTestServerCommand = func(port int) string {
		executable, err := os.Executable()
		if err != nil {
			panic(err)
		}
		// The Windows shell discovery prefers Git Bash on the test machine.
		return fmt.Sprintf("%q -test.run=^TestWindowsRunEndHTTPServer$ -- %d", filepath.ToSlash(executable), port)
	}
	cleanupRunEndServer = func(pid int) { _ = (platform.WindowsProcessGroup{}).Kill(pid) }
}

// The test executable supplies the native server so survival coverage requires
// neither a Python installation nor a Windows Store execution alias.
func TestWindowsRunEndHTTPServer(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	port, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		t.Fatal(err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprintln(w, "Directory listing") })
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), handler); err != nil {
		t.Fatal(err)
	}
}
