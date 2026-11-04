package browser

import pw "github.com/mxschmitt/playwright-go"

func platformBrowserLaunch(*pw.BrowserTypeLaunchPersistentContextOptions) error { return nil }

func runBrowserDriver() (*pw.Playwright, error) { return pw.Run() }
