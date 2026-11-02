package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

func principalDB(t *testing.T, path string) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), path, store.DefaultOptions(), migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func TestPrincipalSeedsOnceAcrossConcurrentStarts(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	t.Setenv("EXCALIBUR_USER_NAME", "Ana Prueba")
	t.Setenv("LC_ALL", "es_ES.UTF-8")
	t.Setenv("TZ", "Europe/Madrid")
	path := filepath.Join(t.TempDir(), "principal.db")
	first, second := principalDB(t, path), principalDB(t, path)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		db := first
		if i%2 == 1 {
			db = second
		}
		wg.Add(1)
		safego.Go("principal-seed-test", nil, func() { defer wg.Done(); _, err := NewPrincipalService(context.Background(), db); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	svc, err := NewPrincipalService(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != (api.PrincipalGetResult{ID: "local", Name: "Ana Prueba", Timezone: "Europe/Madrid", Language: "es"}) {
		t.Fatalf("profile: %+v", got)
	}
	if err := first.ReadInteractive(context.Background(), func(ctx context.Context, q store.Queryer) error {
		var count int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Errorf("rows: %d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPrincipalExistingRowUntouchedAndBackendWiring(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	t.Setenv("EXCALIBUR_USER_NAME", "Must not replace")
	db := principalDB(t, filepath.Join(t.TempDir(), "principal.db"))
	ctx := context.Background()
	if err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language,created_at,updated_at) VALUES ('original','Existing','UTC','en',10,20)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := NewPrincipalService(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	b := NewDaemonBackend(nil, nil, nil)
	b.AttachServices(BackendServices{Principal: svc})
	result, err := b.Passthrough(ctx, api.MethodPrincipalGet, json.RawMessage(`{"ignored":true}`))
	if err != nil {
		t.Fatal(err)
	}
	got := result.(api.PrincipalGetResult)
	if got.ID != "original" || got.Name != "Existing" || got.Timezone != "UTC" || got.Language != "en" {
		t.Fatalf("changed existing: %+v", got)
	}
	if err := db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
		var created, updated int
		if err := q.QueryRow(ctx, `SELECT created_at,updated_at FROM principals`).Scan(&created, &updated); err != nil {
			return err
		}
		if created != 10 || updated != 20 {
			t.Error("timestamps changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = b.Passthrough(ctx, api.MethodPrincipalGet, json.RawMessage(`{`))
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInvalidParams {
		t.Fatalf("params: %v", err)
	}
	_ = db.Close()
	_, err = b.Passthrough(ctx, api.MethodPrincipalGet, nil)
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInternal {
		t.Fatalf("closed store: %v", err)
	}
}
func TestPrincipalOSFallbackAndDisabled(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	t.Setenv("EXCALIBUR_USER_NAME", "")
	expected, err := platform.CurrentUserName()
	if err != nil {
		t.Fatal(err)
	}
	db := principalDB(t, filepath.Join(t.TempDir(), "principal.db"))
	svc, err := NewPrincipalService(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background())
	if err != nil || got.Name != expected {
		t.Fatalf("fallback: %+v %v", got, err)
	}
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "0")
	disabled, err := NewPrincipalService(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = disabled.Get(context.Background())
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeMethodNotFound {
		t.Fatalf("disabled: %v", err)
	}
}
func TestPrincipalDefaults(t *testing.T) {
	for _, tc := range []struct{ locale, language string }{{"es_MX.UTF-8", "es"}, {"en_US", "en"}, {"C", "en"}, {"", "en"}} {
		env := func(key string) string {
			if key == "LANG" {
				return tc.locale
			}
			return ""
		}
		if got := principalLanguage(env); got != tc.language {
			t.Errorf("%s: %s", tc.locale, got)
		}
	}
	name, err := principalName(func(string) string { return "  Override  " }, func() (string, error) { t.Fatal("OS lookup despite override"); return "", nil })
	if err != nil || name != "Override" {
		t.Fatalf("name %q %v", name, err)
	}
}

type failingPrincipalStore struct {
	principalStore
	readErr  error
	writeErr error
}

func (f *failingPrincipalStore) ReadInteractive(ctx context.Context, fn store.ReadFunc) error {
	if f.readErr != nil {
		return f.readErr
	}
	return f.principalStore.ReadInteractive(ctx, fn)
}
func (f *failingPrincipalStore) Write(ctx context.Context, priority store.Priority, fn store.WriteFunc) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	return f.principalStore.Write(ctx, priority, fn)
}

func TestPrincipalInitializationFailuresDoNotStopDaemon(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	t.Setenv("EXCALIBUR_USER_NAME", "")
	t.Setenv("USER", "fallback-login")
	t.Setenv("LOGNAME", "ignored-login")
	for _, failure := range []string{"account", "inspect", "write", "missing-store"} {
		t.Run(failure, func(t *testing.T) {
			db := principalDB(t, filepath.Join(t.TempDir(), "profile.db"))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fault := &failingPrincipalStore{principalStore: db}
			lookup := func() (string, error) { return "Account Name", nil }
			switch failure {
			case "account":
				lookup = func() (string, error) { return "", errors.ErrUnsupported }
			case "inspect":
				fault.readErr = errors.New("inspection unavailable")
			case "write":
				fault.writeErr = errors.New("seed write unavailable")
			}
			var database principalStore = fault
			if failure == "missing-store" {
				database = nil
			}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			svc, err := newPrincipalService(ctx, database, lookup, logger)
			if err != nil || svc == nil {
				t.Fatalf("profile blocked startup: %v", err)
			}
			backend := NewDaemonBackend(nil, nil, nil)
			backend.AttachServices(BackendServices{Principal: svc, Config: &FileConfig{}})
			gate := gateway.New(gateway.DefaultConfig(), backend, nil, nil)
			instance := New(DefaultConfig(), Deps{Gate: gate, Backend: backend})
			addr, err := instance.Start(ctx)
			if err != nil {
				t.Fatalf("daemon did not start: %v", err)
			}
			t.Cleanup(func() { _ = instance.Stop(context.Background()) })
			client := &http.Client{Timeout: time.Second}
			call := func(method string) api.Response {
				t.Helper()
				req, err := api.NewRequest("profile-test", method, nil)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Post("http://"+addr+"/rpc", "application/json", bytes.NewReader(raw))
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				var result api.Response
				if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			profile := call(api.MethodPrincipalGet)
			if failure == "account" {
				if profile.Error != nil {
					t.Fatal(profile.Error)
				}
				var result api.PrincipalGetResult
				if err := json.Unmarshal(profile.Result, &result); err != nil {
					t.Fatal(err)
				}
				if result.Name != "fallback-login" {
					t.Fatalf("fallback not seeded: %+v", result)
				}
			} else {
				if profile.Error == nil || profile.Error.Code != api.CodeInternal || profile.Error.Message != "could not read local profile" {
					t.Fatalf("profile error: %+v", profile)
				}
				if !strings.Contains(logs.String(), "level=WARN") {
					t.Fatalf("no warning: %s", logs.String())
				}
			}
			config := call(api.MethodConfigList)
			if config.Error != nil || !bytes.Contains(config.Result, []byte(`"sections"`)) {
				t.Fatalf("config unavailable: %+v", config)
			}
			if !instance.Running() {
				t.Fatal("profile error stopped the daemon")
			}
			if failure == "write" {
				if err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
					_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language) VALUES ('local','Recovered','UTC','en')`)
					return err
				}); err != nil {
					t.Fatal(err)
				}
				recovered := call(api.MethodPrincipalGet)
				if recovered.Error != nil || !bytes.Contains(recovered.Result, []byte(`"name":"Recovered"`)) {
					t.Fatalf("profile failed to recover: %+v", recovered)
				}
			}
		})
	}
}

func TestPrincipalNameFallbackChain(t *testing.T) {
	for _, tc := range []struct {
		label, override, account, user, login, want string
		err                                         error
	}{
		{label: "explicit override", override: "  Explicit Name  ", account: "Ignored", user: "env-user", login: "env-login", want: "Explicit Name"},
		{label: "OS full name", account: "  Full Name  ", user: "env-user", want: "Full Name"},
		{label: "OS login", account: "account-login", user: "env-user", want: "account-login"},
		{label: "USER after lookup failure", user: "  env-user  ", login: "env-login", err: errors.New("no passwd entry"), want: "env-user"},
		{label: "LOGNAME after unsupported lookup", user: "  ", login: "env-login", err: errors.ErrUnsupported, want: "env-login"},
		{label: "empty OS name", account: "  ", login: "env-login", want: "env-login"},
		{label: "final fallback", err: errors.ErrUnsupported, want: "User"},
		{label: "all empty", want: "User"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			env := func(key string) string {
				switch key {
				case "EXCALIBUR_USER_NAME":
					return tc.override
				case "USER":
					return tc.user
				case "LOGNAME":
					return tc.login
				}
				return ""
			}
			name, err := principalName(env, func() (string, error) {
				if strings.TrimSpace(tc.override) != "" {
					t.Fatal("OS lookup despite explicit override")
				}
				return tc.account, tc.err
			})
			if err != nil || name != tc.want {
				t.Fatalf("name=%q error=%v, want %q", name, err, tc.want)
			}
		})
	}
}
