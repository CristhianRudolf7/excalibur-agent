package tasks

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const VisualVerifierEvidenceSwitch = "EXCALIBUR_VISUAL_VERIFIER_EVIDENCE"
const visualEvidenceMaxBytes = 1500

func VisualVerifierEvidenceEnabled() bool { return envBool(VisualVerifierEvidenceSwitch, true) }

const visualEndStateReview = "A requested visual end state (desktop, window, rendered output) must be supported by these captured-image observations. Missing or contradictory screen evidence is FAIL; name the unmet explicit request property. Process/port and file checks alone do not establish the visual end state.\n"

// Decode deferred inspection arguments as well as direct calls. Captures are
// identified by path, so a later inspection replaces older observations of it.
func imageInspectionArgs(h HistoryTool) (path, compare string, ok bool) {
	name, args := strings.TrimSpace(h.Name), h.Args
	if strings.EqualFold(name, "call_tool") {
		var wrapped struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(args), &wrapped) != nil {
			return "", "", false
		}
		name, args = wrapped.Name, string(wrapped.Arguments)
	}
	if !strings.EqualFold(name, "inspect_image") {
		return "", "", false
	}
	var a struct {
		Path    string `json:"path"`
		Compare string `json:"compare_to"`
	}
	_ = json.Unmarshal([]byte(args), &a)
	return a.Path, a.Compare, true
}

func visualClip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return strings.Repeat(".", n)
	}
	n -= 3
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

func imageEvidenceSummary(h HistoryTool, path, compare string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "inspect_image path=%q", visualClip(path, 96))
	if compare != "" {
		fmt.Fprintf(&b, " compare_to=%q", visualClip(compare, 96))
	}
	b.WriteByte('\n')
	if h.IsError {
		b.WriteString("inspection failed; current visual state unavailable\n")
		return b.String()
	}
	var screen []string
	blank, changed := "blank/uniform: unavailable", "changed_pixels: unavailable (no comparison)"
	inScreen, screenSeen := false, false
	for _, line := range strings.Split(h.Output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "blank/uniform:"):
			blank = visualClip(line, 80)
			inScreen = false
		case strings.HasPrefix(line, "changed_pixels:"):
			changed = visualClip(line, 112)
			inScreen = false
		case line == "screen_text:":
			inScreen, screenSeen = true, true
		case strings.HasPrefix(line, "screen_text:"):
			screenSeen = true
			screen = append(screen, visualClip(line, 128))
			inScreen = false
		case strings.HasPrefix(line, "ASCII (") || strings.HasPrefix(line, "OCR (") || strings.HasPrefix(line, "difference_mask:") || strings.HasPrefix(line, "[screen_text"):
			inScreen = false
		case inScreen && line != "" && len(screen) < 3:
			screen = append(screen, visualClip(line, 128))
		}
	}
	if !screenSeen {
		screen = []string{"unavailable (not extracted)"}
	}
	fmt.Fprintf(&b, "screen_text: %s\n%s\n%s\n", strings.Join(screen, " | "), blank, changed)
	return b.String()
}

// LatestVisualEvidence prioritizes recent captures independently of the ordinary
// evidence-line limit. It preserves screen text and diagnostic flags ahead of
// thumbnails and excludes older inspections of the same capture. Large capture
// sets explicitly report omissions rather than exceeding the context budget.
func LatestVisualEvidence(history []HistoryTool) string {
	if !VisualVerifierEvidenceEnabled() || !envBool("EXCALIBUR_INSPECT_IMAGE", true) {
		return ""
	}
	const omissionRoom = 64
	seen := map[string]bool{}
	var b strings.Builder
	omitted := 0
	for i := len(history) - 1; i >= 0; i-- {
		h := history[i]
		path, compare, ok := imageInspectionArgs(h)
		if !ok {
			continue
		}
		key := filepath.Clean(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		summary := imageEvidenceSummary(h, path, compare)
		if b.Len()+len(summary) > visualEvidenceMaxBytes-omissionRoom {
			omitted++
			continue
		}
		b.WriteString(summary)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "[%d older capture summaries omitted by byte limit]\n", omitted)
	}
	return b.String()
}
