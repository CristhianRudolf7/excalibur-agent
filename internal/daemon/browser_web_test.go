package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestDaemonBrowserSwap(t *testing.T) {
	for _, available := range []bool{false, true} {
		t.Run(map[bool]string{true: "available", false: "missing"}[available], func(t *testing.T) {
			var warnings []string
			svc, err := Assemble(AssembleOptions{BrowserWeb: &wireengine.BrowserWebOptions{Config: config.Default(), DataDir: t.TempDir(), Available: func() error {
				if available {
					return nil
				}
				return errors.New("missing")
			}, Warn: func(s string) { warnings = append(warnings, s) }}})
			if err != nil {
				t.Fatal(err)
			}
			defer svc.BrowserWeb.Close()
			cap, _ := svc.Caps.Lookup("browser")
			if available {
				if _, ok := cap.(*browser.Engine); !ok {
					t.Fatal("not real")
				}
			} else {
				if _, ok := cap.(*capabilities.BrowserFake); !ok || len(warnings) != 1 || !strings.Contains(warnings[0], "browser disabled:") {
					t.Fatal("missing fake or warning")
				}
			}
			if !svc.Catalog.IsDeferred("browser_navigate") {
				t.Fatal("browser not deferred")
			}
		})
	}
}
func TestBrowserInstallNonfatalReport(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		installErr, availableErr error
		want                     string
	}{
		{name: "ready", want: "browser: ready"}, {name: "missing", availableErr: errors.New("missing"), want: "Chrome not found"}, {name: "driver", installErr: errors.New("offline"), want: "driver install failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			in := Installer{Dirs: fakeDirs{t.TempDir()}, BrowserStep: func() string {
				return browserInstallStep(context.Background(), func(context.Context) error { calls++; return tc.installErr }, func() error { return tc.availableErr })
			}}
			rep, err := in.Install(InstallOptions{})
			if err != nil || calls != 1 || !strings.Contains(rep.Browser, tc.want) {
				t.Fatalf("install: %+v %v", rep, err)
			}
		})
	}
}
