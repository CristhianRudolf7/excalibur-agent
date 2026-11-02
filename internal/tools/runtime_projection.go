package tools

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/envsnap"
)

// RuntimeProjectionSwitch gates autonomous long-command advice, default on.
const RuntimeProjectionSwitch = "EXCALIBUR_RUNTIME_PROJECTION"

type runtimeBudgetKey struct{}
type runtimeBudget struct {
	start time.Time
	total time.Duration
}

// WithRuntimeBudget enables projection for an autonomous exec/eval tool call.
// A zero total means unknown; callers must not pass an estimated wall budget.
// Tool wait deadlines are deliberately independent of this run-level budget.
func WithRuntimeBudget(ctx context.Context, start time.Time, total time.Duration) context.Context {
	return context.WithValue(ctx, runtimeBudgetKey{}, runtimeBudget{start, total})
}

const projectionScanBytes = 8192

var progressSignal = regexp.MustCompile(`(?i)(?:\b(?:epoch|iter(?:ation)?)\s+)?\b([0-9]{1,12}(?:\.[0-9]{1,6})?)\s*(%|/\s*([0-9]{1,12})\b|\s+of\s+([0-9]{1,12})\b)`)
var progressBar = regexp.MustCompile(`\[([#=> ._\-█░]{8,80})\]`)
var progressANSI = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

// Latest syntactic signal wins, including zero/invalid signals: never reuse an
// earlier completed phase after a new phase resets progress. Only a bounded
// output tail is inspected, so arbitrary output cannot grow parsing work.
func progressFraction(output string) float64 {
	if len(output) > projectionScanBytes {
		output = output[len(output)-projectionScanBytes:]
		// Do not interpret a number sliced in half at the scan boundary.
		if i := strings.IndexAny(output, "\r\n"); i >= 0 {
			output = output[i+1:]
		} else {
			return 0
		}
	}
	output = progressANSI.ReplaceAllString(output, "")
	last, fraction := -1, float64(0)
	for _, match := range progressSignal.FindAllStringSubmatchIndex(output, -1) {
		start, end := match[0], match[1]
		// Avoid paths, dates, negative numbers and denominator suffixes.
		if start > 0 && strings.ContainsRune("/.-", rune(output[start-1])) ||
			end < len(output) && strings.ContainsRune("/0123456789", rune(output[end])) {
			continue
		}
		n, _ := strconv.ParseFloat(output[match[2]:match[3]], 64)
		d := float64(100)
		for _, group := range []int{6, 8} {
			if match[group] >= 0 {
				d, _ = strconv.ParseFloat(output[match[group]:match[group+1]], 64)
			}
		}
		last, fraction = start, 0
		if d > 0 && n > 0 && n <= d {
			fraction = n / d
		}
	}
	for _, match := range progressBar.FindAllStringSubmatchIndex(output, -1) {
		if match[0] < last {
			continue
		}
		filled, empty := 0, 0
		for _, c := range output[match[2]:match[3]] {
			if strings.ContainsRune("#=>█", c) {
				filled++
			} else {
				empty++
			}
		}
		last, fraction = match[0], 0
		if filled > 0 && empty > 0 {
			fraction = float64(filled) / float64(filled+empty)
		}
	}
	return fraction
}

func projectionSpan(seconds float64) string {
	// Avoid overflowing a duration on tiny progress fractions or long uptimes.
	if seconds > float64((365*24*time.Hour)/time.Second) {
		return ">365d"
	}
	return (time.Duration(math.Ceil(max(0, seconds))) * time.Second).String()
}

func (m *SessionManager) runtimeProjection(ctx context.Context, s *Session) string {
	budget, autonomous := ctx.Value(runtimeBudgetKey{}).(runtimeBudget)
	if !autonomous || !switchDefaultOn(RuntimeProjectionSwitch) {
		return ""
	}
	now := m.now()
	s.mu.Lock()
	elapsed := now.Sub(s.Started)
	if s.exited || elapsed <= max(3*time.Minute, budget.total/10) {
		s.mu.Unlock()
		return ""
	}
	data := s.full.Bytes()
	// Include one extra byte so the parser knows when the tail was clipped.
	output := string(data[max(0, len(data)-projectionScanBytes-1):])
	s.mu.Unlock()
	remaining, remainingLabel := time.Duration(0), "unknown"
	if budget.total > 0 && !budget.start.IsZero() {
		remaining = max(0, budget.total-now.Sub(budget.start))
		remainingLabel = projectionSpan(remaining.Seconds())
	}
	limits := m.resourceReader().Limits()
	line := fmt.Sprintf("runtime: elapsed %s; remaining budget %s; effective CPUs %.2g; memory %s; ",
		projectionSpan(elapsed.Seconds()), remainingLabel, limits.CPUs, envsnap.MemoryLabel(limits.MemoryBytes))
	fraction := progressFraction(output)
	if fraction <= 0 {
		return line + "no progress fraction; time a small sample before continuing.\n"
	}
	left := elapsed.Seconds() * (1/fraction - 1)
	line += fmt.Sprintf("linear estimate %.1f%%: finish in %s", fraction*100, projectionSpan(left))
	if remainingLabel != "unknown" && left > remaining.Seconds() {
		line += "; projected to finish after the time limit"
	}
	return line + ".\n"
}
