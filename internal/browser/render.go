package browser

import (
	"context"
	"errors"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	pw "github.com/mxschmitt/playwright-go"
)

// Render uses the engine's Chrome process, with a separate ephemeral context so
// public page reads cannot access profile credentials. Every network request is
// fulfilled by fetch, which must enforce the caller's SSRF and size policies.
func (e *Engine) Render(ctx context.Context, raw string, fetch func(context.Context, string) ([]byte, string, error)) (string, string, error) {
	if err := supported(); err != nil {
		return "", "", err
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	e.opMu.Lock()
	defer e.opMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if e.closed {
		return "", "", errors.New("browser: engine closed")
	}
	if err := e.start(); err != nil {
		return "", "", err
	}
	e.lastUse = e.cfg.Clock()
	isolated, err := e.browser.Browser().NewContext(pw.BrowserNewContextOptions{ServiceWorkers: pw.ServiceWorkerPolicyBlock, AcceptDownloads: pw.Bool(false)})
	if err != nil {
		return "", "", errors.New("browser: renderer unavailable")
	}
	defer isolated.Close()
	done := make(chan struct{})
	joined := make(chan struct{})
	safego.Go("browser renderer cancellation", nil, func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = isolated.Close()
		case <-done:
		}
	})
	defer func() { close(done); <-joined }()
	if err = isolated.Route("**/*", func(route pw.Route) {
		req := route.Request()
		if req.Method() != "GET" {
			_ = route.Abort()
			return
		}
		body, kind, err := fetch(ctx, req.URL())
		if err != nil {
			_ = route.Abort()
			return
		}
		_ = route.Fulfill(pw.RouteFulfillOptions{Status: pw.Int(200), Body: body, ContentType: pw.String(kind)})
	}); err != nil {
		return "", "", errors.New("browser: renderer guard unavailable")
	}
	if err = isolated.RouteWebSocket("**/*", func(ws pw.WebSocketRoute) { ws.Close() }); err != nil {
		return "", "", errors.New("browser: websocket guard unavailable")
	}
	page, err := isolated.NewPage()
	if err != nil {
		return "", "", errors.New("browser: render page unavailable")
	}
	timeout := 15 * time.Second
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		return "", "", context.DeadlineExceeded
	}
	if _, err = page.Goto(raw, pw.PageGotoOptions{WaitUntil: pw.WaitUntilStateNetworkidle, Timeout: pw.Float(float64(timeout.Milliseconds()))}); err != nil {
		return "", "", errors.New("browser: render navigation failed")
	}
	body, err := page.Content()
	if err != nil {
		return "", "", errors.New("browser: render capture failed")
	}
	if len(body) > 5<<20 {
		return "", "", errors.New("browser: rendered response too large")
	}
	return body, page.URL(), nil
}
