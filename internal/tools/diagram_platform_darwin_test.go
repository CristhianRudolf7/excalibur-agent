//go:build darwin

package tools

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDarwinDiagramChromiumPATHFirst(t *testing.T) {
	names := []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	for index, installed := range names {
		var calls []string
		got, err := darwinDiagramChromium("/Users/test", func(name string) (string, error) {
			calls = append(calls, name)
			if name == installed {
				return "/custom/" + name, nil
			}
			return "", exec.ErrNotFound
		}, func(string) (os.FileInfo, error) {
			t.Fatal("app bundles searched before PATH")
			return nil, os.ErrNotExist
		})
		if err != nil || got != "/custom/"+installed || !reflect.DeepEqual(calls, names[:index+1]) {
			t.Fatalf("%s: got %q, %v, lookups %v", installed, got, err, calls)
		}
	}
}

func TestDarwinDiagramChromiumAppBundles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "browser")
	if err := os.WriteFile(file, nil, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	home := "/Users/diagram tester"
	candidates := []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		home + "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		home + "/Applications/Chromium.app/Contents/MacOS/Chromium",
	}
	for index, installed := range candidates {
		var paths []string
		got, err := darwinDiagramChromium(home, func(name string) (string, error) {
			if filepath.IsAbs(name) {
				return name, nil
			}
			return "", exec.ErrNotFound
		}, func(path string) (os.FileInfo, error) {
			paths = append(paths, path)
			if path == installed {
				return info, nil
			}
			return nil, os.ErrNotExist
		})
		if err != nil || got != installed || !reflect.DeepEqual(paths, candidates[:index+1]) {
			t.Fatalf("%s: got %q, %v, paths %v", installed, got, err, paths)
		}
	}
}

func TestDarwinDiagramChromiumRejectsInvalidBundles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-executable")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		_, err := darwinDiagramChromium("", func(name string) (string, error) {
			if filepath.IsAbs(name) {
				return name, nil
			}
			return "", exec.ErrNotFound
		}, func(candidate string) (os.FileInfo, error) {
			if !filepath.IsAbs(candidate) {
				t.Fatalf("relative bundle path without HOME: %q", candidate)
			}
			return os.Stat(path)
		})
		if !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("invalid bundle accepted: %v", err)
		}
	}
}

func TestDarwinDiagramChromiumHonorsInjectedLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	browser := filepath.Join(home, "Applications", "Chromium.app", "Contents", "MacOS", "Chromium")
	if err := os.MkdirAll(filepath.Dir(browser), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(browser, nil, 0700); err != nil {
		t.Fatal(err)
	}
	// An installed bundle must not escape the injected executable lookup.
	if got, err := diagramChromium(diagramLookup(nil)); got != "" || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("host browser escaped lookup: %q, %v", got, err)
	}
	if got, err := diagramChromium(diagramLookup(map[string]string{browser: browser})); err != nil || got != browser {
		t.Fatalf("allowed bundle not discovered: %q, %v", got, err)
	}
	redirect := filepath.Join(t.TempDir(), "fake-chromium")
	if err := os.WriteFile(redirect, nil, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := diagramChromium(diagramLookup(map[string]string{browser: redirect})); err != nil || got != redirect {
		t.Fatalf("redirected bundle not honored: %q, %v", got, err)
	}
}
