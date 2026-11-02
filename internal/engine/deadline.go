package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// DeadlineSwitch gates the real-deadline plumbing: pacing against the
// wall budget, closing-mode bounds on new commands and the process
// context deadline. Default ON.
const DeadlineSwitch = "EXCALIBUR_DEADLINE"

// TimeFooterSwitch gates the one-line time footer that ends every tool
// result of a time-only run. Default ON.
const TimeFooterSwitch = "EXCALIBUR_TIME_FOOTER"

// DeadlineEnabled reports whether the real-deadline plumbing applies.
func DeadlineEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(DeadlineSwitch))) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// TimeFooterEnabled reports whether tool results carry the time footer.
func TimeFooterEnabled() bool { return envDefaultOn(TimeFooterSwitch) }

// pacingStep is one pacing threshold of the wall budget and the advice
// given once when it is reached.
type pacingStep struct {
	percent int
	advice  string
}

// pacingSteps are the thresholds at 50, 75 and 90 percent of the wall
// budget, in ascending order. Each is announced once per run.
var pacingSteps = []pacingStep{
	{50, "check that the deliverables exist."},
	{75, "stop exploring; verify and finish."},
	{90, "finish now with what works; do not start new work."},
}

// crossPacingLocked marks every pacing threshold that elapsed has reached
// and returns the newly reached indexes into pacingSteps, ascending.
// Callers hold r.mu.
func (r *Run) crossPacingLocked(elapsed, total time.Duration) []int {
	var out []int
	for i, step := range pacingSteps {
		bit := uint8(1) << i
		if elapsed >= total*time.Duration(step.percent)/100 && r.deadlinePaced&bit == 0 {
			r.deadlinePaced |= bit
			out = append(out, i)
		}
	}
	return out
}

// DeadlinePacing injects the pacing advice as volatile notes. It is the
// legacy channel: when the time footer is active the advice rides on the
// footer of the next tool result instead, so the agent sees one signal.
func (r *Run) DeadlinePacing(now time.Time) {
	if !DeadlineEnabled() || r.footerActive() {
		return
	}
	r.mu.Lock()
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	elapsed := now.Sub(r.StartedAt)
	var notes []string
	if total > 0 {
		for _, i := range r.crossPacingLocked(elapsed, total) {
			step := pacingSteps[i]
			if r.assumedWallLocked() {
				notes = append(notes, "Estimated time: the time limit is not known; check progress and wait for running builds and tests before finishing.")
			} else {
				notes = append(notes, fmt.Sprintf("Task deadline %d%%: %s. Next: %s", step.percent, FormatRemaining(total-elapsed), step.advice))
			}
		}
	}
	r.mu.Unlock()
	for _, note := range notes {
		r.InjectNote(note)
	}
}

// footerActive reports whether tool results of this run carry the time
// footer: a time-only run with a wall budget and the switch on.
func (r *Run) footerActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Profile.TimeBudgetOnly && r.Tracker.Limit.MaxSeconds > 0 && TimeFooterEnabled()
}

// timeFooter renders the time footer for a tool result observed at now:
// `[time: 4m10s used, about 10m50s left]`, followed by the pacing advice
// of the highest threshold this result crossed, once per threshold. It
// returns "" when the footer does not apply.
func (r *Run) timeFooter(now time.Time) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Profile.TimeBudgetOnly || r.Tracker.Limit.MaxSeconds <= 0 || !TimeFooterEnabled() {
		return ""
	}
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	elapsed := now.Sub(r.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	left := total - elapsed
	if left < 0 {
		left = 0
	}
	line := fmt.Sprintf("[time: %s used, about %s left]", clockSpan(elapsed), clockSpan(left))
	if r.assumedWallLocked() {
		line = fmt.Sprintf("[time estimate: %s used, assume about %s left; time limit unknown]", clockSpan(elapsed), clockSpan(left))
		r.crossPacingLocked(elapsed, total)
		return line
	}
	if crossed := r.crossPacingLocked(elapsed, total); len(crossed) > 0 {
		step := pacingSteps[crossed[len(crossed)-1]]
		line += fmt.Sprintf(" Pacing %d%%: %s", step.percent, step.advice)
	}
	return line
}

// clockSpan renders a duration in whole seconds, compactly: 45s, 4m10s,
// 1h05m. Durations round down so the line never promises more time than
// exists.
func clockSpan(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs >= 3600:
		return fmt.Sprintf("%dh%02dm", secs/3600, (secs%3600)/60)
	case secs >= 60:
		return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// withFooter appends the footer as the last line of a tool output. An
// empty output becomes the footer alone.
func withFooter(output, footer string) string {
	switch {
	case footer == "":
		return output
	case output == "":
		return footer
	case strings.HasSuffix(output, "\n"):
		return output + footer
	default:
		return output + "\n" + footer
	}
}

// deadlineCommand bounds every new terminal action in closing mode. The
// short context also stops commands whose runtime cannot be inferred
// from shell text.
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
	// Autonomous exec/eval pacing is advisory until the real deadline.
	// The process context owns the hard wall limit.
	if r.Profile.TimeBudgetOnly || r.assumedWallLocked() {
		r.mu.Unlock()
		return 0, ""
	}
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
