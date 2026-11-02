package daemon

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
)

// InstallBrowserDriver is the production, driver-only install step. System
// Chrome is supplied by the operator; failure is a nonfatal report note.
func InstallBrowserDriver() string {
	return browserInstallStep(context.Background(), func(ctx context.Context) error { return browser.Install(ctx, browser.InstallOptions{}) }, browser.Available)
}
func browserInstallStep(ctx context.Context, install func(context.Context) error, available func() error) string {
	if err := install(ctx); err != nil {
		return "browser: driver install failed: " + err.Error()
	}
	if err := available(); err != nil {
		return "browser: Chrome not found, install Google Chrome or Chromium; " + err.Error()
	}
	return "browser: ready"
}
