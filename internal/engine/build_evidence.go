package engine

import (
	"encoding/json"
	"mvdan.cc/sh/v3/syntax"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func buildEvidenceEnabled() bool { return switchOn("EXCALIBUR_BUILD_EVIDENCE") }

type compilerInvocation struct {
	scope          string
	outputs        []string
	trustedExit    bool
	project        bool
	explicitOutput bool
}

func commandAndDir(h tasks.HistoryTool, dir string) (string, string) {
	var a struct {
		Command string `json:"command"`
		Workdir string `json:"workdir"`
	}
	_ = json.Unmarshal([]byte(h.Args), &a)
	if a.Workdir != "" {
		if filepath.IsAbs(a.Workdir) {
			dir = a.Workdir
		} else {
			dir = filepath.Join(dir, a.Workdir)
		}
	}
	return a.Command, dir
}

// A successful shell exit certifies a compiler only in a direct command or
// success-dependent && chain. Semicolon/OR pipelines can mask compiler failure.
func compilerFor(h tasks.HistoryTool, dir string) compilerInvocation {
	command, dir := commandAndDir(h, dir)
	info := compilerInvocation{trustedExit: compilerExitUnmasked(command)}
	for _, segment := range tasks.SplitShellSegments(command) {
		words := tasks.ShellWords(segment)
		if len(words) == 0 {
			continue
		}
		if words[0] == "cd" && len(words) > 1 {
			if filepath.IsAbs(words[1]) {
				dir = words[1]
			} else {
				dir = filepath.Join(dir, words[1])
			}
			continue
		}
		kind := ""
		switch filepath.Base(words[0]) {
		case "cc", "gcc", "g++", "c++", "clang", "clang++":
			kind = "native"
		case "rustc":
			kind = "rust"
		case "go":
			if len(words) > 1 && words[1] == "build" {
				kind = "go"
				info.project = true
			}
		case "cargo":
			if len(words) > 1 && words[1] == "build" {
				kind = "rust"
				info.project = true
			}
		case "make", "cmake", "ninja":
			kind = "native"
			info.project = true
		}
		if kind == "" {
			continue
		}
		info.scope = sourceScopePrefix + kind + ":" + filepath.Clean(dir)
		for i, w := range words {
			if (w == "-o" || w == "--output") && i+1 < len(words) {
				info.explicitOutput = true
				p := words[i+1]
				if !filepath.IsAbs(p) {
					p = filepath.Join(dir, p)
				}
				info.outputs = append(info.outputs, filepath.Clean(p))
			}
		}
		if len(info.outputs) == 0 && !info.project {
			compileOnly := false
			for _, word := range words {
				if word == "-c" || word == "-S" || word == "-E" {
					compileOnly = true
				}
			}
			if kind == "native" && !compileOnly {
				info.outputs = append(info.outputs, filepath.Join(dir, "a.out"))
			}
			if filepath.Base(words[0]) == "rustc" {
				for _, word := range words[1:] {
					if strings.HasSuffix(word, ".rs") {
						info.outputs = append(info.outputs, filepath.Join(dir, strings.TrimSuffix(filepath.Base(word), ".rs")))
						break
					}
				}
			}
		}
	}
	return info
}

func successfulCompiler(h tasks.HistoryTool, info compilerInvocation) bool {
	return !h.IsError && !tasks.HistoryRunning(h) && info.trustedExit && compilerZeroExit.MatchString(h.Output)
}

func executableOperands(h tasks.HistoryTool, dir string) []string {
	command, dir := commandAndDir(h, dir)
	var out []string
	for _, segment := range tasks.SplitShellSegments(command) {
		words := tasks.ShellWords(segment)
		if len(words) == 0 {
			continue
		}
		if words[0] == "cd" && len(words) > 1 {
			if filepath.IsAbs(words[1]) {
				dir = words[1]
			} else {
				dir = filepath.Join(dir, words[1])
			}
			continue
		}
		p := words[0]
		if !strings.Contains(p, "/") {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if elfFile(p) {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// Carry the compiler's original dependency versions into execution receipts.
// Rehashing sources at execution time would certify a stale retained binary.
func bindBuildEvidence(versions map[string]string, h tasks.HistoryTool, dir string, events []ctxcompiler.ActivityEvent, provided func(string) bool) {
	history := historyTools(events)
	binaries := executableOperands(h, dir)
	seen := map[string]bool{}
	for _, binary := range binaries {
		seen[binary] = true
	}
	// Loaded shared libraries are compiled dependencies too. Read receipts
	// cover literal and dynamically discovered library paths without executing
	// source discovery or guessing from the library's extension.
	for path := range versions {
		if !seen[path] && filepath.IsAbs(path) && elfFile(path) {
			binaries = append(binaries, path)
			seen[path] = true
		}
	}
	sort.Strings(binaries)
	inherited := map[string]string{}
	invalid := map[string]bool{}
	for _, binary := range binaries {
		matched := false
		for i := len(history) - 1; i >= 0; i-- {
			build := history[i]
			info := compilerFor(build, dir)
			if info.scope == "" {
				continue
			}
			matches := false
			for _, p := range info.outputs {
				if p == binary {
					matches = true
				}
			}
			// Project build systems choose output names implicitly. Restrict fallback
			// association to the build's tree; explicit -o names match exactly.
			if !matches && info.project && !info.explicitOutput {
				_, buildDir, _ := strings.Cut(strings.TrimPrefix(info.scope, sourceScopePrefix), ":")
				rel, err := filepath.Rel(buildDir, binary)
				matches = err == nil && filepath.IsLocal(rel)
			}
			if !matches {
				continue
			}
			matched = true
			if !(successfulCompiler(build, info) || completedCompilerSession(build, info, history[i+1:])) || len(build.Dependencies) == 0 {
				invalid[binary] = true
				break
			}
			for p, v := range build.Dependencies {
				if old, exists := inherited[p]; exists && old != v {
					inherited[p] = "compiler-source-conflict"
				} else if !exists {
					inherited[p] = v
				}
			}

			break
		}
		// Supplied/system executables do not require a local build. For a new
		// local executable in a source project, absence of a successful build
		// receipt is unknown evidence, never permission to rehash current sources.
		rel, err := filepath.Rel(dir, binary)
		if !matched && err == nil && filepath.IsLocal(rel) && !provided(binary) {
			for _, kind := range []string{"native", "go", "rust"} {
				if len(sourceScopeOperands(sourceScopePrefix+kind+":"+dir)) > 0 {
					invalid[binary] = true
					break
				}
			}
		}
	}
	for path, version := range inherited {
		versions[path] = version
	}
	for path := range invalid {
		versions[path] = "compiler-exit-unverified"
	}
}

func compilerResultHistory(name, args string, res tools.Result) tasks.HistoryTool {
	return tasks.HistoryTool{Name: name, Args: args, Output: res.Output, IsError: res.Status != tools.StatusOK}
}

var compilerZeroExit = regexp.MustCompile(`(?m)^exit: 0(?:\s|$)`)
var compilerSessionID = regexp.MustCompile(`session:\s*(sess-[0-9]+)`)

func compilerExitUnmasked(command string) bool {
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return false
	}
	var safe func(*syntax.Stmt) bool
	safe = func(stmt *syntax.Stmt) bool {
		if stmt.Background || stmt.Negated {
			return false
		}
		switch cmd := stmt.Cmd.(type) {
		case *syntax.CallExpr:
			return true
		case *syntax.BinaryCmd:
			return cmd.Op == syntax.AndStmt && safe(cmd.X) && safe(cmd.Y)
		default:
			return false
		}
	}
	return safe(file.Stmts[0])
}

func completedCompilerSession(build tasks.HistoryTool, info compilerInvocation, later []tasks.HistoryTool) bool {
	if build.IsError || !info.trustedExit || !tasks.HistoryRunning(build) {
		return false
	}
	match := compilerSessionID.FindStringSubmatch(build.Output)
	if len(match) != 2 {
		return false
	}
	for _, h := range later {
		if h.Name != "exec_io" {
			continue
		}
		var args struct {
			Session string `json:"session"`
			Action  string `json:"action"`
		}
		if json.Unmarshal([]byte(h.Args), &args) != nil || args.Session != match[1] || (args.Action != "read" && args.Action != "wait_for") {
			continue
		}
		if !h.IsError && strings.Contains(h.Output, "exited: true") && compilerZeroExit.MatchString(h.Output) {
			return true
		}
	}
	return false
}

func elfFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	n, _ := f.Read(magic[:])
	return n == 4 && string(magic[:]) == "\x7fELF"
}
