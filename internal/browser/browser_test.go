package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

func TestDefaultsAndManifest(t *testing.T) {
	c := (Config{ProfileDir: filepath.Join(t.TempDir(), "profile")}).defaults()
	if !*c.Sandbox || !*c.Headless || c.Channel != "chrome" || c.MaxTabs <= 0 || c.Blobs == nil || c.Clock == nil {
		t.Fatalf("unsafe defaults: %+v", c)
	}
	f := false
	c = (Config{Sandbox: &f, Headless: &f}).defaults()
	if *c.Sandbox || *c.Headless {
		t.Fatal("explicit false lost")
	}
	f = true
	if *c.Sandbox {
		t.Fatal("mutable policy")
	}
	if !reflect.DeepEqual(New(Config{}).Manifest(), capabilities.BrowserManifest()) {
		t.Fatal("manifest drift")
	}
}
func TestURLPolicy(t *testing.T) {
	for _, raw := range []string{"https://example.com/a", "http://127.0.0.1:8000/", "about:blank"} {
		if err := allowedURL(raw); err != nil {
			t.Errorf("allowed %s: %v", raw, err)
		}
	}
	for _, raw := range []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/html,hi", "ftp://x", "about:config", "https:///", "//example.com", "https://user:pass@example.com", "https://example.com/\n"} {
		if allowedURL(raw) == nil {
			t.Errorf("allowed dangerous URL %s", raw)
		}
	}
	raw := redactURL("https://user:password@a.test/?api_key=private&query=public&access_token=x#secret")
	if strings.Contains(raw, "private") || strings.Contains(raw, "password") || strings.Contains(raw, "=x") || !strings.Contains(raw, "query=public") {
		t.Fatal(raw)
	}
}
func TestUploadPolicy(t *testing.T) {
	root := t.TempDir()
	allowed := filepath.Join(root, "allowed")
	if err := os.Mkdir(allowed, 0700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(allowed, "ok")
	outside := filepath.Join(root, "outside")
	for _, p := range []string{inside, outside} {
		if err := os.WriteFile(p, []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := allowedUpload(inside, []string{allowed}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{outside, allowed, link, filepath.Join(allowed, "missing")} {
		if _, err := allowedUpload(p, []string{allowed}); err == nil {
			t.Errorf("allowed %s", p)
		}
	}
}
func TestSecretDomainAndBlobCopies(t *testing.T) {
	secret := MemorySecrets{"login": {Value: "sentinel-credential", Hosts: []string{"example.com"}}}
	for _, host := range []string{"evil.com", "sub.example.com", "example.com.evil.com"} {
		value, err := secret.Resolve(context.Background(), "login", host)
		if err == nil || value != "" || strings.Contains(err.Error(), "sentinel-credential") {
			t.Fatal("domain leak")
		}
	}
	if value, err := secret.Resolve(context.Background(), "login", "EXAMPLE.COM"); err != nil || value != "sentinel-credential" {
		t.Fatal("exact host denied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := secret.Resolve(ctx, "login", "example.com"); err == nil {
		t.Fatal("ignored cancellation")
	}
	store := &MemoryBlobs{}
	data := []byte("data")
	ref, err := store.Put(context.Background(), "test", data)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 'x'
	got := store.Get(ref)
	got[0] = 'y'
	if string(store.Get(ref)) != "data" {
		t.Fatal("mutable blob")
	}
}
func TestBlockFixtures(t *testing.T) {
	cases := []struct {
		url, html string
		status    int
		want      string
	}{
		{"https://x/", "<script src='https://www.google.com/recaptcha/api.js'></script>", 200, capabilities.BlockedCaptcha},
		{"https://x/", "<iframe src='https://hcaptcha.com/challenge'></iframe>", 200, capabilities.BlockedCaptcha},
		{"https://x/", "<script src='https://challenges.cloudflare.com/turnstile/v0/api.js'></script>", 200, capabilities.BlockedCaptcha},
		{"https://x/", "<h1>Verify you are human</h1>", 200, capabilities.BlockedCaptcha},
		{"https://x/", "<input TYPE = 'password'>", 200, capabilities.BlockedLogin},
		{"https://x/signin", "", 200, capabilities.BlockedLogin},
		{"https://x/", "<input autocomplete='one-time-code'>", 200, capabilities.Blocked2FA},
		{"https://x/", "<input id='otp'>", 200, capabilities.Blocked2FA},
		{"https://x/", "<h1>Access denied</h1>", 200, capabilities.BlockedBySite},
		{"https://x/", "", 403, capabilities.BlockedBySite}, {"https://x/", "", 429, capabilities.BlockedBySite},
		{"https://x/catalog", "<p>captcha and password documentation</p>", 200, ""},
	}
	for _, c := range cases {
		if got := detectBlock(c.url, c.html, c.status); got != c.want {
			t.Errorf("%s %s: %q want %q", c.url, c.html, got, c.want)
		}
	}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func TestRefsAndReadiness(t *testing.T) {
	e := New(Config{})
	tab := &tab{snapshot: "snap-1", refs: map[string]Target{"r1": {Role: "button", Name: "Go"}}}
	_, r, ok := e.ref(tab, arguments{})
	if ok || r.ErrorText != capabilities.NotReadyMessage {
		t.Fatal(r)
	}
	tab.ready = true
	for _, args := range []arguments{{"snapshot_id": raw("snap-0"), "ref": raw("r1")}, {"snapshot_id": raw("snap-1"), "ref": raw("r2")}} {
		_, r, ok = e.ref(tab, args)
		if ok || !strings.HasPrefix(r.ErrorText, capabilities.StaleRefMessage) || r.ErrClass != capabilities.ErrInput {
			t.Fatal(r)
		}
	}
}
func TestRoutineStorageAndAssertions(t *testing.T) {
	dir := t.TempDir()
	steps := map[string][]Step{"../name": {{Action: "click", Target: Target{Role: "button", Name: "Save"}, Assert: raw(Assertion{Type: "text_contains", Value: "Saved"})}}}
	if err := saveRoutines(dir, steps); err != nil {
		t.Fatal(err)
	}
	got, err := loadRoutines(dir)
	if err != nil || !reflect.DeepEqual(got, steps) {
		t.Fatalf("roundtrip: %v %v", got, err)
	}
	for _, a := range []Assertion{{Type: "text_contains", Value: "Saved"}, {Type: "url_matches", Value: `/done$`}} {
		if !textAssertion(a, "Saved successfully", "https://x/done") || textAssertion(a, "bad", "https://x/no") {
			t.Fatal(a)
		}
	}
	if a, err := parseAssertion(raw("Saved")); err != nil || a.Type != "text_contains" {
		t.Fatal(a, err)
	}
	for _, bad := range []any{Assertion{Type: "eval"}, Assertion{Type: "url_matches", Value: "["}, Assertion{Type: "element_visible"}} {
		if _, err := parseAssertion(raw(bad)); err == nil {
			t.Fatal(bad)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "routines.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRoutines(dir); err == nil {
		t.Fatal("accepted corrupt storage")
	}
}
func TestIdempotencyConcurrentPollAndCancel(t *testing.T) {
	if supported() != nil {
		t.Skip("Linux only")
	}
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
	defer e.Close()
	var wg sync.WaitGroup
	results := make(chan capabilities.TaskResult, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := e.Invoke("browser_routine", raw(map[string]any{"action": "record", "routine": "routine", "steps": []Step{{Action: "click", Target: Target{Role: "button", Name: "Save"}}}}), "same")
			if err != nil {
				t.Error(err)
			}
			results <- r
		}()
	}
	wg.Wait()
	close(results)
	var first capabilities.TaskResult
	for r := range results {
		if first.State == "" {
			first = r
		}
		if !reflect.DeepEqual(first, r) || r.State != capabilities.StateCompleted {
			t.Fatal(r)
		}
	}
	if len(e.dedup) != 1 || len(e.routines) != 1 {
		t.Fatal("duplicate effects")
	}
	for i := 0; i < 2; i++ {
		r, _ := e.Poll(first.TaskID)
		if !reflect.DeepEqual(r, first) {
			t.Fatal(r)
		}
	}
	cancelled, _ := e.Cancel("missing")
	if cancelled.State != capabilities.StateCancelled {
		t.Fatal(cancelled)
	}
	r, _ := e.Invoke("browser_unknown", raw(map[string]any{}), "")
	if r.ErrClass != capabilities.ErrInput {
		t.Fatal(r)
	}
	r, _ = e.Invoke("browser_status", []byte("invalid"), "")
	if r.ErrClass != capabilities.ErrInput {
		t.Fatal(r)
	}
}

func TestEscapedSecretRedactionAndProtectedRoutines(t *testing.T) {
	e := New(Config{ProfileDir: filepath.Join(t.TempDir(), "profile")})
	defer e.Close()
	e.secrets = []string{"a\"b\ncredential"}
	source := map[string]any{"text": "before a\"b\ncredential after", "nested": []any{map[string]any{"value": "a\"b\ncredential"}}}
	encoded := raw(e.cleanJSON(source))
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "before [secret] after" {
		t.Fatal(decoded)
	}
	if supported() != nil {
		return
	}
	r, err := e.Invoke("browser_routine", raw(map[string]any{"action": "record", "routine": "protected", "steps": []Step{{Action: "type", Target: Target{Role: "textbox", Name: "Query"}, Value: e.secrets[0]}}}), "")
	if err != nil || r.ErrClass != capabilities.ErrInput || strings.Contains(r.ErrorText, "credential") {
		t.Fatal(r, err)
	}
	if _, err := os.Stat(filepath.Join(e.cfg.RoutinesDir, "routines.json")); !os.IsNotExist(err) {
		t.Fatal("persisted protected content")
	}
}

func TestAvailableMissingDriverIsHermetic(t *testing.T) {
	if supported() != nil {
		t.Skip("Linux only")
	}
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", t.TempDir())
	err := Available()
	if err == nil || !strings.Contains(err.Error(), "driver missing") {
		t.Fatal(err)
	}
}

func TestExpiredTaskIsTransientAndCancelledTaskIsCancelled(t *testing.T) {
	e := New(Config{})
	defer e.Close()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	r := e.execute(expired, "browser_status", arguments{}, nil)
	if r.ErrClass != capabilities.ErrTransient {
		t.Fatal(r)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	r = e.execute(cancelled, "browser_status", arguments{}, nil)
	if r.State != capabilities.StateCancelled {
		t.Fatal(r)
	}
}
