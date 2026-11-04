package daemon

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func projectFormatterArgs(root *os.Root, args []string) []string {
	args = args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil
	}
	file, err := root.Open("package.json")
	if err != nil {
		return nil
	}
	defer file.Close()
	var project struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&project) != nil {
		return nil
	}
	script := project.Scripts[args[0]]
	// Only expand a single direct formatter script; do not recurse into npm.
	if fields := strings.Fields(script); len(fields) == 0 || fields[0] == "npm" || fields[0] == "pnpm" || fields[0] == "yarn" {
		return nil
	}
	return formatterArgs(root, script)
}

func formatterCovers(root *os.Root, command, path string) bool {
	args := formatterArgs(root, command)
	if len(args) < 2 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch filepath.Base(args[0]) {
	case "prettier":
		if !strings.Contains("|.js|.jsx|.ts|.tsx|.mjs|.cjs|.css|.scss|.html|.vue|.svelte|", "|"+ext+"|") {
			return false
		}
	case "gofmt":
		if ext != ".go" {
			return false
		}
	case "black", "ruff":
		if ext != ".py" {
			return false
		}
	case "rustfmt":
		if ext != ".rs" {
			return false
		}
	case "shfmt":
		if ext != ".sh" {
			return false
		}
	case "clang-format":
		if !strings.Contains("|.c|.h|.cpp|.hpp|.cs|.java|", "|"+ext+"|") {
			return false
		}
	default:
		return false
	}
	for index := 1; index < len(args); index++ {
		arg := args[index]
		if strings.HasPrefix(arg, "-") {
			// Flags accepting a value must not accidentally expand coverage.
			switch arg {
			case "--config", "--ignore-path", "--parser", "--exclude", "--extend-exclude", "--line-length", "--target-version", "--stdin-filepath", "-style", "--style":
				index++
			}
			continue
		}
		arg = strings.TrimPrefix(strings.Trim(arg, `"'`), "./")
		if arg == "." || arg == path || strings.HasPrefix(path, strings.TrimSuffix(arg, "/")+"/") {
			return true
		}
		if matched, _ := filepath.Match(arg, path); matched {
			return true
		}
	}
	return false
}
