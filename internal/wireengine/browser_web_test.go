package wireengine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
	"github.com/CristhianRudolf7/excalibur-agent/internal/websearch"
)

func TestBuildEmbeddedOptionalWeb(t *testing.T) {
	ctx := context.Background()
	memory, err := BuildEmbedded(EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := memory.Exec.Execute(ctx, "web_search", json.RawMessage(`{"query":"SQLite"}`), tools.ExecuteOptions{})
	if err != nil || !strings.Contains(out.Output, "SQLite guide") {
		t.Fatalf("nil must keep memory: %+v %v", out, err)
	}
	cfg := config.Default()
	cfg.Browser.Enabled = false
	cfg.Search.Providers["tavily"] = config.SearchProviderConfig{KeyRef: "env:BR3_ABSENT_KEY"}
	cfg.Search.Providers["brave"] = config.SearchProviderConfig{KeyRef: "env:BR3_ABSENT_KEY"}
	t.Setenv("BR3_ABSENT_KEY", "")
	real, err := NewBrowserWeb(BrowserWebOptions{Config: cfg, DataDir: t.TempDir(), Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer real.Close()
	if _, ok := real.Web.Backend.(*websearch.Backend); !ok {
		t.Fatal("not real backend")
	}
	emb, err := BuildEmbedded(EmbeddedOptions{Web: real.Web})
	if err != nil {
		t.Fatal(err)
	}
	out, err = emb.Exec.Execute(ctx, "web_search", json.RawMessage(`{"query":"SQLite"}`), tools.ExecuteOptions{})
	if err != nil || out.Status != tools.StatusToolError || strings.Contains(out.Output, "SQLite guide") {
		t.Fatalf("real backend not invoked: %+v %v", out, err)
	}
	if memory.Prefix != emb.Prefix {
		t.Fatal("prefix changed")
	}
}
func TestBrowserWebAvailabilityAndPrivatePaths(t *testing.T) {
	cfg := config.Default()
	dir := t.TempDir()
	var warnings []string
	opts := BrowserWebOptions{Config: cfg, DataDir: dir, Warn: func(s string) { warnings = append(warnings, s) }, Available: func() error { return errors.New("missing Chrome") }}
	r, err := NewBrowserWeb(opts)
	if err != nil {
		t.Fatal(err)
	}
	if r.Browser != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "browser disabled:") {
		t.Fatal("unavailable browser")
	}
	opts.Available = func() error { return nil }
	r, err = NewBrowserWeb(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Browser == nil {
		t.Fatal("available browser not built")
	}
	for _, part := range []string{"profile", "downloads", "routines"} {
		st, err := os.Stat(dir + "/browser/" + part)
		if err != nil || st.Mode().Perm() != 0700 {
			t.Fatalf("private %s: %v", part, err)
		}
	}
	cfg.Browser.Enabled = false
	opts.Config = cfg
	opts.Available = func() error { t.Fatal("disabled browser probed"); return nil }
	r, err = NewBrowserWeb(opts)
	if err != nil || r.Browser != nil {
		t.Fatal("disabled browser built")
	}
}
func TestSearchKeysResolveOnlyAtExecution(t *testing.T) {
	t.Setenv("BR3_TEST_KEY", "first")
	resolve := searchKey(BrowserWebOptions{}, "env:BR3_TEST_KEY")
	t.Setenv("BR3_TEST_KEY", "second")
	val, err := resolve(context.Background())
	if err != nil || val != "second" {
		t.Fatal("key not resolved at boundary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = resolve(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
	if _, err = searchKey(BrowserWebOptions{}, "secret://key")(context.Background()); err == nil {
		t.Fatal("nil vault accepted")
	}
}

func TestSearchAndBrowserVaultPolicies(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	entries := []struct {
		name, tool      string
		domains, scopes []string
	}{
		{name: "search-key", tool: "web_search", scopes: []string{"project-a"}},
		{name: "form-key", tool: "browser_act", domains: []string{"allowed.example"}, scopes: []string{"project-a"}},
		{name: "wrong-tool", tool: "exec", domains: []string{"allowed.example"}},
	}
	for _, entry := range entries {
		if _, err := v.Set(ctx, vault.SetParams{Name: entry.name, Type: vault.TypeAPIKey, Value: []byte("test-private-value"), AllowedTools: []string{entry.tool}, Domains: entry.domains, Scopes: entry.scopes}); err != nil {
			t.Fatal(err)
		}
	}
	opts := BrowserWebOptions{Vault: v, RunID: "browser-test", Project: "project-a", Delegated: true}
	if key, err := searchKey(opts, "secret://search-key")(ctx); err != nil || key != "test-private-value" {
		t.Fatal("allowed search key unavailable")
	}
	if _, err := searchKey(opts, "secret://wrong-tool")(ctx); err == nil {
		t.Fatal("search bypassed tool policy")
	}
	secrets := browserSecrets{opts}
	if key, err := secrets.Resolve(ctx, "secret://form-key", "allowed.example"); err != nil || key != "test-private-value" {
		t.Fatal("allowed form key unavailable")
	}
	if _, err := secrets.Resolve(ctx, "secret://form-key", "other.example"); err == nil {
		t.Fatal("form bypassed domain policy")
	}
	opts.Project = "project-b"
	if _, err := searchKey(opts, "secret://search-key")(ctx); err == nil {
		t.Fatal("search bypassed delegate project policy")
	}
	if _, err := (browserSecrets{opts}).Resolve(ctx, "secret://form-key", "allowed.example"); err == nil {
		t.Fatal("form bypassed delegate project policy")
	}
}

func TestBraveStorageRightsStartupLogAndWiring(t *testing.T) {
	const message = "Brave enabled for durable use: storage_rights=true (requires a Brave plan with storage rights)"
	for _, enabled := range []bool{false, true} {
		cfg := config.Default()
		cfg.Browser.Enabled = false
		cfg.Search.Order = []string{"brave"}
		cfg.Search.Providers["brave"] = config.SearchProviderConfig{KeyRef: "env:BR3_STORAGE_TEST_MISSING", StorageRights: enabled}
		t.Setenv("BR3_STORAGE_TEST_MISSING", "")
		var messages []string
		r, err := NewBrowserWeb(BrowserWebOptions{Config: cfg, DataDir: t.TempDir(), Warn: func(s string) { messages = append(messages, s) }})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			_, err = r.Web.Backend.Search(context.Background(), "query", tools.SearchOptions{})
			if err == nil || strings.Contains(err.Error(), "transient use only") == enabled {
				t.Fatalf("storage rights not wired (%t): %v", enabled, err)
			}
		}
		count := 0
		for _, s := range messages {
			if s == message {
				count++
			}
		}
		want := 0
		if enabled {
			want = 1
		}
		if count != want {
			t.Fatalf("startup log count = %d, want %d", count, want)
		}
		if err = r.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrowserCredentialFieldReferences(t *testing.T) {
	ctx := context.Background()
	v := openTestVault(t)
	for _, field := range []string{"username", "email", "password", "otp", "api_key"} {
		value := "private-" + field + "-sentinel"
		if _, err := v.Create(ctx, vault.CreateParams{Name: "credentials-fixture-" + field, Type: vault.TypeWebsitePassword, Value: []byte(value), AllowedTools: []string{"browser_act"}, Domains: []string{"example.com"}}); err != nil {
			t.Fatal(err)
		}
		resolver := browserSecrets{BrowserWebOptions{Vault: v}}
		for _, prefix := range []string{"", "secret://"} {
			got, err := resolver.Resolve(ctx, prefix+"credentials-fixture."+field, "example.com")
			if err != nil || got != value {
				t.Fatalf("field %s unavailable: %v", field, err)
			}
		}
		for _, host := range []string{"evil.test", "sub.example.com", "example.com.evil.test"} {
			got, err := resolver.Resolve(ctx, "credentials-fixture."+field, host)
			if err == nil || strings.Contains(got, value) {
				t.Fatal("wrong host resolved credential")
			}
		}
	}
	v.LockVault()
	if _, err := (browserSecrets{BrowserWebOptions{Vault: v}}).Resolve(ctx, "credentials-fixture.password", "example.com"); err == nil {
		t.Fatal("locked vault resolved credential")
	}
}
