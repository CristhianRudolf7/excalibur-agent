package daemon

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type deliveryFormatter struct {
	Check string   `json:"check"`
	Files []string `json:"files"`
}

type deliverySource struct {
	path    string
	changed time.Time
}

func sourceExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".css", ".scss", ".html", ".vue", ".svelte",
		".go", ".py", ".rs", ".java", ".c", ".h", ".cpp", ".hpp", ".cs", ".rb", ".sh", ".sql":
		return true
	}
	return false
}

func excludedSourceDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "vendored", "dist", "build", "out", "coverage", "generated",
		".next", ".nuxt", ".svelte-kit", ".venv", "venv", "__pycache__", "target":
		return true
	}
	return false
}

var generatedSourceHeader = regexp.MustCompile(`(?m)^(?://|#|/\*| \*) (?:Code generated .*DO NOT EDIT\.|@generated\b)`)

// Scan the workspace, not just files volunteered in the requirement matrix.
// Root.FS keeps traversal and reads inside the delegated workspace.
func deliverySources(root *os.Root) ([]deliverySource, []string) {
	var files []deliverySource
	var pending []string
	err := fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if excludedSourceDir(entry.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !sourceExtension(path) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			pending = append(pending, path+":1: hand-written source must be a regular workspace file")
			return nil
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return err
		}
		const limit = 4 << 20
		source, err := io.ReadAll(io.LimitReader(file, limit+1))
		_ = file.Close()
		if err != nil {
			return err
		}
		header := source[:min(len(source), 1024)]
		if generatedSourceHeader.Match(header) {
			return nil
		}
		files = append(files, deliverySource{path: path, changed: info.ModTime()})
		if len(source) > limit {
			pending = append(pending, path+":1: source exceeds readability scan limit; split into modules")
			return nil
		}
		pending = append(pending, sourceReadability(path, string(source))...)
		return nil
	})
	if err != nil {
		pending = append(pending, "source readability scan failed: "+err.Error())
	}
	return files, pending
}

func (e *deliveryEvidence) checkSourceQuality(root *os.Root, formatters []deliveryFormatter) ([]deliverySource, []string) {
	files, pending := deliverySources(root)
	covered := map[string]time.Time{}
	for _, formatter := range formatters {
		if !e.passed[formatter.Check] || !formatterCommand(root, formatter.Check) {
			pending = append(pending, "execute an offline formatter successfully (project formatter or prettier/gofmt/black): "+formatter.Check)
			continue
		}
		for _, file := range formatter.Files {
			if !fs.ValidPath(file) || !sourceExtension(file) {
				pending = append(pending, "invalid formatted source path: "+file)
				continue
			}
			if !formatterCovers(root, formatter.Check, file) {
				pending = append(pending, file+":1: formatter command must target this source file with a compatible formatter")
				continue
			}
			if checked := e.checkedAt[formatter.Check]; checked.After(covered[file]) {
				covered[file] = checked
			}
		}
	}
	for _, file := range files {
		if covered[file.path].IsZero() || file.changed.After(covered[file.path]) {
			pending = append(pending, file.path+":1: run the formatter after the latest edit and include its exact command and this file in formatters")
		}
	}
	return files, pending
}

// Accept a formatter invocation, never a command merely mentioning one. Project
// scripts are read from package.json; network-installing wrappers are disallowed.
func formatterCommand(root *os.Root, command string) bool {
	args := formatterArgs(root, command)
	if len(args) < 2 {
		return false
	}
	tool := filepath.Base(args[0])
	has := func(flag string) bool {
		for _, arg := range args[1:] {
			if arg == flag {
				return true
			}
		}
		return false
	}
	if has("--version") || has("--help") || has("-h") || has("--exit-zero") {
		return false
	}
	switch tool {
	case "prettier":
		return has("--check") || has("--write")
	case "gofmt":
		// -l and -d alone exit zero even when files are unformatted.
		return has("-w")
	case "black":
		return !has("--diff") || has("--check")
	case "ruff":
		return args[1] == "format" && (!has("--diff") || has("--check"))
	case "rustfmt":
		return !has("--emit")
	case "clang-format":
		return has("-i") || has("--dry-run") && has("--Werror")
	case "shfmt":
		return has("-w") || has("-d")
	}
	return false
}

func formatterArgs(root *os.Root, command string) []string {
	if strings.ContainsAny(command, ";&|<>`\n\r$") {
		return nil
	}
	args := strings.Fields(command)
	if len(args) < 2 {
		return nil
	}
	if args[0] == "npm" || args[0] == "pnpm" || args[0] == "yarn" {
		return projectFormatterArgs(root, args)
	}
	if args[0] == "npx" && len(args) > 3 && args[1] == "--no-install" {
		args = args[2:]
	}
	if len(args) > 3 && (args[0] == "python" || args[0] == "python3") && args[1] == "-m" {
		args = args[2:]
	}
	return args
}

var literalDataLine = regexp.MustCompile(`^[\s\d\[\]{},:.+\-"']+$`)
var literalDataArray = regexp.MustCompile(`\[[\s\d\[\]{},:.+\-"']*\]`)

func sourceReadability(path, source string) []string {
	var pending []string
	lineCount := len(strings.Split(strings.TrimSuffix(source, "\n"), "\n"))
	if lineCount > 600 {
		pending = append(pending, fmt.Sprintf("%s:601: source has %d lines (maximum 600); split responsibilities into modules/components", path, lineCount))
	}
	masked := literalDataArray.ReplaceAllStringFunc(sourceCodeOnly(path, source), func(data string) string {
		return "[]" + strings.Repeat("\n", strings.Count(data, "\n"))
	})
	total, lines, first := 0, 0, 0
	braces, parens := 0, 0
	for index, line := range strings.Split(masked, "\n") {
		statements := 0
		for _, char := range line {
			switch char {
			case '{':
				braces++
			case '}':
				braces = max(0, braces-1)
			case '(':
				parens++
			case ')':
				parens = max(0, parens-1)
			case ';':
				if braces == 0 && parens == 0 {
					statements++
				}
			}
		}
		if statements > 2 {
			pending = append(pending, fmt.Sprintf("%s:%d: %d top-level statements on one line (maximum 2)", path, index+1, statements))
		}
		if strings.TrimSpace(line) == "" || literalDataLine.MatchString(line) {
			continue
		}
		length := utf8.RuneCountInString(strings.TrimRight(line, " \t\r"))
		total += length
		lines++
		if first == 0 {
			first = index + 1
		}
		if length > 200 {
			pending = append(pending, fmt.Sprintf("%s:%d: code line has %d characters outside literals/data (maximum 200); format and split it", path, index+1, length))
		}
	}
	if lines > 0 && total > lines*100 {
		pending = append(pending, fmt.Sprintf("%s:%d: average code line length %.1f exceeds 100; format and split it", path, first, float64(total)/float64(lines)))
	}
	return pending
}
