package engine

import "strings"

// Autonomous exec/eval runs must retain time for delivery. Interactive
// assistant verification keeps its existing contract.
func (e *Engine) boundedRefusals(run *Run) bool {
	return e.explicitFinishRequired(run) && switchOn("EXCALIBUR_FINISH_REFUSAL_CAP")
}

func boundedRefusalKey(item RefusalItem) string {
	if strings.Contains(item.Missing, "changes since the last check are unverified") {
		return "unverified-changes"
	}
	return item.ID
}

func (e *Engine) countBlockingReasons(run *Run, reasons []string) {
	if !e.boundedRefusals(run) {
		return
	}
	items := e.refusalItems(run, reasons)
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.fin.blockingCounts == nil {
		run.fin.blockingCounts = map[string]int{}
	}
	seen := map[string]bool{}
	for _, item := range items {
		key := boundedRefusalKey(item)
		if !seen[key] {
			run.fin.blockingCounts[key]++
			seen[key] = true
		}
	}
}

func (e *Engine) hardFinishReasons(run *Run, reasons []string) []string {
	if !e.boundedRefusals(run) {
		return reasons
	}
	var hard []string
	for _, reason := range reasons {
		items := e.refusalItems(run, []string{reason})
		blocks := false
		run.mu.Lock()
		for _, item := range items {
			if run.fin.blockingCounts[boundedRefusalKey(item)] < 2 {
				blocks = true
			}
		}
		if !blocks {
			found := false
			for _, old := range run.fin.finishAdvisory {
				found = found || old == reason
			}
			if !found {
				run.fin.finishAdvisory = append(run.fin.finishAdvisory, reason)
			}
		}
		run.mu.Unlock()
		if blocks {
			hard = append(hard, reason)
		}
	}
	return hard
}
