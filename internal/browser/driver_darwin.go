//go:build darwin

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	home, _ := os.UserHomeDir()
	_, err = darwinChannelPath(home, channel, os.Stat, exec.LookPath)
	return err
}

const darwinStandardChromePath = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

func darwinChannelPath(home, channel string, stat func(string) (os.FileInfo, error), lookup func(string) (string, error)) (string, error) {
	if channel == "chromium" {
		return findDarwinBrowser(home, false, stat, lookup)
	}
	if channel != "chrome" {
		return "", errors.New("browser: unsupported channel")
	}
	path, err := findDarwinBrowser(home, true, stat, lookup)
	if err != nil {
		return "", errors.New("browser: system Google Chrome missing; install Google Chrome or explicitly configure chromium")
	}
	return path, nil
}

// chromiumPath locates an installed browser without downloading one. The
// explicit chromium channel can use either Chromium or Google Chrome.
func chromiumPath() (string, error) {
	home, _ := os.UserHomeDir()
	return findDarwinBrowser(home, false, os.Stat, exec.LookPath)
}

// findDarwinBrowser takes the filesystem and PATH lookup explicitly so discovery
// can be tested without requiring an installed browser or changing the host.
func findDarwinBrowser(home string, chromeOnly bool, stat func(string) (os.FileInfo, error), lookup func(string) (string, error)) (string, error) {
	bundles := []string{"Google Chrome.app/Contents/MacOS/Google Chrome"}
	if !chromeOnly {
		bundles = append(bundles, "Chromium.app/Contents/MacOS/Chromium")
	}
	roots := []string{"/Applications"}
	if home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	for _, root := range roots {
		for _, bundle := range bundles {
			path := filepath.Join(root, bundle)
			if info, err := stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
				return path, nil
			}
		}
	}
	names := []string{"google-chrome"}
	if !chromeOnly {
		names = []string{"chromium", "google-chrome"}
	}
	for _, name := range names {
		if path, err := lookup(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("browser: system Chrome or Chromium missing; install Google Chrome or Chromium")
}

// processStats samples only the selected profile's browser process tree.
func processStats(profile string) (int64, []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "/bin/ps", "-ww", "-axo", "pid=,ppid=,rss=,command=").Output()
	if err != nil {
		return 0, nil
	}
	return parseDarwinProcessStats(string(raw), profile)
}

// parseDarwinProcessStats parses BSD ps output (RSS is in KiB), then includes
// descendants and processes reparented after launch that still name the profile.
func parseDarwinProcessStats(output, profile string) (int64, []string) {
	if profile == "" {
		return 0, nil
	}
	parents := map[int]int{}
	commands := map[int]string{}
	rss := map[int]int64{}
	roots := map[int]bool{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		kb, e3 := strconv.ParseInt(fields[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || pid <= 0 || parent < 0 || kb < 0 || kb > (1<<63-1)/1024 {
			continue
		}
		command := strings.TrimSpace(line)
		for range 3 {
			end := strings.IndexFunc(command, unicode.IsSpace)
			command = strings.TrimSpace(command[end:])
		}
		parents[pid], commands[pid], rss[pid] = parent, command, kb*1024
		for _, arg := range []string{"--user-data-dir=" + profile, "--user-data-dir=\"" + profile + "\"", "--user-data-dir='" + profile + "'"} {
			if strings.Contains(" "+command+" ", " "+arg+" ") {
				roots[pid] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for pid, parent := range parents {
			if roots[parent] && !roots[pid] {
				roots[pid], changed = true, true
			}
		}
	}
	pids := make([]int, 0, len(roots))
	for pid := range roots {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	var total int64
	var cmds []string
	for _, pid := range pids {
		total += rss[pid]
		cmds = append(cmds, commands[pid])
	}
	return total, cmds
}
