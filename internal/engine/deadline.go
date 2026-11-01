package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DeadlineSwitch = "EXCALIBUR_DEADLINE"

func DeadlineEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(DeadlineSwitch))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

var instructionDeadline = regexp.MustCompile(`(?i)\byou\s+have\s+([0-9]+(?:\.[0-9]+)?)\s+(seconds?|minutes?|hours?)\b`)

// ResolveDeadline preserves the external timeout, reserving closing time inside it.
func ResolveDeadline(explicit int64, instruction string, fallback int64, source string) (int64, string) {
	if !DeadlineEnabled() {
		return fallback, source
	}
	if explicit > 0 {
		return explicit, "explicit"
	}
	for _, key := range HarborTimeoutEnvs {
		f, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
		if err == nil && f >= 1 && f < float64(math.MaxInt64/int64(time.Second)) {
			return int64(f), key
		}
	}
	matches := instructionDeadline.FindAllStringSubmatch(instruction, -1)
	var shortest int64
	for _, m := range matches {
		f, _ := strconv.ParseFloat(m[1], 64)
		switch strings.ToLower(m[2])[0] {
		case 'm':
			f *= 60
		case 'h':
			f *= 3600
		}
		if f >= 1 && f < float64(math.MaxInt64/int64(time.Second)) && (shortest == 0 || int64(f) < shortest) {
			shortest = int64(f)
		}
	}
	if shortest > 0 {
		return shortest, "instruction"
	}
	return fallback, source
}

// DeadlinePacing emits each threshold once, even if a slow action crossed several.
func (r *Run) DeadlinePacing(now time.Time) {
	if !DeadlineEnabled() {
		return
	}
	r.mu.Lock()
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	elapsed := now.Sub(r.StartedAt)
	var notes []string
	if total > 0 {
		for i, percent := range []int{50, 75, 90} {
			if elapsed >= total*time.Duration(percent)/100 && r.deadlinePaced&(1<<i) == 0 {
				r.deadlinePaced |= 1 << i
				instruction := "Check progress and prioritize the remaining acceptance checks."
				if percent == 75 {
					instruction = "Stop exploring; converge on a working deliverable and verify it."
				}
				if percent == 90 {
					instruction = "Leave the best deliverable in place and call finish now. Do not start long commands."
				}
				notes = append(notes, fmt.Sprintf("Task deadline %d%%: %s. %s", percent, FormatRemaining(total-elapsed), instruction))
			}
		}
	}
	r.mu.Unlock()
	for _, note := range notes {
		r.InjectNote(note)
	}
}

// deadlineCommand bounds every new terminal action in closing mode. The short
// context also stops commands whose runtime cannot be inferred from shell text.
func (r *Run) deadlineCommand(now time.Time, name string, args json.RawMessage) (time.Duration, string) {
	if name == "call_tool" {
		var wrapped struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(args, &wrapped) == nil && (wrapped.Name == "exec" || wrapped.Name == "exec_io") {
			name, args = wrapped.Name, wrapped.Arguments
		}
	}
	if !DeadlineEnabled() || (name != "exec" && name != "exec_io") {
		return 0, ""
	}
	r.mu.Lock()
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	elapsed := now.Sub(r.StartedAt)
	r.mu.Unlock()
	if total <= 0 || elapsed < total*9/10 {
		return 0, ""
	}
	cap := 5 * time.Second
	if left := total - elapsed; left < cap {
		cap = left
	}
	if cap <= 0 {
		return time.Nanosecond, "Task deadline reached; leave the deliverable in place and call finish."
	}
	var obj map[string]any
	_ = json.Unmarshal(args, &obj)
	if name == "exec_io" {
		action, _ := obj["action"].(string)
		switch action {
		case "write", "send", "keys":
			return cap, "Deadline closing mode forbids starting commands through an interactive session; call finish."
		}
	}
	for _, key := range []string{"timeout_s", "timeout_secs", "timeout", "time_limit", "duration", "timeout_ms", "yield_ms"} {
		if n, ok := obj[key].(float64); ok {
			if key == "timeout_ms" || key == "yield_ms" {
				n /= 1000
			}
			if n > cap.Seconds() {
				return cap, "Deadline closing mode forbids long commands; use a check of at most 5 seconds and call finish."
			}
		}
	}
	if obj["background"] == true {
		return cap, "Deadline closing mode forbids starting background commands; call finish."
	}
	return cap, ""
}
