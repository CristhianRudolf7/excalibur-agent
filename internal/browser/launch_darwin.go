package browser

import (
	"os"
	"os/exec"

	pw "github.com/mxschmitt/playwright-go"
)

func platformBrowserLaunch(launch *pw.BrowserTypeLaunchPersistentContextOptions) error {
	home, _ := os.UserHomeDir()
	return darwinBrowserLaunch(launch, home, os.Stat, exec.LookPath)
}

func darwinBrowserLaunch(launch *pw.BrowserTypeLaunchPersistentContextOptions, home string, stat func(string) (os.FileInfo, error), lookup func(string) (string, error)) error {
	if launch.Channel == nil || *launch.Channel != "chrome" || launch.ExecutablePath != nil {
		return nil
	}
	path, err := darwinChannelPath(home, "chrome", stat, lookup)
	if err != nil {
		return err
	}
	// Playwright resolves the standard app bundle through its named channel.
	if path != darwinStandardChromePath {
		launch.Channel = nil
		launch.ExecutablePath = pw.String(path)
	}
	return nil
}

func runBrowserDriver() (*pw.Playwright, error) { return pw.Run() }
