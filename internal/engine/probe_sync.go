package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"mvdan.cc/sh/v3/syntax"
)

const probeSyncSwitch = "EXCALIBUR_PROBE_SYNC"

// All observations are bounded and read-only. Unknown/dynamic paths and large
// files are omitted rather than guessed. No tool, shell or model is invoked.
const trialFileLimit = 256
const trialByteLimit = 4 << 20
const trialSingleFileLimit = 1 << 20

type trialFiles map[string]string // literal path -> complete content (bounded)
type probeCopy struct {
	deliverable, copy, original string
	improved                    bool
	noted, reserveNoted         bool
}
type probeSyncState struct {
	request string
	copies  map[string]*probeCopy
	checks  map[string]checkMeasurement
}
type trialObservation struct {
	required trialFiles
	files    trialFiles
}

// Autonomous exec/eval pacing must not change interactive assistant behavior.
func (r *Run) trialAdviceEnabled(key string) bool {
	if !switchOn(key) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Profile.ExplicitFinish
}

func (e *Engine) captureTrialObservation(r *Run, calls []providers.ToolCall) trialObservation {
	if !r.trialAdviceEnabled(probeSyncSwitch) {
		return trialObservation{}
	}
	r.mu.Lock()
	request, dir := r.Request, r.Workdir
	r.mu.Unlock()
	var paths []string
	for _, d := range e.deliverables(r, request, nil) {
		paths = append(paths, d.Path)
	}
	required := readTrialFiles(paths)
	paths = nil
	for _, tc := range calls {
		paths = append(paths, trialOperands(tc, dir)...)
	}
	return trialObservation{required: required, files: readTrialFiles(paths)}
}

func literalTrialWord(w *syntax.Word) string {
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, sub := range p.Parts {
				lit, ok := sub.(*syntax.Lit)
				if !ok {
					return ""
				}
				b.WriteString(lit.Value)
			}
		default:
			return ""
		}
	}
	return b.String()
}

// Include literal shell operands, redirections and paths in inline scripts.
// Directory operands allow cp/rsync to a directory and checks using `cd`.
func trialOperands(tc providers.ToolCall, workdir string) []string {
	var args map[string]any
	_ = json.Unmarshal(tc.Arguments, &args)
	if dir, ok := args["workdir"].(string); ok && dir != "" {
		workdir = resolveFinishPath(workdir, dir)
	}
	var paths []string
	add := func(p string) {
		if p != "" && !strings.HasPrefix(p, "-") {
			paths = append(paths, resolveFinishPath(workdir, p))
		}
	}
	for _, key := range []string{"path", "file_path", "file", "filename", "destination"} {
		p, _ := args[key].(string)
		add(p)
	}
	command, _ := args["command"].(string)
	if f, err := syntax.NewParser().Parse(strings.NewReader(command), ""); err == nil {
		syntax.Walk(f, func(n syntax.Node) bool {
			if w, ok := n.(*syntax.Word); ok {
				text := literalTrialWord(w)
				add(text)
				for _, p := range fileOperandRe.FindAllString(text, trialFileLimit) {
					add(p)
				}
				return false
			}
			return true
		})
	}
	return paths
}

func readTrialFiles(paths []string) trialFiles {
	files := trialFiles{}
	remaining := int64(trialByteLimit)
	entries := 0
	for _, root := range paths {
		if entries >= trialFileLimit || remaining <= 0 {
			break
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			entries++
			if entries > trialFileLimit || remaining <= 0 {
				return fs.SkipAll
			}
			if _, exists := files[path]; exists {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > trialSingleFileLimit || info.Size() > remaining {
				return nil
			}
			// WalkDir never follows symlinks; devices and pipes are never opened.
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			body, err := io.ReadAll(io.LimitReader(f, min(remaining, trialSingleFileLimit)+1))
			_ = f.Close()
			remaining -= int64(len(body))
			if err == nil && int64(len(body)) == info.Size() {
				files[path] = string(body)
			}
			return nil
		})
	}
	return files
}

func trialDigest(body string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(body))) }

// A check is an executable observation, including failed diagnostics, rather
// than an existence/read probe or an editing-only command. The existing finish
// evidence classifier supplies this distinction without any new command list.
func trialCheck(tc providers.ToolCall, res tools.Result) (string, bool) {
	if tc.Name != "exec" || (res.Status != tools.StatusOK && res.Status != tools.StatusToolError) {
		return "", false
	}
	h := tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: res.Output}
	if tasks.HistoryRunning(h) {
		return "", false
	}
	command := execCommand(h.Args)
	if command == "" || isProbeCommand(command) || !tasks.ExercisesDeliverable(command) {
		return "", false
	}
	if tasks.LastArtifactChange([]tasks.HistoryTool{h}) >= 0 && !tasks.EntryVerifiesChanges(h) {
		return "", false
	}
	return tasks.NormalizeCallArgs(string(tc.Arguments)), true
}

// Explicit diagnostic totals compare across changing output/timing text. Do not
// infer an improvement from arbitrary numbers such as durations or line numbers.
var checkCountBefore = regexp.MustCompile(`(?i)\b([0-9]+)\s+(warnings?|errors?|failures?|failed)\b`)
var checkCountAfter = regexp.MustCompile(`(?i)\b(warnings?|errors?|failures?|failed)\s*[:=]\s*([0-9]+)\b`)

type checkMeasurement struct {
	counts      map[string]int
	passed      bool
	fingerprint string
}

func measureTrialCheck(tc providers.ToolCall, res tools.Result) checkMeasurement {
	counts := map[string]int{}
	add := func(label, value string) {
		label = strings.TrimSuffix(strings.ToLower(label), "s")
		if label == "failed" {
			label = "failure"
		}
		n, err := strconv.Atoi(value)
		if err == nil {
			counts[label] = n
		}
	}
	for _, m := range checkCountBefore.FindAllStringSubmatch(res.Output, -1) {
		add(m[2], m[1])
	}
	for _, m := range checkCountAfter.FindAllStringSubmatch(res.Output, -1) {
		add(m[1], m[2])
	}
	code, known := exitCodeOfOutput(res.Output)
	failed, _ := execFailed(tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: res.Output})
	passed := res.Status == tools.StatusOK && known && code == 0 && !failed
	for _, n := range counts {
		if n > 0 {
			passed = false
		}
	}
	return checkMeasurement{counts: counts, passed: passed, fingerprint: tasks.ResultFingerprint(res.Output)}
}
func (m checkMeasurement) improves(old checkMeasurement) bool {
	if m.passed && !old.passed {
		return true
	}
	better := false
	if len(m.counts) == 0 || len(m.counts) != len(old.counts) {
		return false
	}
	for key, n := range m.counts {
		prev, ok := old.counts[key]
		if !ok || n > prev {
			return false
		}
		better = better || n < prev
	}
	return better
}

func (e *Engine) observeProbeCopies(r *Run, calls []providers.ToolCall, results []tools.Result, before trialObservation) {
	if before.required == nil || !r.trialAdviceEnabled(probeSyncSwitch) {
		return
	}
	r.mu.Lock()
	request, dir := r.Request, r.Workdir
	r.mu.Unlock()
	var paths []string
	for _, tc := range calls {
		paths = append(paths, trialOperands(tc, dir)...)
	}
	after := readTrialFiles(paths)
	r.mu.Lock()
	defer r.mu.Unlock()
	state := &r.probes
	if state.request != request || state.copies == nil {
		*state = probeSyncState{request: request, copies: map[string]*probeCopy{}, checks: map[string]checkMeasurement{}}
	}
	// Require an observed write of identical content. Reading a preexisting
	// duplicate must never be mistaken for creating a working copy.
	for _, tc := range calls {
		for dest, source := range trialCopyEdges(tc, dir, before.required) {
			content, exists := after[dest]
			if !exists {
				continue
			}
			if old, existed := before.files[dest]; existed && old == content {
				continue
			}
			key := source + "\x00" + dest
			if state.copies[key] == nil && len(state.copies) < trialFileLimit {
				state.copies[key] = &probeCopy{deliverable: source, copy: dest, original: trialDigest(before.required[source])}
			}
		}
	}
	for path, content := range after {
		if old, exists := before.files[path]; exists && old == content {
			continue
		}
		if _, required := before.required[path]; required {
			continue
		}
		for original, body := range before.required {
			if content != body || path == original {
				continue
			}
			key := original + "\x00" + path
			if state.copies[key] == nil && len(state.copies) < trialFileLimit {
				state.copies[key] = &probeCopy{deliverable: original, copy: path, original: trialDigest(body)}
			}
		}
	}
	for i, tc := range calls {
		command, check := trialCheck(tc, results[i])
		if !check {
			continue
		}
		measurement := measureTrialCheck(tc, results[i])
		old, seen := state.checks[command]
		for _, copy := range state.copies {
			// Only literal files actually used by this check qualify, not another
			// sibling with the same basename in an unrelated directory.
			operands := readTrialFiles(trialOperands(tc, dir))
			if content, used := operands[copy.copy]; used && trialDigest(content) != copy.original && (measurement.passed || (seen && measurement.improves(old))) {
				copy.improved = true
			}
		}
		if len(state.checks) < trialFileLimit || seen {
			state.checks[command] = measurement
		}
	}
}

func (e *Engine) probeSyncCheck(r *Run, now time.Time) {
	if !r.trialAdviceEnabled(probeSyncSwitch) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.probes.request != r.Request {
		return
	}
	var paths []string
	for _, copy := range r.probes.copies {
		paths = append(paths, copy.deliverable, copy.copy)
	}
	files := readTrialFiles(paths)
	total := time.Duration(r.Tracker.Limit.MaxSeconds) * time.Second
	explicit := r.Profile.WallSource != "default" && total > 0 && !r.StartedAt.IsZero()
	half := explicit && now.Sub(r.StartedAt) >= total/2
	reserve := explicit && now.Sub(r.StartedAt) >= total*70/100
	var keys []string
	for key := range r.probes.copies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		copy := r.probes.copies[key]
		original, exists := files[copy.deliverable]
		content, readable := files[copy.copy]
		noteKey := "budget.probe_sync." + trialDigest(copy.deliverable)
		if !exists || !readable || trialDigest(original) != copy.original || trialDigest(content) == copy.original {
			if copy.noted {
				r.controlNoteLocked(noteKey, "")
			}
			continue
		}
		first := !copy.noted && (copy.improved || half)
		repeat := copy.noted && !copy.reserveNoted && reserve
		if first || repeat {
			copy.noted = true
			copy.reserveNoted = reserve
			r.controlNoteLocked(noteKey, "you are editing a copy; the deliverable at "+copy.deliverable+" is unchanged — copy your best version back now")
		}
	}
}

func executedResults(results []tools.Result, indexes []int) []tools.Result {
	out := make([]tools.Result, len(indexes))
	for j, i := range indexes {
		out[j] = results[i]
	}
	return out
}

// A literal cp/rsync edge also survives copy-and-edit in one shell call, where
// the intermediate identical file cannot be sampled between commands.
func trialCopyEdges(tc providers.ToolCall, workdir string, required trialFiles) map[string]string {
	edges := map[string]string{}
	if tc.Name != "exec" {
		return edges
	}
	var args struct {
		Command string `json:"command"`
		Workdir string `json:"workdir"`
	}
	_ = json.Unmarshal(tc.Arguments, &args)
	if args.Workdir != "" {
		workdir = resolveFinishPath(workdir, args.Workdir)
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(args.Command), "")
	if err != nil {
		return edges
	}
	syntax.Walk(f, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok || len(call.Args) < 3 {
			return true
		}
		name := filepath.Base(literalTrialWord(call.Args[0]))
		if name != "cp" && name != "rsync" {
			return true
		}
		var operands []string
		for _, word := range call.Args[1:] {
			value := literalTrialWord(word)
			if value == "" {
				return true
			}
			if strings.HasPrefix(value, "-") {
				// Only option forms with no separate value are understood.
				if strings.HasPrefix(value, "--") && value != "--" && value != "--archive" && value != "--recursive" {
					return true
				}
				if value == "-t" || value == "-S" || value == "-T" {
					return true
				}
				continue
			}
			operands = append(operands, value)
		}
		if len(operands) != 2 {
			return true
		}
		source, dest := resolveFinishPath(workdir, operands[0]), resolveFinishPath(workdir, operands[1])
		for path := range required {
			target := ""
			if path == source {
				target = dest
				if info, err := os.Stat(dest); err == nil && info.IsDir() {
					target = filepath.Join(dest, filepath.Base(path))
				}
			} else if rel, err := filepath.Rel(source, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				// For an existing directory cp nests the source; rsync with a trailing
				// slash copies its contents. Only record a target actually present.
				target = filepath.Join(dest, rel)
				if name == "cp" {
					if _, err := os.Stat(filepath.Join(dest, filepath.Base(source), rel)); err == nil {
						target = filepath.Join(dest, filepath.Base(source), rel)
					}
				}
				if name == "rsync" && !strings.HasSuffix(operands[0], "/") {
					target = filepath.Join(dest, filepath.Base(source), rel)
				}
			}
			if target != "" && target != path {
				edges[target] = path
			}
		}
		return true
	})
	return edges
}
