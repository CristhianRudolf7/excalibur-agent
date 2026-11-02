package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

var localImportRe = regexp.MustCompile(`(?m)\b(?:from|import|require)\s*\(?\s*["']?([a-zA-Z_][a-zA-Z0-9_./-]*)`)

func (e *Engine) captureEvidenceDependencies(run *Run, tc providers.ToolCall, res tools.Result) map[string]string {
	if !tasks.DependencyFreshnessEnabled() || res.Status != tools.StatusOK || (tc.Name != "exec" && tc.Name != "read_file" && tc.Name != "call_tool") {
		return nil
	}
	run.mu.Lock()
	request, dir := run.Request, run.Workdir
	run.mu.Unlock()
	h := tasks.HistoryTool{Name: tc.Name, Args: string(tc.Arguments), Output: res.Output}
	if !tasks.IsExercisingCall(h) && tc.Name != "read_file" {
		return nil
	}
	paths := historyFileOperands(h, dir)
	for _, f := range e.deliverables(run, request, nil) {
		paths = append(paths, f.Path)
	}
	// Follow literal data operands and local imports in inspected scripts.
	// No code is executed by dependency discovery.
	versions := map[string]string{}
	for len(paths) > 0 && len(versions) < 1024 {
		path := paths[0]
		paths = paths[1:]
		if _, seen := versions[path]; seen {
			continue
		}
		versions[path] = fileVersion(path)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
			continue
		}
		if _, script := programRunners[strings.ToLower(filepath.Ext(path))]; !script {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		candidates := fileOperandRe.FindAllString(string(body), -1)
		for _, m := range localImportRe.FindAllStringSubmatch(string(body), -1) {
			module := strings.ReplaceAll(m[1], ".", "/")
			candidates = append(candidates, module+".py", module+"/__init__.py", module+".js")
		}
		for _, p := range candidates {
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				paths = append(paths, p)
			}
		}
	}
	// Incomplete discovery must fall back to conservative history checks.
	if len(paths) > 0 {
		return nil
	}
	return versions
}
