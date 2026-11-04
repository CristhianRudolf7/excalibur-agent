package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	pw "github.com/mxschmitt/playwright-go"
)

func TestWindowsBrowserRoots(t *testing.T) {
	for _, product := range []string{"Google/Chrome/Application/chrome.exe", "Microsoft/Edge/Application/msedge.exe", "Chromium/Application/chrome.exe"} {
		for _, variable := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			t.Run(product+variable, func(t *testing.T) {
				for _, key := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
					t.Setenv(key, t.TempDir())
				}
				want := filepath.Join(os.Getenv(variable), filepath.FromSlash(product))
				if err := os.MkdirAll(filepath.Dir(want), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(want, nil, 0600); err != nil {
					t.Fatal(err)
				}
				got, err := ChromiumExecutable(func(string) (string, error) { return "", os.ErrNotExist })
				if err != nil || got != want {
					t.Fatalf("got %q, %v; want %q", got, err, want)
				}
			})
		}
	}
}

func TestWindowsDriverArtifactsAndLaunch(t *testing.T) {
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", "")
	t.Setenv("PLAYWRIGHT_NODEJS_PATH", "")
	t.Setenv("PLAYWRIGHT_CLI_PATH", "")
	opts, err := windowsDriverOptions("")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := pw.NewDriver(opts)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(os.Getenv("LOCALAPPDATA"), "ms-playwright-go", driver.Version)
	if opts.DriverDirectory != want {
		t.Fatalf("cache %q, want %q", opts.DriverDirectory, want)
	}
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", want)
	cmd := driver.Command("--version")
	if filepath.Base(cmd.Path) != "node.exe" {
		t.Fatal(cmd.Path)
	}
	if err := os.MkdirAll(filepath.Dir(cmd.Args[1]), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{cmd.Path: "", cmd.Args[1]: "", filepath.Join(filepath.Dir(cmd.Args[1]), "package.json"): `{"version":"` + driver.Version + `"}`} {
		if err := os.WriteFile(name, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	t.Setenv("ProgramFiles", root)
	chrome := filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe")
	if err := os.MkdirAll(filepath.Dir(chrome), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Available(); err != nil {
		t.Fatal(err)
	}
	launch := pw.BrowserTypeLaunchPersistentContextOptions{ChromiumSandbox: pw.Bool(false)}
	if err := platformBrowserLaunch(&launch); err != nil {
		t.Fatal(err)
	}
	if launch.Channel != nil || launch.ExecutablePath == nil || !*launch.ChromiumSandbox {
		t.Fatalf("launch: %+v", launch)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(cmd.Args[1]), "package.json"), []byte(`{"version":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Available(); err == nil {
		t.Fatal("accepted incompatible driver")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Install(ctx, InstallOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
