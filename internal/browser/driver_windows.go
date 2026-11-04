package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	pw "github.com/mxschmitt/playwright-go"
	"golang.org/x/sys/windows/registry"
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
	runOpts, err := windowsDriverOptions(opts.DriverDirectory)
	if err != nil {
		return err
	}
	err = pw.Install(runOpts)
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
	opts, err := windowsDriverOptions("")
	if err != nil {
		return err
	}
	d, err := pw.NewDriver(opts)
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
	_, err = chromiumPath()
	return err
}

// processStats has no profile-scoped process sampler on Windows yet.
func processStats(string) (int64, []string) { return 0, nil }

func chromiumPath() (string, error) { return ChromiumExecutable(exec.LookPath) }

// ChromiumExecutable locates a system Chromium browser for automation and diagrams.
// The lookup callback preserves callers' injectable PATH lookup behavior.
func ChromiumExecutable(lookup func(string) (string, error)) (string, error) {
	for _, name := range []string{"chrome.exe", "msedge.exe", "chromium.exe"} {
		if p, err := lookup(name); err == nil {
			return p, nil
		}
	}
	for _, product := range []string{"Google/Chrome/Application/chrome.exe", "Microsoft/Edge/Application/msedge.exe", "Chromium/Application/chrome.exe"} {
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root != "" {
				candidate := filepath.Join(root, filepath.FromSlash(product))
				if regularBrowserFile(candidate) {
					return candidate, nil
				}
			}
		}
	}
	for _, name := range []string{"chrome.exe", "msedge.exe"} {
		for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
			for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
				key, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\`+name, registry.QUERY_VALUE|view)
				if err != nil {
					continue
				}
				candidate, typ, err := key.GetStringValue("")
				key.Close()
				if err == nil && typ == registry.EXPAND_SZ {
					candidate, err = registry.ExpandString(candidate)
				}
				if err == nil && regularBrowserFile(candidate) {
					return candidate, nil
				}
			}
		}
	}
	return "", errors.New("browser: system Chrome, Edge or Chromium missing; install a Chromium browser")
}

func regularBrowserFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func platformBrowserLaunch(launch *pw.BrowserTypeLaunchPersistentContextOptions) error {
	p, err := chromiumPath()
	if err != nil {
		return err
	}
	launch.Channel = nil
	launch.ExecutablePath = pw.String(p)
	// Playwright emits --no-sandbox when this is false. Keep Windows sandboxing.
	launch.ChromiumSandbox = pw.Bool(true)
	return nil
}

func windowsDriverOptions(dir string) (*pw.RunOptions, error) {
	if dir == "" {
		dir = os.Getenv("PLAYWRIGHT_DRIVER_PATH")
	}
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, errors.New("browser: driver cache unavailable")
		}
		// Ask the pinned driver for its version without its HOME-based default.
		driver, err := pw.NewDriver(&pw.RunOptions{DriverDirectory: cache})
		if err != nil {
			return nil, errors.New("browser: driver unavailable")
		}
		dir = filepath.Join(cache, "ms-playwright-go", driver.Version)
	}
	return &pw.RunOptions{DriverDirectory: dir, SkipInstallBrowsers: true, Stdout: io.Discard, Stderr: io.Discard}, nil
}

func runBrowserDriver() (*pw.Playwright, error) {
	opts, err := windowsDriverOptions("")
	if err != nil {
		return nil, err
	}
	return pw.Run(opts)
}
