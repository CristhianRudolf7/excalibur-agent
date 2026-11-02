package engine

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

const sourceScopePrefix = "\x00source-scope:"

// Project runners read source/config files implicitly. Include their source
// manifest so adding a new source file also invalidates the old check. Ordinary
// diagnostic output files do not become source dependencies.
func projectSourceScope(h tasks.HistoryTool, dir string) string {
	var a struct {
		Command string `json:"command"`
		Workdir string `json:"workdir"`
	}
	if json.Unmarshal([]byte(h.Args), &a) != nil {
		return ""
	}
	if a.Workdir != "" {
		if filepath.IsAbs(a.Workdir) {
			dir = a.Workdir
		} else {
			dir = filepath.Join(dir, a.Workdir)
		}
	}
	if dir == "" {
		return ""
	}
	for _, segment := range tasks.SplitShellSegments(a.Command) {
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
		// Explicit source operands define a narrower check than a project-wide
		// runner. Its actual reads/imports supply the remaining dependencies.
		explicitSource := false
		for _, word := range words[1:] {
			switch strings.ToLower(filepath.Ext(word)) {
			case ".py", ".go", ".rs", ".c", ".cc", ".cpp", ".js", ".mjs", ".ts", ".tsx", ".jsx":
				explicitSource = true
			}
		}
		if explicitSource {
			continue
		}
		kind := ""
		switch filepath.Base(words[0]) {
		case "go":
			kind = "go"
		case "cargo":
			kind = "rust"
		case "npm", "npx", "yarn", "pnpm":
			kind = "javascript"
		case "bun", "deno":
			if len(words) < 2 || words[1] != "test" {
				continue
			}
			kind = "javascript"
		case "pytest", "py.test":
			kind = "python"
		case "python", "python3":
			projectTest := false
			for i := 1; i+1 < len(words); i++ {
				if words[i] == "-m" && (words[i+1] == "pytest" || words[i+1] == "unittest") {
					projectTest = true
				}
			}
			if !projectTest {
				continue
			}
			kind = "python"
		case "make", "cmake", "ninja":
			kind = "native"
		}
		if kind != "" {
			return sourceScopePrefix + kind + ":" + filepath.Clean(dir)
		}
	}
	return ""
}

func sourceScopeFile(kind, path string) bool {
	base, ext := filepath.Base(path), strings.ToLower(filepath.Ext(path))
	switch kind {
	case "go":
		return ext == ".go" || base == "go.mod" || base == "go.sum" || base == "go.work" || base == "go.work.sum"
	case "rust":
		return ext == ".rs" || base == "Cargo.toml" || base == "Cargo.lock"
	case "python":
		return ext == ".py" || base == "pyproject.toml" || base == "pytest.ini" || base == "setup.cfg" || base == "requirements.txt"
	case "javascript":
		return ext == ".js" || ext == ".mjs" || ext == ".cjs" || ext == ".ts" || ext == ".tsx" || ext == ".jsx" || base == "package.json" || strings.HasSuffix(base, "lock.json") || base == "tsconfig.json"
	case "native":
		return ext == ".c" || ext == ".cc" || ext == ".cpp" || ext == ".h" || ext == ".hpp" || ext == ".cmake" || base == "Makefile" || base == "CMakeLists.txt" || base == "build.ninja"
	}
	return false
}

func sourceScopeVersion(scope string) string {
	kind, dir, ok := strings.Cut(strings.TrimPrefix(scope, sourceScopePrefix), ":")
	if !ok {
		return "unreadable"
	}
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if sourceScopeFile(kind, path) {
			files = append(files, path)
			if len(files) > 8192 {
				return fmt.Errorf("source scope exceeds receipt limit")
			}
		}
		return nil
	})
	if err != nil {
		return "unreadable"
	}
	sort.Strings(files)
	digest := sha256.New()
	for _, path := range files {
		fmt.Fprintf(digest, "%s\x00%s\n", path, fileVersion(path))
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

// Source files supply additional literal data references; generated logs and
// unrelated workspace files are excluded unless a checked source names them.
func sourceScopeOperands(scope string) []string {
	kind, dir, ok := strings.Cut(strings.TrimPrefix(scope, sourceScopePrefix), ":")
	if !ok {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if sourceScopeFile(kind, path) {
			if info, err := d.Info(); err == nil && info.Mode().IsRegular() && info.Size() <= 256<<10 {
				out = append(out, path)
			}
		}
		if len(out) > 8192 {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

func validDependencyVersions(versions map[string]string) bool {
	if len(versions) == 0 {
		return false
	}
	for _, version := range versions {
		if version == "unreadable" {
			return false
		}
	}
	return true
}
