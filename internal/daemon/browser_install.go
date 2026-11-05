package daemon

import (
	"context"
	"github.com/CristhianRudolf7/excalibur-agent/internal/browser"
	"os"
	"time"
)

// InstallBrowserDriver is the production, driver-only install step. System
// Chrome is supplied by the operator; failure is a nonfatal report note.
func InstallBrowserDriver() string {
	if os.Getenv("EXCALIBUR_SKIP_BROWSER_INSTALL") == "1" {
		return "browser: driver installation deferred by operator"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return browserInstallStep(ctx, func(ctx context.Context) error { return browser.Install(ctx, browser.InstallOptions{}) }, browser.Available)
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
