package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// BrowserConfig controls the single persistent browser in assistant profiles.
// Empty paths are resolved under the daemon data directory at startup.
type BrowserConfig struct {
	Enabled      bool          `json:"enabled" toml:"enabled"`
	Headless     bool          `json:"headless" toml:"headless"`
	Sandbox      bool          `json:"sandbox" toml:"sandbox"`
	Channel      string        `json:"channel" toml:"channel"`
	IdleTimeout  time.Duration `json:"idle_timeout" toml:"idle_timeout"`
	MaxTabs      int           `json:"max_tabs" toml:"max_tabs"`
	ProfileDir   string        `json:"profile_dir" toml:"profile_dir"`
	DownloadsDir string        `json:"downloads_dir" toml:"downloads_dir"`
	RoutinesDir  string        `json:"routines_dir" toml:"routines_dir"`
}

// SearchConfig holds provider references, never credential values.
type SearchConfig struct {
	Order     []string                        `json:"order" toml:"order"`
	Providers map[string]SearchProviderConfig `json:"providers" toml:"providers"`
	UserAgent string                          `json:"user_agent" toml:"user_agent"`
}
type SearchProviderConfig struct {
	KeyRef string `json:"key_ref" toml:"key_ref"`
	// StorageRights asserts that the operator has a Brave plan permitting storage.
	StorageRights bool `json:"storage_rights" toml:"storage_rights"`
}

func defaultBrowser() BrowserConfig {
	return BrowserConfig{Enabled: true, Headless: true, Sandbox: true, Channel: "chrome", IdleTimeout: 10 * time.Minute, MaxTabs: 8}
}
func defaultSearch() SearchConfig {
	return SearchConfig{Order: []string{"tavily", "brave"}, Providers: map[string]SearchProviderConfig{"tavily": {KeyRef: "secret://tavily_api_key"}, "brave": {KeyRef: "secret://brave_api_key"}}, UserAgent: "Mozilla/5.0 (compatible; Excalibur/1.0; https://github.com/CristhianRudolf7/excalibur-agent)"}
}
func validateBrowserSearch(c Config) error {
	if c.Browser.Channel != "chrome" && c.Browser.Channel != "chromium" {
		return fmt.Errorf("config: invalid browser.channel")
	}
	if c.Browser.IdleTimeout <= 0 || c.Browser.MaxTabs <= 0 {
		return fmt.Errorf("config: browser idle_timeout and max_tabs must be positive")
	}
	if len(c.Search.Order) == 0 {
		return fmt.Errorf("config: search.order must not be empty")
	}
	seen := map[string]bool{}
	for _, name := range c.Search.Order {
		if (name != "tavily" && name != "brave") || seen[name] {
			return fmt.Errorf("config: invalid or duplicate search provider")
		}
		seen[name] = true
		if _, ok := c.Search.Providers[name]; !ok {
			return fmt.Errorf("config: missing search provider")
		}
	}
	for name, p := range c.Search.Providers {
		if name != "tavily" && name != "brave" {
			return fmt.Errorf("config: unknown search provider")
		}
		ref := p.KeyRef
		if strings.HasPrefix(ref, "secret://") && len(ref) > len("secret://") {
			continue
		}
		if strings.HasPrefix(ref, "env:") && len(ref) > 4 && !strings.ContainsAny(ref[4:], "= \t\r\n\x00") {
			continue
		}
		return fmt.Errorf("config: search key_ref must be secret://name or env:NAME")
	}
	if strings.ContainsAny(c.Search.UserAgent, "\r\n\x00") {
		return fmt.Errorf("config: invalid search.user_agent")
	}
	return nil
}
func applyBrowserSearchEnv(m map[string]any, prefix string, env []string, disabled bool) {
	if disabled {
		return
	}
	for _, kv := range env {
		name, val, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch name {
		case prefix + "BROWSER_ENABLED", prefix + "BROWSER_SANDBOX":
			field := "enabled"
			if name == prefix+"BROWSER_SANDBOX" {
				field = "sandbox"
			}
			b, err := strconv.ParseBool(val)
			if err != nil {
				setPath(m, []string{"browser", field}, val)
				continue
			}
			setPath(m, []string{"browser", field}, b)
			if field == "sandbox" && !b {
				slog.Warn("browser: Chromium sandbox disabled by environment")
			}
		case prefix + "SEARCH_BRAVE_STORAGE_RIGHTS":
			var value any = val
			if b, err := strconv.ParseBool(val); err == nil {
				value = b
			}
			setPath(m, []string{"search", "providers", "brave", "storage_rights"}, value)
		case prefix + "SEARCH_ORDER":
			var order []string
			for _, part := range strings.Split(val, ",") {
				order = append(order, strings.TrimSpace(part))
			}
			setPath(m, []string{"search", "order"}, order)
		}
	}
}

// TOML decoding replaces map entries, so merge provider fields explicitly before
// decoding. A storage-rights-only overlay must retain the default key reference.
func mergeSearchProviderDefaults(merged map[string]any) {
	search, _ := merged["search"].(map[string]any)
	providers, _ := search["providers"].(map[string]any)
	for name, p := range defaultSearch().Providers {
		if fields, ok := providers[name].(map[string]any); ok {
			defaults := map[string]any{"key_ref": p.KeyRef, "storage_rights": p.StorageRights}
			deepMerge(defaults, fields)
			providers[name] = defaults
		}
	}
}
