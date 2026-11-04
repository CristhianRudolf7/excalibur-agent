//go:build darwin

package robot

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

func supported() error                { return nil }
func configureProcess(cmd *exec.Cmd)  { platform.DarwinProcessGroup{}.Configure(cmd) }
func stopProcess(cmd *exec.Cmd) error { return (platform.DarwinProcessGroup{}).Kill(cmd.Process.Pid) }

func defaultRobotPython() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/excalibur/robot-venv/bin/python")
}
func defaultRobotScript() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local/share/excalibur/robot-bridge/server.py")
}

// Darwin's resolver can stall Python HTTPServer between bind and listen
// during a reverse lookup of loopback. The bridge only needs a numeric
// loopback server name. Keep this adjustment local to the owned interpreter.
const robotDarwinBootstrap = `
import http.server, os, runpy, socketserver, sys
def bind_numeric(self):
    socketserver.TCPServer.server_bind(self)
    self.server_name, self.server_port = self.server_address[:2]
http.server.HTTPServer.server_bind = bind_numeric
script = sys.argv[1]
sys.argv = sys.argv[1:]
sys.path[0] = os.path.dirname(os.path.abspath(script))
runpy.run_path(script, run_name="__main__")
`

func startRobotProcess(cmd *exec.Cmd) error {
	cmd.Args = append([]string{cmd.Args[0], "-c", robotDarwinBootstrap}, cmd.Args[1:]...)
	return cmd.Start()
}
func cleanupRobotProcess(*exec.Cmd) {}
