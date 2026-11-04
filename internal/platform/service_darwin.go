//go:build darwin

package platform

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"golang.org/x/sys/unix"
)

const defaultLaunchAgentLabel = "com.excalibur.daemon"

// DarwinServiceManager manages the user's launchd LaunchAgent (25.2).
// It restarts failures with a ten-second throttle. launchd has no
// sd_notify watchdog; the daemon's notifier is inert without its socket.
type DarwinServiceManager struct {
	// Label overrides com.excalibur.daemon, for separate installations.
	Label string
	// ExecPath overrides the current executable.
	ExecPath string
	// UnitDir overrides ~/Library/LaunchAgents for tests or development.
	UnitDir string
	// Run overrides command execution. Tests inject it to avoid launchctl.
	Run func(name string, args ...string) ([]byte, error)
}

var _ ServiceManager = DarwinServiceManager{}

// LaunchAgentLabel returns the configured label, including the default used
// by Install. Callers can describe autostart without duplicating its name.
func (m DarwinServiceManager) LaunchAgentLabel() string {
	if m.Label != "" {
		return m.Label
	}
	return defaultLaunchAgentLabel
}

func (m DarwinServiceManager) label() (string, error) {
	label := m.LaunchAgentLabel()
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return "", errors.New("platform: invalid LaunchAgent label")
		}
	}
	if label == "." || label == ".." {
		return "", errors.New("platform: invalid LaunchAgent label")
	}
	return label, nil
}

func (m DarwinServiceManager) plistPath() (string, string, error) {
	label, err := m.label()
	if err != nil {
		return "", "", err
	}
	dir := m.UnitDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		dir = filepath.Join(home, "Library", "LaunchAgents")
	}
	return filepath.Join(dir, label+".plist"), label, nil
}

func (m DarwinServiceManager) run(args ...string) ([]byte, error) {
	if m.Run != nil {
		return m.Run("/bin/launchctl", args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
}

func launchAgentDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// Install atomically writes a 0644 plist, unloads a previous instance,
// then bootstraps it in the user's GUI domain. No GUI login means
// bootstrap returns an error; the plist remains for the next login.
func (m DarwinServiceManager) Install() error {
	path, label, err := m.plistPath()
	if err != nil {
		return err
	}
	bin := m.ExecPath
	if bin == "" {
		bin = os.Getenv("EXCALIBUR_EXEC_PATH")
	}
	if bin == "" {
		bin, err = os.Executable()
		if err != nil {
			return err
		}
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return err
	}
	data, err := (DarwinPaths{}).DataDir()
	if err != nil {
		return err
	}
	logs := filepath.Join(data, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		return err
	}
	environment := map[string]string{"TMPDIR": os.TempDir(), "PATH": os.Getenv("PATH")}
	if environment["PATH"] == "" {
		environment["PATH"] = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"} {
		if value := os.Getenv(key); value != "" {
			environment[key] = value
		}
	}
	body, err := renderLaunchAgent(label, bin, logs, environment)
	if err != nil {
		return err
	}
	if err := writeLaunchAgent(path, body); err != nil {
		return err
	}
	if err := m.bootout(label); err != nil {
		return err
	}
	if out, err := m.run("bootstrap", launchAgentDomain(), path); err != nil {
		return fmt.Errorf("platform: launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func writeLaunchAgent(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".excalibur-plist-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	if err := f.Chmod(0o644); err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (m DarwinServiceManager) bootout(label string) error {
	out, err := m.run("bootout", launchAgentDomain()+"/"+label)
	if err != nil && !launchAgentNotLoaded(out, err) {
		return fmt.Errorf("platform: launchctl bootout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func launchAgentNotLoaded(out []byte, err error) bool {
	if errors.Is(err, unix.ESRCH) {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 3 {
		return true
	}
	message := strings.ToLower(string(out))
	return strings.Contains(message, "could not find service") || strings.Contains(message, "no such process") || strings.Contains(message, "service not found")
}

// Uninstall unloads the job and removes its plist. Missing jobs and
// missing files are successful, making repeat uninstall safe.
func (m DarwinServiceManager) Uninstall() error {
	path, label, err := m.plistPath()
	if err != nil {
		return err
	}
	if err := m.bootout(label); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status reports absent, installed (unloaded), inactive, or active with
// its process ID. Unexpected launchctl failures remain visible.
func (m DarwinServiceManager) Status() (string, error) {
	path, label, err := m.plistPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "absent", nil
		}
		return "", err
	}
	out, err := m.run("print", launchAgentDomain()+"/"+label)
	if err != nil {
		if launchAgentNotLoaded(out, err) {
			return "installed", nil
		}
		return "", fmt.Errorf("platform: launchctl print: %w", err)
	}
	state, pid, err := parseLaunchAgentStatus(string(out))
	if err != nil {
		return "", err
	}
	if state == "running" {
		if pid > 0 {
			return fmt.Sprintf("active (pid %d)", pid), nil
		}
		return "active", nil
	}
	return "inactive", nil
}

func parseLaunchAgentStatus(output string) (string, int, error) {
	var state string
	var pid, depth int
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if depth == 1 {
			key, value, found := strings.Cut(line, " = ")
			if found {
				switch key {
				case "state":
					state = strings.TrimSpace(value)
				case "pid":
					n, err := strconv.Atoi(strings.TrimSpace(value))
					if err != nil || n <= 0 {
						return "", 0, errors.New("platform: invalid launchctl pid")
					}
					pid = n
				}
			}
		}
		// launchctl prints dictionaries on separate lines. Braces inside
		// an environment value or executable path are ordinary characters.
		if strings.HasSuffix(line, "= {") {
			depth++
		} else if line == "}" {
			depth--
		}
	}
	if state == "" || depth != 0 {
		return "", 0, errors.New("platform: launchctl state missing")
	}
	return state, pid, nil
}

// renderLaunchAgent is pure: all paths and environment values are
// inputs and XML-escaped, with deterministic environment key order.
func renderLaunchAgent(label, bin, logs string, environment map[string]string) ([]byte, error) {
	escape := func(value string) (string, error) {
		var b bytes.Buffer
		err := xml.EscapeText(&b, []byte(value))
		return b.String(), err
	}
	tmpl, err := template.New("launch-agent").Funcs(template.FuncMap{"xml": escape}).Parse(launchAgentTemplate)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	err = tmpl.Execute(&body, struct {
		Label, Binary, Stdout, Stderr string
		Environment                   map[string]string
	}{
		label, bin, filepath.Join(logs, "daemon.stdout.log"), filepath.Join(logs, "daemon.stderr.log"), environment,
	})
	return body.Bytes(), err
}

const launchAgentTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{xml .Label}}</string>
  <key>ProgramArguments</key>
  <array><string>{{xml .Binary}}</string><string>daemon</string><string>start</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Standard</string>
  <key>StandardOutPath</key><string>{{xml .Stdout}}</string>
  <key>StandardErrorPath</key><string>{{xml .Stderr}}</string>
  <key>EnvironmentVariables</key>
  <dict>{{range $key, $value := .Environment}}
    <key>{{xml $key}}</key><string>{{xml $value}}</string>{{end}}
  </dict>
</dict>
</plist>
`
