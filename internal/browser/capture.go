package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
	pw "github.com/mxschmitt/playwright-go"
)

// Fixed observer functions only; tool arguments never become executable source.
const refScript = `function(ref) {
 if (!this.getClientRects().length || getComputedStyle(this).visibility === 'hidden') return null;
 this.setAttribute('data-excalibur-ref',ref);
 return {type:this.type || this.tagName.toLowerCase(), visible:true,
 enabled:!this.disabled && this.getAttribute('aria-disabled') !== 'true',
 value:this.type === 'password' ? '[secret]' : String(this.value || ''),
 checked:!!this.checked,expanded:this.getAttribute('aria-expanded')};
}`

// accessibilityElements uses Chrome's actual accessibility tree for role/name,
// then binds each backend DOM node to a ref. Duplicate accessible names are safe.
func (e *Engine) accessibilityElements(t *tab, id string) ([]element, string, error) {
	cdp, err := e.browser.NewCDPSession(t.page)
	if err != nil {
		return nil, "", err
	}
	defer cdp.Detach()
	tree, err := cdp.Send("Accessibility.getFullAXTree", nil)
	if err != nil {
		return nil, "", err
	}
	encoded, err := json.Marshal(tree)
	if err != nil {
		return nil, "", err
	}
	var ax struct {
		Nodes []struct {
			Ignored bool `json:"ignored"`
			Backend int  `json:"backendDOMNodeId"`
			Role    struct {
				Value string `json:"value"`
			} `json:"role"`
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(encoded, &ax); err != nil {
		return nil, "", err
	}
	elements := []element{}
	for _, node := range ax.Nodes {
		if node.Ignored || node.Backend == 0 {
			continue
		}
		switch node.Role.Value {
		case "button", "link", "textbox", "searchbox", "checkbox", "radio", "combobox", "listbox", "option", "slider", "spinbutton", "switch", "tab", "menuitem", "menuitemcheckbox", "menuitemradio", "treeitem":
		default:
			continue
		}
		resolved, err := cdp.Send("DOM.resolveNode", map[string]any{"backendNodeId": node.Backend})
		if err != nil {
			continue
		}
		object, ok := resolved.(map[string]any)["object"].(map[string]any)
		if !ok {
			continue
		}
		objectID, ok := object["objectId"].(string)
		if !ok {
			continue
		}
		ref := fmt.Sprintf("r%d", len(elements)+1)
		value, callErr := cdp.Send("Runtime.callFunctionOn", map[string]any{"objectId": objectID, "functionDeclaration": refScript, "arguments": []map[string]any{{"value": id + "-" + ref}}, "returnByValue": true})
		_, _ = cdp.Send("Runtime.releaseObject", map[string]any{"objectId": objectID})
		if callErr != nil {
			continue
		}
		result, ok := value.(map[string]any)["result"].(map[string]any)
		if !ok || result["value"] == nil {
			continue
		}
		raw, _ := json.Marshal(result["value"])
		var el element
		if json.Unmarshal(raw, &el) != nil {
			continue
		}
		el.Ref, el.Role, el.Name = ref, node.Role.Value, node.Name.Value
		elements = append(elements, el)
		if len(elements) >= 60 {
			break
		}
	}
	text, err := t.page.Evaluate(`() => document.body?.innerText || ''`)
	if err != nil {
		return nil, "", err
	}
	visible, _ := text.(string)
	return elements, visible, nil
}

type element struct {
	Ref      string `json:"ref"`
	Role     string `json:"role"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Visible  bool   `json:"visible"`
	Enabled  bool   `json:"enabled"`
	Value    string `json:"value"`
	Checked  bool   `json:"checked"`
	Expanded any    `json:"expanded"`
}

func (e *Engine) snapshot(ctx context.Context, t *tab) capabilities.TaskResult {
	if !t.ready {
		return failed(capabilities.NotReadyMessage, capabilities.ErrInput)
	}
	id := e.id("snap")
	interactive, text, err := e.accessibilityElements(t, id)
	if err != nil {
		return failed("browser: snapshot unavailable", capabilities.ErrTransient)
	}
	captured := struct {
		Interactive []element
		Text        string
	}{interactive, text}
	t.refs = map[string]Target{}
	t.snapshot = id
	for i := range captured.Interactive {
		el := &captured.Interactive[i]
		t.refs[el.Ref] = Target{Role: el.Role, Name: el.Name}
		el.Name, el.Value = short(e.clean(el.Name)), short(e.clean(el.Value))
	}
	dom, err := t.page.Content()
	if err != nil {
		return failed("browser: DOM capture failed", capabilities.ErrTransient)
	}
	domRef, err := e.cfg.Blobs.Put(ctx, "dom", []byte(e.clean(dom)))
	if err != nil {
		return failed("browser: blob storage failed", capabilities.ErrTransient)
	}
	options := pw.PageScreenshotOptions{FullPage: pw.Bool(false), Timeout: pw.Float(10000)}
	// A page can echo a typed secret anywhere, including images/canvas. Mask the
	// entire capture after secret entry instead of persisting pixels containing it.
	if t.private {
		options.Style = pw.String("* { visibility: hidden !important; }")
	}
	shot, err := t.page.Screenshot(options)
	if err != nil {
		return failed("browser: screenshot failed", capabilities.ErrTransient)
	}
	shotRef, err := e.cfg.Blobs.Put(ctx, "screenshot", shot)
	if err != nil {
		return failed("browser: blob storage failed", capabilities.ErrTransient)
	}
	e.detect(t)
	payload := map[string]any{"snapshot_id": id, "tab_id": t.id, "url": e.safeURL(t.url), "title": e.clean(t.title), "loading": false, "interactive": captured.Interactive, "text": boundedText(e.clean(captured.Text), 4000), "capture_ref": shotRef, "dom_ref": domRef, "capture_masked": t.private}
	// Bound actual encoded bytes, including escaping and multibyte text.
	for {
		encoded, _ := json.Marshal(payload)
		if len(encoded) <= inlineBudget {
			break
		}
		if len(captured.Interactive) > 0 {
			captured.Interactive = captured.Interactive[:len(captured.Interactive)-1]
			payload["interactive"] = captured.Interactive
		} else {
			text := payload["text"].(string)
			if len(text) == 0 {
				return failed("browser: capture metadata exceeds output budget", capabilities.ErrPermanent)
			}
			runes := []rune(text)
			payload["text"] = string(runes[:len(runes)/2])
		}
		payload["truncated"] = true
	}
	return complete(id, payload)
}

type eventLog struct {
	mu        sync.Mutex
	console   []map[string]any
	network   []map[string]any
	navStatus int
}

func appendBounded(entries []map[string]any, v map[string]any) []map[string]any {
	entries = append(entries, v)
	if len(entries) > 50 {
		entries = entries[len(entries)-50:]
	}
	return entries
}
func short(s string) string {
	r := []rune(s)
	if len(r) > 500 {
		return string(r[:500]) + "…"
	}
	return s
}
func (e *Engine) listen(t *tab) {
	e.listenLiveClose(t.live)
	if e.hasLiveObserver() || e.LiveWatched(t.session) {
		e.listenLive(t.live)
	}
	events := t.events
	t.page.OnConsole(func(message pw.ConsoleMessage) {
		events.mu.Lock()
		defer events.mu.Unlock()
		events.console = appendBounded(events.console, map[string]any{"type": message.Type(), "text": short(e.clean(message.Text()))})
	})
	page := t.page
	t.page.OnResponse(func(response pw.Response) {
		r := response.Request()
		events.mu.Lock()
		defer events.mu.Unlock()
		if r.IsNavigationRequest() && r.Frame() == page.MainFrame() {
			events.navStatus = response.Status()
		}
		events.network = appendBounded(events.network, map[string]any{"method": r.Method(), "url": short(e.safeURL(r.URL())), "status": response.Status(), "type": r.ResourceType(), "timing": copyTiming(r)})
	})
	t.page.OnRequestFailed(func(r pw.Request) {
		events.mu.Lock()
		defer events.mu.Unlock()
		events.network = appendBounded(events.network, map[string]any{"method": r.Method(), "url": short(e.safeURL(r.URL())), "status": 0, "type": r.ResourceType(), "timing": copyTiming(r), "failed": true})
	})
}
func (e *Engine) devtools(t *tab, a arguments) capabilities.TaskResult {
	kind := a.str("type")
	if kind == "" {
		kind = "console"
	}
	var entries any
	switch kind {
	case "console":
		t.events.mu.Lock()
		entries = append([]map[string]any{}, t.events.console...)
		t.events.mu.Unlock()
	case "network":
		t.events.mu.Lock()
		entries = append([]map[string]any{}, t.events.network...)
		t.events.mu.Unlock()
	case "performance":
		cdp, err := e.browser.NewCDPSession(t.page)
		if err != nil {
			return failed("browser: performance unavailable", capabilities.ErrTransient)
		}
		defer cdp.Detach()
		if _, err = cdp.Send("Performance.enable", nil); err != nil {
			return failed("browser: performance unavailable", capabilities.ErrTransient)
		}
		entries, err = cdp.Send("Performance.getMetrics", nil)
		if err != nil {
			return failed("browser: performance unavailable", capabilities.ErrTransient)
		}
	default:
		return failed("browser: unknown devtools type", capabilities.ErrInput)
	}
	raw, _ := json.Marshal(entries)
	var decoded any
	_ = json.Unmarshal(raw, &decoded)
	cleanedRaw, _ := json.Marshal(e.cleanJSON(decoded))
	cleaned := string(cleanedRaw)
	if len(cleaned) > inlineBudget-1024 {
		// Full records stay retrievable; inline entries remain a valid JSON array.
		ref, err := e.cfg.Blobs.Put(context.Background(), "devtools", []byte(cleaned))
		if err != nil {
			return failed("browser: blob storage failed", capabilities.ErrTransient)
		}
		return complete(t.id, map[string]any{"type": kind, "entries": []any{}, "blob": ref, "truncated": true})
	}
	var safe any
	_ = json.Unmarshal([]byte(cleaned), &safe)
	return complete(t.id, map[string]any{"type": kind, "entries": safe})
}
func (e *Engine) emulate(t *tab, a arguments) capabilities.TaskResult {
	width, height := int(a.number("width", 1280)), int(a.number("height", 720))
	var viewport struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if json.Unmarshal(a["viewport"], &viewport) == nil && viewport.Width > 0 && viewport.Height > 0 {
		width, height = viewport.Width, viewport.Height
	}
	mobile, touch, scale, agent := false, false, float64(1), ""
	if name := a.str("device"); name != "" {
		device := e.driver.Devices[name]
		if device == nil {
			return failed("browser: unknown device", capabilities.ErrInput)
		}
		width, height = device.Viewport.Width, device.Viewport.Height
		mobile, touch, scale, agent = device.IsMobile, device.HasTouch, device.DeviceScaleFactor, device.UserAgent
	}
	if width < 1 || height < 1 || width > 10000 || height > 10000 {
		return failed("browser: invalid viewport", capabilities.ErrInput)
	}
	cdp := t.emulation
	var err error
	if cdp == nil {
		cdp, err = e.browser.NewCDPSession(t.page)
		if err != nil {
			return failed("browser: emulation unavailable", capabilities.ErrTransient)
		}
		t.emulation = cdp
	}
	// Chrome discards emulation overrides when their CDP session detaches.
	// Keep this session owned by the tab until its page closes.
	if err = t.page.SetViewportSize(width, height); err != nil {
		return failed("browser: viewport unavailable", capabilities.ErrTransient)
	}
	if _, err = cdp.Send("Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "mobile": mobile, "deviceScaleFactor": scale}); err != nil {
		return failed("browser: emulation unavailable", capabilities.ErrTransient)
	}
	if _, err = cdp.Send("Emulation.setTouchEmulationEnabled", map[string]any{"enabled": touch}); err != nil {
		return failed("browser: emulation unavailable", capabilities.ErrTransient)
	}
	if agent != "" {
		if _, err = cdp.Send("Emulation.setUserAgentOverride", map[string]any{"userAgent": agent}); err != nil {
			return failed("browser: emulation unavailable", capabilities.ErrTransient)
		}
	}
	if locale := a.str("locale"); locale != "" {
		if _, err = cdp.Send("Emulation.setLocaleOverride", map[string]any{"locale": locale}); err != nil {
			return failed("browser: locale unavailable", capabilities.ErrInput)
		}
	}
	if scheme := a.str("color_scheme"); scheme != "" {
		if scheme != "light" && scheme != "dark" && scheme != "no-preference" {
			return failed("browser: invalid color scheme", capabilities.ErrInput)
		}
		if err = t.page.EmulateMedia(pw.PageEmulateMediaOptions{ColorScheme: colorScheme(scheme)}); err != nil {
			return failed("browser: color scheme unavailable", capabilities.ErrTransient)
		}
	}
	return complete(t.id, map[string]any{"emulated": strings.TrimSpace(a.str("device")), "viewport": map[string]int{"width": width, "height": height}, "locale": a.str("locale"), "color_scheme": a.str("color_scheme")})
}

func colorScheme(value string) *pw.ColorScheme { scheme := pw.ColorScheme(value); return &scheme }

func (e *Engine) cleanJSON(value any) any {
	switch v := value.(type) {
	case string:
		return e.clean(v)
	case []any:
		for i := range v {
			v[i] = e.cleanJSON(v[i])
		}
		return v
	case map[string]any:
		for key, item := range v {
			v[key] = e.cleanJSON(item)
		}
		return v
	}
	return value
}

func boundedText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

// Copy timing on the event dispatcher before Playwright mutates completion fields.
func copyTiming(request pw.Request) pw.RequestTiming {
	if timing := request.Timing(); timing != nil {
		return *timing
	}
	return pw.RequestTiming{}
}
