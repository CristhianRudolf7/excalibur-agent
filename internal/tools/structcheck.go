package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// Parse checks for write_file (EXCALIBUR_STRUCTURED_WRITE_CHECK). A
// shell escape turns real newlines into the two characters backslash
// and n, so a structured file arrives as one line that does not parse.
// Refusing that case before the write, and warning after a write that
// does not parse, keeps a broken config out of the workspace.

// errCheckUnavailable means a format cannot be checked on this host,
// for example when python3 is not on PATH. It never refuses a write.
var errCheckUnavailable = errors.New("parser unavailable on this host")

// literalEscapeText is the refusal for single-line text whose literal
// backslash-n sequences parse only as real newlines.
const literalEscapeText = `content has literal \n sequences; send real newlines`

type structuredFormat struct {
	name  string
	parse func(content string) error
	// warn adds a post-write warning line when the written file does
	// not parse. Only the formats the harness parses in-process warn.
	warn bool
}

// formatForPath returns the parse check for a file extension.
func formatForPath(path string) (structuredFormat, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return structuredFormat{name: "JSON", parse: checkJSON, warn: true}, true
	case ".toml":
		return structuredFormat{name: "TOML", parse: checkTOML, warn: true}, true
	case ".yaml", ".yml":
		return structuredFormat{name: "YAML", parse: checkYAML, warn: true}, true
	case ".ini", ".cfg":
		return structuredFormat{name: "INI", parse: checkINI}, true
	case ".py":
		return structuredFormat{name: "Python", parse: checkPython}, true
	case ".sh":
		return structuredFormat{name: "shell", parse: checkShell}, true
	}
	return structuredFormat{}, false
}

// literalEscapeRefusal refuses a single-line write whose text does not
// parse as written but parses once its literal \n sequences become real
// newlines. It returns "" when the write may proceed.
func literalEscapeRefusal(path, content string) string {
	f, ok := formatForPath(path)
	if !ok || strings.Contains(content, "\n") || !strings.Contains(content, `\n`) {
		return ""
	}
	err := f.parse(content)
	if err == nil || errors.Is(err, errCheckUnavailable) {
		return ""
	}
	if f.parse(strings.ReplaceAll(content, `\n`, "\n")) != nil {
		return ""
	}
	return fmt.Sprintf("refused: %s. As written the %s text does not parse (%v); with real newlines it does", literalEscapeText, f.name, err)
}

// parseWarning returns a warning line when a written JSON, TOML or YAML
// file does not parse, or "" when it parses or cannot be checked.
func parseWarning(path, content string) string {
	f, ok := formatForPath(path)
	if !ok || !f.warn {
		return ""
	}
	err := f.parse(content)
	if err == nil || errors.Is(err, errCheckUnavailable) {
		return ""
	}
	return fmt.Sprintf("warning: %s does not parse as %s: %v", path, f.name, err)
}

func checkJSON(content string) error {
	var v any
	return json.Unmarshal([]byte(content), &v)
}

func checkTOML(content string) error {
	var v map[string]any
	_, err := toml.Decode(content, &v)
	return err
}

func checkYAML(content string) error {
	var v any
	return yaml.Unmarshal([]byte(content), &v)
}

// checkINI accepts comments, [section] headers and key = value (or
// key: value) lines. It is deliberately permissive about values.
func checkINI(content string) error {
	for i, line := range strings.Split(content, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "", strings.HasPrefix(t, "#"), strings.HasPrefix(t, ";"):
		case strings.HasPrefix(t, "["):
			if len(t) < 3 || !strings.HasSuffix(t, "]") {
				return fmt.Errorf("line %d: malformed section header", i+1)
			}
		default:
			j := strings.IndexAny(t, "=:")
			if j < 0 {
				return fmt.Errorf("line %d: expected key = value", i+1)
			}
			if strings.TrimSpace(t[:j]) == "" {
				return fmt.Errorf("line %d: empty key", i+1)
			}
		}
	}
	return nil
}

// checkPython and checkShell use the host interpreter in syntax-only
// mode with the text on stdin. Nothing is executed. They run only for
// single-line writes that carry literal backslash-n sequences, so the
// cost is paid rarely.
func checkPython(content string) error {
	return runStdinCheck(content, "python3", "-c", "import ast, sys; ast.parse(sys.stdin.read(), '<write_file>')")
}

func checkShell(content string) error {
	return runStdinCheck(content, "sh", "-n")
}

func runStdinCheck(content string, argv ...string) error {
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return errCheckUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, argv[1:]...)
	cmd.Stdin = strings.NewReader(content)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return errCheckUnavailable
		}
		return errors.New(lastNonEmptyLine(string(out)))
	}
	return nil
}

// lastNonEmptyLine returns the final non-blank line of tool output,
// clipped, which is where a parser states the error.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	last := "syntax error"
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			last = t
		}
	}
	return clipText(last, 200)
}
