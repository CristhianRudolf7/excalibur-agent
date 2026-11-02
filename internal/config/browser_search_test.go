package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBrowserSearchDefaultsAndLayers(t *testing.T) {
	c := Default()
	if !c.Browser.Enabled || !c.Browser.Headless || !c.Browser.Sandbox || c.Browser.Channel != "chrome" || c.Browser.MaxTabs != 8 || c.Browser.IdleTimeout != 10*time.Minute {
		t.Fatalf("browser defaults: %+v", c.Browser)
	}
	if !reflect.DeepEqual(c.Search.Order, []string{"tavily", "brave"}) || c.Search.Providers["tavily"].KeyRef != "secret://tavily_api_key" || c.Search.Providers["brave"].KeyRef != "secret://brave_api_key" {
		t.Fatal("search defaults")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[browser]\nmax_tabs = 4\nidle_timeout = '3m'\n[search]\norder = ['brave']\n[search.providers.brave]\nkey_ref = 'env:TEST_KEY'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(LoadOptions{SystemPath: path, UserPath: filepath.Join(t.TempDir(), "missing"), StartDir: t.TempDir(), NoEnv: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Browser.MaxTabs != 4 || c.Browser.IdleTimeout != 3*time.Minute || !c.Browser.Sandbox || c.Search.Providers["brave"].KeyRef != "env:TEST_KEY" {
		t.Fatalf("layers: %+v %+v", c.Browser, c.Search)
	}
	if err = WriteFile(path, c); err != nil {
		t.Fatal(err)
	}
	again, err := Load(LoadOptions{SystemPath: path, UserPath: path, NoEnv: true, StartDir: t.TempDir()})
	if err != nil || !reflect.DeepEqual(c, again) {
		t.Fatalf("roundtrip: %v", err)
	}
}
func TestBrowserSearchEnvironmentAndSandboxWarning(t *testing.T) {
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	c, err := Load(LoadOptions{SkipFiles: true, Env: []string{"EXCALIBUR_BROWSER_ENABLED=0", "EXCALIBUR_BROWSER_SANDBOX=0", "EXCALIBUR_SEARCH_ORDER=brave,tavily", "EXCALIBUR_BROWSER__IDLE_TIMEOUT=2m"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Browser.Enabled || c.Browser.Sandbox || c.Browser.IdleTimeout != 2*time.Minute || c.Search.Order[0] != "brave" {
		t.Fatal("environment ignored")
	}
	if !strings.Contains(log.String(), "sandbox disabled") {
		t.Fatal("missing sandbox warning")
	}
	c, err = Load(LoadOptions{SkipFiles: true, NoEnv: true, Env: []string{"EXCALIBUR_BROWSER_ENABLED=0"}})
	if err != nil || !c.Browser.Enabled {
		t.Fatal("NoEnv ignored")
	}
	c, err = Load(LoadOptions{SkipFiles: true, Env: []string{"EXCALIBUR_BROWSER_ENABLED=0"}, Overrides: map[string]any{"browser.enabled": true}})
	if err != nil || !c.Browser.Enabled {
		t.Fatal("flag precedence")
	}
}
func TestBrowserSearchValidation(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"channel": func(c *Config) { c.Browser.Channel = "invalid" }, "tabs": func(c *Config) { c.Browser.MaxTabs = 0 }, "idle": func(c *Config) { c.Browser.IdleTimeout = 0 },
		"empty_order": func(c *Config) { c.Search.Order = nil }, "unknown": func(c *Config) { c.Search.Order = []string{"other"} }, "duplicate": func(c *Config) { c.Search.Order = []string{"tavily", "tavily"} }, "missing": func(c *Config) { delete(c.Search.Providers, "tavily") }, "literal_key": func(c *Config) { c.Search.Providers["tavily"] = SearchProviderConfig{KeyRef: "literal"} }, "empty_env": func(c *Config) { c.Search.Providers["tavily"] = SearchProviderConfig{KeyRef: "env:"} }, "user_agent": func(c *Config) { c.Search.UserAgent = "bad\r\nheader" },
	} {
		t.Run(name, func(t *testing.T) {
			c := Default()
			change(&c)
			if c.Validate() == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}

func TestBraveStorageRightsConfigAndEnvironment(t *testing.T) {
	if Default().Search.Providers["brave"].StorageRights {
		t.Fatal("storage rights must default to false")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[search.providers.brave]\nstorage_rights = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := LoadOptions{SystemPath: path, UserPath: filepath.Join(t.TempDir(), "missing"), StartDir: t.TempDir(), Env: []string{}}
	cfg, err := Load(opts)
	if err != nil || !cfg.Search.Providers["brave"].StorageRights {
		t.Fatalf("TOML opt-in ignored: %v", err)
	}
	for _, value := range []string{"0", "1", "false", "true"} {
		opts.Env = []string{"EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=" + value}
		cfg, err = Load(opts)
		want := value == "1" || value == "true"
		if err != nil || cfg.Search.Providers["brave"].StorageRights != want {
			t.Fatalf("environment %s: %v", value, err)
		}
	}
	opts.Env = []string{"EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=0"}
	opts.NoEnv = true
	cfg, err = Load(opts)
	if err != nil || !cfg.Search.Providers["brave"].StorageRights {
		t.Fatalf("NoEnv ignored: %v", err)
	}
	opts.NoEnv = false
	opts.Env = []string{"EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=invalid"}
	if _, err = Load(opts); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	opts.Env = []string{"EXCALIBUR_SEARCH_BRAVE_STORAGE_RIGHTS=1"}
	opts.Overrides = map[string]any{"search.providers.brave.storage_rights": false}
	cfg, err = Load(opts)
	if err != nil || cfg.Search.Providers["brave"].StorageRights {
		t.Fatalf("flag precedence ignored: %v", err)
	}
}
