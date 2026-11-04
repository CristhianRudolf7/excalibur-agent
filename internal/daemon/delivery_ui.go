package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
)

type deliveryUIEvidence struct {
	Check  string `json:"check"`
	Report string `json:"report"`
}

// The browser suite discovers the union of controls across screen states before
// recording assertions. Keeping inventory separate makes omitted checks visible.
type deliveryBrowserReport struct {
	Runner  string                  `json:"runner"`
	Screens []deliveryBrowserScreen `json:"screens"`
	Checks  []deliveryControlCheck  `json:"checks"`
}

type deliveryBrowserScreen struct {
	Name       string            `json:"name"`
	URL        string            `json:"url"`
	Discovered int               `json:"discovered"`
	Controls   []deliveryControl `json:"controls"`
}

type deliveryControl struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Selector   string `json:"selector"`
	Kind       string `json:"kind"`
	Form       string `json:"form,omitempty"`
	Repeatable bool   `json:"repeatable,omitempty"`
}

type deliveryControlCheck struct {
	Screen  string          `json:"screen"`
	Control string          `json:"control"`
	Action  string          `json:"action"`
	Effect  string          `json:"effect"`
	Target  string          `json:"target"`
	Before  json.RawMessage `json:"before"`
	After   json.RawMessage `json:"after"`
	Passed  bool            `json:"passed"`
}

func (e *deliveryEvidence) checkUIEvidence(root *os.Root, manifest deliveryManifest, sources []deliverySource) []string {
	evidence := manifest.UI
	if !e.passed[evidence.Check] {
		return []string{"ui.check must name a successful executed browser suite covering every interactive control"}
	}
	if !fs.ValidPath(evidence.Report) {
		return []string{"ui.report must name a browser-generated JSON report inside the workspace"}
	}
	file, err := root.Open(evidence.Report)
	if err != nil {
		return []string{"browser control report unavailable: " + evidence.Report}
	}
	defer file.Close()
	var report deliveryBrowserReport
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return []string{"invalid browser control report: " + err.Error()}
	}
	if decoder.Decode(new(any)) != io.EOF {
		return []string{"invalid browser control report: expected a single JSON object"}
	}
	var pending []string
	add := func(format string, args ...any) { pending = append(pending, fmt.Sprintf(format, args...)) }
	if report.Runner != "playwright" && report.Runner != "puppeteer" && report.Runner != "webdriver" {
		add("ui.report must come from an executed playwright, puppeteer or webdriver browser suite")
	}
	checked := e.checkedAt[evidence.Check]
	info, err := file.Stat()
	if err != nil || info.ModTime().After(checked) {
		add("browser report changed after its check; rerun %s", evidence.Check)
	}
	for _, source := range sources {
		if source.changed.After(checked) || err == nil && source.changed.After(info.ModTime()) {
			add("%s:1: source changed after browser evidence; regenerate the report and rerun %s", source.path, evidence.Check)
		}
	}
	screens := map[string]bool{}
	controls := map[string]deliveryControl{}
	var controlKeys []string
	for _, screen := range report.Screens {
		if screens[screen.Name] || strings.TrimSpace(screen.Name) == "" || strings.Contains(screen.Name, "/") {
			add("browser inventory needs unique nonempty screen names: %s", screen.Name)
		}
		screens[screen.Name] = true
		address, err := url.Parse(screen.URL)
		if err != nil || address.Host == "" || address.Scheme != "http" && address.Scheme != "https" {
			add("browser inventory %s needs the actual visited application URL", screen.Name)
		}
		if screen.Discovered != len(screen.Controls) || screen.Discovered < 0 || screen.Controls == nil {
			add("browser inventory %s must enumerate every discovered interactive control, including conditional states", screen.Name)
		}
		for _, control := range screen.Controls {
			key := screen.Name + "/" + control.ID
			if _, exists := controls[key]; exists || strings.TrimSpace(control.ID) == "" || strings.TrimSpace(control.Label) == "" || strings.TrimSpace(control.Selector) == "" {
				add("browser control %s needs a unique ID, label and DOM selector", key)
			}
			if !validControlKind(control.Kind) || control.Repeatable && control.Kind != "form" {
				add("browser control %s has an invalid kind or repeatable declaration", key)
			}
			controls[key] = control
			controlKeys = append(controlKeys, key)
		}
	}
	for _, screen := range manifest.Screens {
		if !screens[screen.Name] {
			add("browser inventory and control checks missing for screen %s", screen.Name)
		}
	}
	if len(report.Screens) == 0 {
		add("browser report must inventory every screen and its interactive controls")
	}
	passed := map[string]bool{}
	saved := map[string]bool{}
	for _, check := range report.Checks {
		key := check.Screen + "/" + check.Control
		control, exists := controls[key]
		if !exists {
			add("browser check %s has no discovered control", key)
			continue
		}
		if !check.Passed || strings.TrimSpace(check.Action) == "" || strings.TrimSpace(check.Target) == "" || !controlEffect(control.Kind, check) {
			add("browser control %s (%s) failed to prove its labelled data/state effect; notice-only or unchanged results fail", key, control.Label)
			continue
		}
		passed[key] = true
		if check.Effect == "records" {
			saved[key] = true
		}
	}
	sort.Strings(controlKeys)
	for _, key := range controlKeys {
		control := controls[key]
		if !passed[key] {
			add("execute a browser assertion for %s (%s)", key, control.Label)
		}
		if control.Repeatable {
			if !saved[key] {
				add("repeatable form %s needs a successful submit/read-back browser assertion using records", key)
			}
			screen := strings.SplitN(key, "/", 2)[0]
			for _, kind := range []string{"add-item", "remove-item"} {
				found := false
				for candidate, other := range controls {
					if strings.HasPrefix(candidate, screen+"/") && other.Form == control.ID && other.Kind == kind && passed[candidate] {
						found = true
					}
				}
				if !found {
					add("repeatable form %s needs an executed %s browser check proving variable-length items", key, kind)
				}
			}
		}
		if control.Kind == "add-item" || control.Kind == "remove-item" {
			screen := strings.SplitN(key, "/", 2)[0]
			form, exists := controls[screen+"/"+control.Form]
			if !exists || form.Kind != "form" || !form.Repeatable {
				add("browser control %s must reference its repeatable form", key)
			}
		}
	}
	return pending
}

func validControlKind(kind string) bool {
	switch kind {
	case "search", "filter", "button", "submit", "form", "detail", "field", "link", "add-item", "remove-item":
		return true
	}
	return false
}
