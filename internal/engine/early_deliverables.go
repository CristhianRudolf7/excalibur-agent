package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"mvdan.cc/sh/v3/syntax"
)

const earlyDeliverablesSwitch = "EXCALIBUR_EARLY_DELIVERABLES"
const earlyDeliverablesGuidance = "For required outputs from long computation, first write valid outputs with a fast configuration, then replace them with better results. Never stop the only running producer of a missing deliverable unless a valid output already exists."
const earlyDeliverablesNote = "required outputs are still missing: write valid outputs now from the best available result, then improve them"

// Delivery pacing belongs to autonomous exec/eval runs. Interactive assistant
// turns retain their existing guidance and terminal behavior.
func (r *Run) earlyDeliverablesEnabled() bool {
	if !switchOn(earlyDeliverablesSwitch) {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Profile.ExplicitFinish
}

func (e *Engine) missingEarlyDeliverables(run *Run) []deliverable {
	run.mu.Lock()
	request := run.Request
	run.mu.Unlock()
	var missing []deliverable
	for _, file := range e.deliverables(run, request, workHistory(run)) {
		// An empty placeholder does not yet give a producer a result to preserve.
		info, err := os.Stat(file.Path)
		if os.IsNotExist(err) || (err == nil && info.Mode().IsRegular() && info.Size() == 0) {
			missing = append(missing, file)
		}
	}
	return missing
}

func (e *Engine) earlyDeliverablesCheck(run *Run, now time.Time) {
	if !run.earlyDeliverablesEnabled() {
		return
	}
	run.mu.Lock()
	total := time.Duration(run.Tracker.Limit.MaxSeconds) * time.Second
	// Require positive, explicitly configured wall time. An estimate must never
	// become a reason to rush delivery, even when other time switches are off.
	due := !run.earlyDeliverablesNoted && run.Profile.WallSource != "default" && total > 0 &&
		!run.StartedAt.IsZero() && now.Sub(run.StartedAt) >= total/2
	run.mu.Unlock()
	if !due || len(e.missingEarlyDeliverables(run)) == 0 {
		return
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if !run.earlyDeliverablesNoted {
		run.earlyDeliverablesNoted = true
		run.controlNoteLocked("budget.early_deliverables", earlyDeliverablesNote)
	}
}

func (e *Engine) earlyLiveSessions(run *Run) []tools.SessionInfo {
	history := e.currentSessionHistory(workHistory(run))
	live := map[string]bool{}
	for _, id := range tasks.LiveSessions(history) {
		live[id] = true
	}
	if e.deps.ActiveSessions != nil {
		var out []tools.SessionInfo
		for _, session := range e.deps.ActiveSessions() {
			if session.Alive && live[string(session.ID)] {
				out = append(out, session)
			}
		}
		return out
	}
	var out []tools.SessionInfo
	for id := range live {
		out = append(out, tools.SessionInfo{ID: tools.SessionID(id), Alive: true, Command: sessionStartCommand(history, id)})
	}
	return out
}

// literalShellWord recognizes quoted static arguments without expanding shell
// variables, substitutions or globs. In particular pkill -f 'python worker.py'
// must keep its complete pattern rather than becoming an empty argument.
func literalShellWord(word *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			text, ok := literalShellWord(&syntax.Word{Parts: p.Parts})
			if !ok {
				return "", false
			}
			b.WriteString(text)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func earlyShellCalls(command string) [][]string {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}
	var calls [][]string
	syntax.Walk(file, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if !ok {
			return true
		}
		var words []string
		for _, arg := range call.Args {
			text, _ := literalShellWord(arg)
			words = append(words, text)
		}
		if len(words) > 0 {
			calls = append(calls, words)
		}
		return true
	})
	return calls
}

func stoppingSignal(signal string) bool {
	switch strings.ToUpper(strings.TrimPrefix(strings.ToUpper(signal), "SIG")) {
	case "", "INTERRUPT", "TERMINATE", "INT", "TERM", "KILL", "HUP", "QUIT", "STOP", "TSTP", "1", "2", "3", "9", "15", "19", "20":
		return true
	}
	return false
}

func stopTargetsSession(words []string, session tools.SessionInfo) bool {
	if len(words) == 0 {
		return false
	}
	name := filepath.Base(words[0])
	// Accept ordinary shell wrappers without treating text such as echo 'kill'
	// as an executable stop request.
	if name == "sudo" || name == "env" || name == "command" {
		words = words[1:]
		for len(words) > 0 && (strings.HasPrefix(words[0], "-") || (name == "env" && strings.Contains(words[0], "="))) {
			option := words[0]
			words = words[1:]
			if len(words) > 0 && ((name == "sudo" && (option == "-u" || option == "-g" || option == "--user" || option == "--group")) ||
				(name == "env" && (option == "-u" || option == "--unset" || option == "-C" || option == "--chdir"))) {
				words = words[1:]
			}
		}
		return stopTargetsSession(words, session)
	}
	if name != "kill" && name != "pkill" && name != "killall" {
		return false
	}
	signal := "TERM"
	fullCommand := false
	var targets []string
	for i := 1; i < len(words); i++ {
		word := words[i]
		if name == "pkill" && (word == "--full" || (strings.HasPrefix(word, "-") && !strings.HasPrefix(word, "--") && strings.Contains(word, "f"))) {
			fullCommand = true
			continue
		}
		if word == "-l" || word == "-L" || word == "--list" || word == "--table" {
			return false
		}
		if word == "-s" || word == "--signal" || (name == "kill" && word == "-n") {
			i++
			if i >= len(words) {
				return false
			}
			signal = words[i]
			continue
		}
		if strings.HasPrefix(word, "--signal=") {
			signal = strings.TrimPrefix(word, "--signal=")
			continue
		}
		if word == "--" {
			targets = append(targets, words[i+1:]...)
			break
		}
		if strings.HasPrefix(word, "-") {
			if name == "kill" && i > 1 && session.PID > 0 && word == "-"+strconv.Itoa(session.PID) {
				targets = append(targets, word)
			} else if name == "kill" || stoppingSignal(strings.TrimPrefix(word, "-")) || word == "-0" || (len(word) > 1 && word[1:] == strings.ToUpper(word[1:])) {
				signal = strings.TrimPrefix(word, "-")
			}
			continue
		}
		targets = append(targets, word)
	}
	if !stoppingSignal(signal) {
		return false
	}
	for _, target := range targets {
		if name == "kill" {
			if session.PID > 0 && (target == strconv.Itoa(session.PID) || target == "-"+strconv.Itoa(session.PID)) {
				return true
			}
			continue
		}
		if target == "" {
			continue
		}
		if name == "pkill" {
			subject := session.Command
			if !fullCommand {
				fields := strings.Fields(subject)
				if len(fields) == 0 {
					continue
				}
				subject = filepath.Base(fields[0])
			}
			if pattern, err := regexp.Compile(target); err == nil && pattern.MatchString(subject) {
				return true
			}
		} else if mentionsFile(session.Command, filepath.Base(target)) {
			return true
		}
	}
	return false
}

func stoppingSession(name string, raw json.RawMessage, session tools.SessionInfo) bool {
	var args struct {
		Action  string   `json:"action"`
		Session string   `json:"session"`
		Signal  string   `json:"signal"`
		Text    string   `json:"text"`
		Keys    []string `json:"keys"`
		Command string   `json:"command"`
	}
	if json.Unmarshal(raw, &args) != nil {
		return false
	}
	if name == "exec_io" && args.Session == string(session.ID) {
		switch args.Action {
		case "close":
			return true
		case "signal":
			return stoppingSignal(args.Signal)
		case "write":
			return strings.ContainsAny(args.Text, "\x03\x1c\x1a")
		case "keys":
			for _, key := range args.Keys {
				switch strings.ToLower(key) {
				case "ctrl-c", "ctrl_c", "intr", "ctrl-z":
					return true
				}
			}
		}
	}
	if name == "exec" {
		for _, words := range earlyShellCalls(args.Command) {
			if stopTargetsSession(words, session) {
				return true
			}
		}
	}
	return false
}

func earlyProducer(command, workdir, request string, missing []deliverable) bool {
	for _, output := range missing {
		if mentionsFile(command, output.Base) {
			return true
		}
	}
	for _, words := range earlyShellCalls(command) {
		for _, word := range words {
			if _, ok := programRunners[strings.ToLower(filepath.Ext(word))]; !ok && !isProgramPath(resolveFinishPath(workdir, word)) {
				continue
			}
			for _, output := range missing {
				for _, sentence := range requestSentences(request) {
					if mentionsFile(sentence, filepath.Base(word)) && mentionsFile(sentence, output.Base) {
						return true
					}
				}
			}
			path := resolveFinishPath(workdir, word)
			info, err := os.Stat(path)
			// Never open a FIFO or device while producing advisory text.
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			script, err := os.Open(path)
			if err != nil {
				continue
			}
			info, err = script.Stat()
			var content []byte
			if err == nil && info.Mode().IsRegular() {
				content, _ = io.ReadAll(io.LimitReader(script, 256*1024))
			}
			script.Close()
			for _, output := range missing {
				if mentionsFile(string(content), output.Base) {
					return true
				}
			}
		}
	}
	return false
}

// Capture live facts before execution: a successful signal may remove the
// target before the result is annotated. This warning never refuses a call.
func (e *Engine) earlyProducerStopWarning(run *Run, call providers.ToolCall) string {
	if !run.earlyDeliverablesEnabled() {
		return ""
	}
	name, raw := unwrapCallTool(call.Name, call.Arguments)
	if name != "exec" && name != "exec_io" {
		return ""
	}
	sessions := e.earlyLiveSessions(run)
	var targets []tools.SessionInfo
	for _, session := range sessions {
		if stoppingSession(name, raw, session) {
			targets = append(targets, session)
		}
	}
	if len(targets) == 0 {
		return ""
	}
	missing := e.missingEarlyDeliverables(run)
	if len(missing) == 0 {
		return ""
	}
	run.mu.Lock()
	request, workdir := run.Request, run.Workdir
	run.mu.Unlock()
	for _, session := range targets {
		if earlyProducer(session.Command, workdir, request, missing) {
			return fmt.Sprintf("warning: stopping session %s may end a live producer of still-missing required outputs; preserve valid outputs from the best available result before stopping the only producer.", session.ID)
		}
	}
	return ""
}
