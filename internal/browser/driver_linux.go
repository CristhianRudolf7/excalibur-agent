//go:build linux

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	pw "github.com/mxschmitt/playwright-go"
)

func supported() error { return nil }

// InstallOptions selects the driver cache directory. Browsers are never downloaded.
type InstallOptions struct{ DriverDirectory string }

// Install installs only Playwright's driver. The upstream download is synchronous;
// cancellation is checked before and after it and never starts a browser.
func Install(ctx context.Context, opts InstallOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := pw.Install(&pw.RunOptions{DriverDirectory: opts.DriverDirectory, SkipInstallBrowsers: true, Stdout: io.Discard, Stderr: io.Discard})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return errors.New("browser: driver installation failed")
	}
	return nil
}

// Available checks the driver and the system Chrome without launching a browser.
func Available() error { return AvailableForChannel("chrome") }

// AvailableForChannel checks the selected system browser without launching it.
func AvailableForChannel(channel string) error {
	d, err := pw.NewDriver()
	if err != nil {
		return errors.New("browser: driver unavailable; run browser.Install first")
	}
	command := d.Command("--version")
	// Inspect the installed artifacts without starting Node: a loaded host can
	// take tens of seconds merely to start its CLI, which is not a missing driver.
	if _, err := exec.LookPath(command.Path); err != nil {
		return errors.New("browser: driver missing; run browser.Install first")
	}
	if len(command.Args) < 2 {
		return errors.New("browser: driver missing; run browser.Install first")
	}
	if info, err := os.Stat(command.Args[1]); err != nil || !info.Mode().IsRegular() {
		return errors.New("browser: driver missing; run browser.Install first")
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(command.Args[1]), "package.json"))
	var metadata struct {
		Version string `json:"version"`
	}
	if err != nil || json.Unmarshal(data, &metadata) != nil || metadata.Version != d.Version {
		return errors.New("browser: driver missing or incompatible; run browser.Install first")
	}
	if channel == "chromium" {
		_, err := chromiumPath()
		return err
	}
	if channel != "chrome" {
		return errors.New("browser: unsupported channel")
	}
	if _, err := exec.LookPath("google-chrome"); err != nil {
		return errors.New("browser: system Google Chrome missing; install google-chrome or explicitly configure chromium")
	}
	return nil
}

// processStats finds only this profile's Chrome tree, including reparented children.
func processStats(profile string) (int64, []string) {
	entries, _ := os.ReadDir("/proc")
	parents := map[int]int{}
	commands := map[int]string{}
	rss := map[int]int64{}
	roots := map[int]bool{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join("/proc", entry.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		cmd := strings.ReplaceAll(string(raw), "\x00", " ")
		commands[pid] = cmd
		if strings.Contains(cmd, "--user-data-dir="+profile+" ") {
			roots[pid] = true
		}
		stat, _ := os.ReadFile(filepath.Join(dir, "status"))
		for _, line := range strings.Split(string(stat), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "PPid:":
				parents[pid], _ = strconv.Atoi(fields[1])
			case "VmRSS:":
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				rss[pid] = kb * 1024
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for pid, parent := range parents {
			if roots[parent] && !roots[pid] {
				roots[pid] = true
				changed = true
			}
		}
	}
	var total int64
	var cmds []string
	for pid := range roots {
		total += rss[pid]
		cmds = append(cmds, commands[pid])
	}
	return total, cmds
}

func chromiumPath() (string, error) {
	for _, name := range []string{"chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("browser: system Chromium missing; install Chromium")
}
