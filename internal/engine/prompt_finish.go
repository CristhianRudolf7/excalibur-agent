package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

const PromptFinishNote = "Deterministic artifact checks pass; finishing still requires evidence that the requested end state is reached."

const endStateFinishNoteSwitch = "EXCALIBUR_END_STATE_FINISH_NOTE"

// A deterministic artifact check cannot interpret screen contents. Defer the
// positive note until an independent PASS covers the current visual evidence.
// This conservative rule avoids request-specific visual keyword heuristics.
func visualEvidenceVersion(history []tasks.HistoryTool) ([32]byte, bool) {
	h := sha256.New()
	found := false
	for _, item := range history {
		if item.IsError {
			continue
		}
		if item.Name == "inspect_image" || strings.Contains(item.Output, "screen_text:") ||
			(item.Name == "call_tool" && strings.Contains(item.Args, `"inspect_image"`)) {
			raw, _ := json.Marshal(item)
			_, _ = h.Write(raw)
			found = true
		}
	}
	var version [32]byte
	copy(version[:], h.Sum(nil))
	return version, found
}

type finishVerifierReport struct {
	request          string
	explicit, failed bool
	visualVersion    [32]byte
}

func (r *Run) recordFinishVerifierReport(history []tasks.HistoryTool, explicit, failed bool) {
	version, _ := visualEvidenceVersion(history)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishVerifierReport = finishVerifierReport{r.Request, explicit, failed, version}
}

// Callers hold run.mu. Retiring a stale note also preserves its journal trail.
func (r *Run) retirePromptFinishNoteLocked() {
	for _, note := range r.Volatile {
		if strings.HasPrefix(note, PromptFinishNote) {
			r.controlNoteLocked(controlNoteKey(note), "")
			return
		}
	}
}

// PromptFinishEnabled gates advisory stopping guidance; it never bypasses finish.
func PromptFinishEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_PROMPT_FINISH"))) {
	case "off", "0", "false":
		return false
	default:
		return true
	}
}

type promptFinishRecord struct {
	Enabled              bool       `json:"enabled"`
	FirstAllChecksPassAt *time.Time `json:"first_all_checks_pass_at,omitempty"`
	RepeatedVerification int        `json:"repeated_verification"`
}

// promptFinishCheck observes existing evidence without extra model/tool calls.
// It runs before context compilation so the very next turn sees the note.
func (e *Engine) promptFinishCheck(run *Run) {
	if !PromptFinishEnabled() || !e.explicitFinishRequired(run) {
		return
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	history := historyTools(run.History)
	guarded := switchOn(endStateFinishNoteSwitch)
	if guarded {
		visualVersion, visual := visualEvidenceVersion(history)
		report := run.finishVerifierReport
		reviewed := report.request == run.Request && report.explicit
		if (reviewed && report.failed) || (visual && (!reviewed || report.failed || report.visualVersion != visualVersion)) {
			run.retirePromptFinishNoteLocked()
			return
		}
	}
	if len(history) == run.promptFinishSeen {
		return
	}
	run.promptFinishSeen = len(history)
	cfg := e.selfVerifyConfig()
	drafts := run.drafts
	if len(drafts) == 0 {
		drafts = tasks.ExtractDraftCriteria(run.Request, cfg.MaxDrafts)
	}
	if len(drafts) == 0 || len(history) == 0 {
		return
	}
	cfg.Enable, cfg.EnableVerification, cfg.EnableContrast = true, true, true
	cfg.EnableStrong, cfg.EnableStrongExercise = true, true
	rep, _ := tasks.ApplySelfVerify(cfg, tasks.Evaluate(run.GateInput), run.GateInput, tasks.SelfVerifyRequest{
		Request: run.Request, Drafts: drafts, History: history, Steps: run.Steps,
	})
	// Artifact checks consult the run's withdrawal state under its own lock.
	// Release the snapshot lock before entering that shared gate.
	request, workdir := run.Request, run.Workdir
	run.mu.Unlock()
	rep = e.applyArtifacts(run, rep, history, request, "", workdir)
	run.mu.Lock()
	if !rep.OK {
		if guarded {
			run.retirePromptFinishNoteLocked()
		}
		return
	}
	// Require successful evidence for each criterion, not just a global exit=0.
	for _, d := range drafts {
		found := false
		for _, h := range history[tasks.LastArtifactChange(history)+1:] {
			if h.IsError || (h.Name != "exec" && h.Name != "read_file" && h.Name != "call_tool") {
				continue
			}
			if d.Key != "" && strings.Contains(strings.ToLower(h.Args+"\n"+h.Output), strings.ToLower(d.Key)) {
				found = true
			}
			if d.Key == "" && d.Check != "" && strings.Contains(h.Args, d.Check) {
				found = true
			}
		}
		if !found {
			return
		}
	}
	if run.firstAllChecksPassAt.IsZero() {
		run.firstAllChecksPassAt = time.Now().UTC()
	}
	note := PromptFinishNote
	last := history[len(history)-1]
	signature := finishCheckSignature(last)
	var artifactVersion strings.Builder
	for _, d := range drafts {
		path := d.Key
		if !filepath.IsAbs(path) {
			path = filepath.Join(run.Workdir, path)
		}
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(&artifactVersion, "%s:%d:%d;", path, info.Size(), info.ModTime().UnixNano())
		}
	}
	version := fmt.Sprintf("%d:%s", tasks.LastArtifactChange(history), artifactVersion.String())
	if version != run.promptFinishArtifactVersion {
		run.promptFinishSignatures = map[[32]byte]bool{}
		// On the first passing milestone, include earlier checks for the other
		// criteria so alternating re-verification is recognized immediately.
		if run.promptFinishArtifactVersion == "" {
			for _, h := range history[tasks.LastArtifactChange(history)+1 : len(history)-1] {
				if !h.IsError && (h.Name == "exec" || h.Name == "read_file" || h.Name == "call_tool") {
					run.promptFinishSignatures[finishCheckSignature(h)] = true
				}
			}
		}
		run.promptFinishArtifactVersion = version
	}
	if run.promptFinishSignatures == nil {
		run.promptFinishSignatures = map[[32]byte]bool{}
	}
	if run.promptFinishSignatures[signature] {
		run.repeatedVerification++
		note += " Repeated verification returned unchanged evidence; avoid re-running the same check unless artifacts changed."
	}
	run.promptFinishSignatures[signature] = true
	run.writePromptFinishRecordLocked()
	if guarded {
		state := fmt.Sprintf("%s:%s:%v", run.Request, version, drafts)
		if run.promptFinishStates[state] {
			return
		}
		if run.promptFinishStates == nil {
			run.promptFinishStates = map[string]bool{}
		}
		run.promptFinishStates[state] = true
	}
	run.controlNoteLocked(controlNoteKey(note), note)
}

func (r *Run) writePromptFinishRecordLocked() {
	rec := promptFinishRecord{Enabled: PromptFinishEnabled(), RepeatedVerification: r.repeatedVerification}
	if !r.firstAllChecksPassAt.IsZero() {
		t := r.firstAllChecksPassAt
		rec.FirstAllChecksPassAt = &t
	}
	// The driver installs a separate artifact directory; never write audit data to
	// the user's project workdir.
	if r.promptFinishRecordDir == "" {
		return
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err == nil {
		f, err := os.CreateTemp(r.promptFinishRecordDir, ".prompt-finish-*")
		if err != nil {
			return
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr == nil && closeErr == nil {
			_ = os.Rename(f.Name(), filepath.Join(r.promptFinishRecordDir, "prompt_finish.json"))
		}
	}
}

// SetPromptFinishRecordDir installs the run's audit destination before Start.
func (r *Run) SetPromptFinishRecordDir(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promptFinishRecordDir = dir
	r.writePromptFinishRecordLocked()
}

// Terminal session IDs and durations vary even when a check returns the same
// evidence. Keep the exit status and output, excluding only that known envelope.
func finishCheckOutput(output string) string {
	header, body, ok := strings.Cut(output, "\n")
	if ok && strings.HasPrefix(header, "exit: ") {
		if status, _, found := strings.Cut(header, "  duration: "); found {
			return status + "\n" + body
		}
	}
	return output
}

func finishCheckSignature(h tasks.HistoryTool) [32]byte {
	args := h.Args
	var parsed any
	if json.Unmarshal([]byte(args), &parsed) == nil {
		if raw, err := json.Marshal(parsed); err == nil {
			args = string(raw)
		}
	}
	return sha256.Sum256([]byte(h.Name + "\n" + args + "\n" + finishCheckOutput(h.Output)))
}
