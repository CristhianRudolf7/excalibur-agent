//go:build darwin

package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pw "github.com/mxschmitt/playwright-go"
)

func TestDarwinBrowserDiscovery(t *testing.T) {
	home := "/Users/browser tester"
	for _, path := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		home + "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		home + "/Applications/Chromium.app/Contents/MacOS/Chromium",
	} {
		t.Run(path, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "browser")
			if err := os.WriteFile(executable, nil, 0700); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(executable)
			if err != nil {
				t.Fatal(err)
			}
			stat := func(candidate string) (os.FileInfo, error) {
				if candidate == path {
					return info, nil
				}
				return nil, os.ErrNotExist
			}
			lookup := func(string) (string, error) { t.Fatal("PATH searched before app bundles"); return "", os.ErrNotExist }
			got, err := findDarwinBrowser(home, false, stat, lookup)
			if err != nil || got != path {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestDarwinBrowserPATHAndMissing(t *testing.T) {
	stat := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	for _, name := range []string{"chromium", "google-chrome"} {
		got, err := findDarwinBrowser("", false, stat, func(candidate string) (string, error) {
			if candidate == name {
				return "/opt/homebrew/bin/" + name, nil
			}
			return "", os.ErrNotExist
		})
		if err != nil || got != "/opt/homebrew/bin/"+name {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	_, err := findDarwinBrowser("", true, stat, func(candidate string) (string, error) {
		if candidate == "chromium" {
			t.Fatal("Chrome channel must not silently use Chromium")
		}
		return "", os.ErrNotExist
	})
	if err == nil {
		t.Fatal("missing browser accepted")
	}
}

func TestDarwinBrowserRejectsNonExecutable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, t.TempDir()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = findDarwinBrowser("", false, func(string) (os.FileInfo, error) { return info, nil }, func(string) (string, error) { return "", os.ErrNotExist })
		if err == nil {
			t.Fatal("non-executable browser accepted")
		}
	}
}

func TestDarwinChromiumPathUsesHOME(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	browser := filepath.Join(home, "Applications/Chromium.app/Contents/MacOS/Chromium")
	if err := os.MkdirAll(filepath.Dir(browser), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(browser, nil, 0700); err != nil {
		t.Fatal(err)
	}
	// Use injected discovery to keep this test independent of system installations.
	got, err := findDarwinBrowser(os.Getenv("HOME"), false, func(path string) (os.FileInfo, error) {
		if !strings.HasPrefix(path, home+"/") {
			return nil, os.ErrNotExist
		}
		return os.Stat(path)
	}, func(string) (string, error) { return "", os.ErrNotExist })
	if err != nil || got != browser {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDarwinPSProfileTree(t *testing.T) {
	output := `  PID PPID RSS COMMAND
  13 12 3 /Applications/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper --type=renderer
  12 11 2 helper --type=gpu-process
  11 1 10 /Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir=/Users/test/My  Profile
  14 1 4 helper --user-data-dir=/Users/test/My  Profile --type=utility
  15 1 900 other --user-data-dir=/Users/test/My  Profile-other
  16 1 800 other --user-data-dir=/unrelated
  bad line
  17 1 -1 broken --user-data-dir=/Users/test/My  Profile
  18 1 9223372036854775807 broken --user-data-dir=/Users/test/My  Profile
`
	rss, commands := parseDarwinProcessStats(output, "/Users/test/My  Profile")
	if rss != 19*1024 || len(commands) != 4 {
		t.Fatalf("got %d, %v", rss, commands)
	}
	if !strings.Contains(commands[0], "My  Profile") {
		t.Fatal("profile spaces lost")
	}
	_, again := parseDarwinProcessStats(output, "/Users/test/My  Profile")
	if !reflect.DeepEqual(commands, again) {
		t.Fatal("unstable process order")
	}
	for _, profile := range []string{"", "/missing"} {
		rss, commands = parseDarwinProcessStats(output, profile)
		if rss != 0 || len(commands) != 0 {
			t.Fatalf("unrelated processes: %d %v", rss, commands)
		}
	}
}

func TestDarwinPSQuotedProfiles(t *testing.T) {
	for _, value := range []string{`"/tmp/My Profile"`, `'/tmp/My Profile'`} {
		rss, commands := parseDarwinProcessStats("1 0 7 chrome --user-data-dir="+value, "/tmp/My Profile")
		if rss != 7*1024 || len(commands) != 1 {
			t.Fatalf("got %d %v", rss, commands)
		}
	}
}

func TestDarwinDriverCancelledInstall(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Install(ctx, InstallOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := supported(); err != nil {
		t.Fatal(err)
	}
	if !*(Config{}).defaults().Sandbox {
		t.Fatal("sandbox must default to enabled")
	}
}

func TestDarwinDriverAvailabilityChecksArtifactsWithoutLaunching(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "cli.js")
	t.Setenv("PLAYWRIGHT_CLI_PATH", cli)
	// Any attempt to execute the driver would fail. Availability only reads files.
	t.Setenv("PLAYWRIGHT_NODEJS_PATH", "/usr/bin/false")
	t.Setenv("PATH", dir)
	browser := filepath.Join(dir, "chromium")
	if err := os.WriteFile(browser, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if err := AvailableForChannel("chromium"); err == nil {
		t.Fatal("missing CLI accepted")
	}
	if err := os.WriteFile(cli, nil, 0600); err != nil {
		t.Fatal(err)
	}
	driver, err := pw.NewDriver()
	if err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(dir, "package.json")
	if err := os.WriteFile(metadata, []byte(`{"version":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AvailableForChannel("chromium"); err == nil {
		t.Fatal("incompatible driver accepted")
	}
	if err := os.WriteFile(metadata, []byte(`{"version":"`+driver.Version+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AvailableForChannel("chromium"); err != nil {
		t.Fatal(err)
	}
	if err := AvailableForChannel("unknown"); err == nil {
		t.Fatal("unknown channel accepted")
	}
}

func TestDarwinAvailabilityMatchesChannelLaunchPath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "browser")
	if err := os.WriteFile(file, nil, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	home := "/Users/test"
	for _, test := range []struct {
		channel, installed string
		available          bool
	}{
		{"chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", true},
		{"chrome", home + "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", true},
		{"chromium", home + "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", true},
		{"unknown", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", false},
	} {
		path, err := darwinChannelPath(home, test.channel, func(candidate string) (os.FileInfo, error) {
			if candidate == test.installed {
				return info, nil
			}
			return nil, os.ErrNotExist
		}, func(string) (string, error) { return "", os.ErrNotExist })
		if (err == nil) != test.available {
			t.Errorf("%s at %s: %q, %v", test.channel, test.installed, path, err)
		}
		if test.available && path != test.installed {
			t.Errorf("unexpected launch path: %q", path)
		}
	}
}
