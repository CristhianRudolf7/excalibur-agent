package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRerunHintsUsePlainBoundedCommands(t *testing.T) {
	for _, command := range []string{
		"echo 'quoted value'",
		"echo first\necho second",
		"echo " + strings.Repeat("x", 200),
		"echo " + strings.Repeat("界", 200) + "\necho second",
	} {
		t.Run(command[:min(len(command), 20)], func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"command": command})
			first, _, multi := strings.Cut(command, "\n")
			runes := []rune(first)
			expected := first
			if len(runes) > 160 {
				expected = string(runes[:160]) + "…"
			} else if multi {
				expected += "…"
			}
			history := []HistoryTool{{Name: "exec", Args: string(raw), Output: "exit: 0"}}
			if got := suggestChecks(history); got != "re-run "+expected {
				t.Fatalf("suggestion = %q, want plain command %q", got, expected)
			}
			if got := strongHint(nil, nil, history); got != "exercise the deliverable (re-run "+expected+")" {
				t.Fatalf("strong hint = %q", got)
			}
		})
	}
	t.Setenv("EXCALIBUR_PLAIN_CHECK_HINTS", "0")
	if got := rerunCommandHint(`{"command":"echo ok"}`); got != `{"command":"echo ok"}` {
		t.Fatalf("switch did not restore legacy JSON hint: %s", got)
	}
}
