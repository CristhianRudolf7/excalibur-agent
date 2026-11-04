//go:build darwin

package browser

import (
	"os"
	"path/filepath"
	"testing"

	pw "github.com/mxschmitt/playwright-go"
)

func TestDarwinChromeLaunchDiscovery(t *testing.T) {
	file := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(file, nil, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	home := "/Users/browser tester"
	for _, installed := range []string{
		darwinStandardChromePath,
		home + "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/opt/homebrew/bin/google-chrome",
		"",
	} {
		t.Run(installed, func(t *testing.T) {
			stat := func(path string) (os.FileInfo, error) {
				if path == installed {
					return info, nil
				}
				return nil, os.ErrNotExist
			}
			lookup := func(name string) (string, error) {
				if name != "google-chrome" {
					t.Fatalf("Chrome channel searched for %q", name)
				}
				if installed == "/opt/homebrew/bin/google-chrome" {
					return installed, nil
				}
				return "", os.ErrNotExist
			}
			launch := pw.BrowserTypeLaunchPersistentContextOptions{Channel: pw.String("chrome"), ChromiumSandbox: pw.Bool(true)}
			err := darwinBrowserLaunch(&launch, home, stat, lookup)
			if installed == "" {
				if err == nil {
					t.Fatal("missing Chrome accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			available, err := darwinChannelPath(home, "chrome", stat, lookup)
			if err != nil || available != installed {
				t.Fatalf("availability path: %q, %v", available, err)
			}
			if installed == darwinStandardChromePath {
				if launch.Channel == nil || *launch.Channel != "chrome" || launch.ExecutablePath != nil {
					t.Fatalf("standard Chrome must use Playwright's channel: %+v", launch)
				}
			} else if launch.Channel != nil || launch.ExecutablePath == nil || *launch.ExecutablePath != installed {
				t.Fatalf("discovered executable not selected: %+v", launch)
			}
			if !*launch.ChromiumSandbox {
				t.Fatal("sandbox disabled")
			}
		})
	}
}

func TestDarwinLaunchPreservesExplicitExecutable(t *testing.T) {
	for _, launch := range []pw.BrowserTypeLaunchPersistentContextOptions{
		{ExecutablePath: pw.String("/custom/chromium")},
		{Channel: pw.String("chrome"), ExecutablePath: pw.String("/custom/chrome")},
		{Channel: pw.String("chromium")},
	} {
		before := launch
		err := darwinBrowserLaunch(&launch, "", func(string) (os.FileInfo, error) {
			t.Fatal("unexpected filesystem lookup")
			return nil, os.ErrNotExist
		}, func(string) (string, error) {
			t.Fatal("unexpected PATH lookup")
			return "", os.ErrNotExist
		})
		if err != nil || launch.Channel != before.Channel || launch.ExecutablePath != before.ExecutablePath {
			t.Fatalf("explicit launch changed: %+v, %v", launch, err)
		}
	}
}
