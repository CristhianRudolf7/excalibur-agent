package engine

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// commandWords visits executable positions, never comment or quoted argument text.
func commandWords(command string) [][]string {
	f, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return nil
	}
	var out [][]string
	syntax.Walk(f, func(n syntax.Node) bool {
		c, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		var words []string
		for _, w := range c.Args {
			word := w.Lit()
			if word == "" && len(w.Parts) > 0 {
				if prefix, ok := w.Parts[0].(*syntax.Lit); ok {
					if key, _, assigned := strings.Cut(prefix.Value, "="); assigned {
						word = key + "="
					}
				}
			}
			words = append(words, word)
		}
		for len(words) > 0 {
			name := filepath.Base(words[0])
			if name != "sudo" && name != "env" && name != "nohup" && name != "time" && name != "nice" {
				break
			}
			words = words[1:]
			for len(words) > 0 && (strings.HasPrefix(words[0], "-") || strings.Contains(words[0], "=")) {
				option := words[0]
				words = words[1:]
				takesValue := (name == "nice" && (option == "-n" || option == "--adjustment")) ||
					(name == "sudo" && (option == "-u" || option == "-g" || option == "--user" || option == "--group")) ||
					(name == "env" && (option == "-u" || option == "--unset" || option == "-C" || option == "--chdir")) ||
					(name == "time" && (option == "-o" || option == "--output" || option == "-f" || option == "--format"))
				if takesValue && len(words) > 0 {
					words = words[1:]
				}
			}
		}
		if len(words) > 0 {
			words[0] = filepath.Base(words[0])
			out = append(out, words)
		}
		return true
	})
	return out
}

func packageOrBuildCommand(command string) bool {
	for _, w := range commandWords(command) {
		switch w[0] {
		case "apt-get", "apt", "dpkg", "pip", "pip3", "npm", "yarn", "pnpm", "cargo", "make", "cmake", "gcc", "clang", "mvn", "gradle", "conda", "gem", "ninja":
			return true
		case "go":
			if len(w) > 1 && (w[1] == "build" || w[1] == "install" || w[1] == "get") {
				return true
			}
		case "uv":
			if len(w) > 1 && w[1] == "pip" {
				return true
			}
		}
	}
	return false
}

// displayProbeCommand recognizes only compositions of display/probe programs.
// The workspace snapshot still has to prove that the call changed no files.
func displayProbeCommand(command string) bool {
	words := commandWords(command)
	if len(words) == 0 {
		return false
	}
	for _, w := range words {
		switch w[0] {
		case "echo", "printf", "true", "ls", "cat", "head", "tail", "wc", "test", "[":
		case "grep":
			count := false
			for _, a := range w[1:] {
				if a == "--count" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "c")) {
					count = true
				}
			}
			if !count {
				return false
			}
		case "git":
			if len(w) < 2 {
				return false
			}
			if w[1] == "status" {
				continue
			}
			if w[1] != "diff" {
				return false
			}
			stat := false
			for _, a := range w[2:] {
				if a == "--stat" {
					stat = true
				}
			}
			if !stat {
				return false
			}
		case "python", "python3":
			if len(w) < 3 || w[1] != "-m" || w[2] != "py_compile" {
				return false
			}
		case "py_compile":
		default:
			return false
		}
	}
	return true
}
