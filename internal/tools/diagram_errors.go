package tools

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	diagramLinePosition     = regexp.MustCompile(`(?im)\bline\s*[:=]?\s+(\d+)\b[: ,)]*(.*)`)
	diagramFilePosition     = regexp.MustCompile(`(?m)^[^\n]*?:(\d+)(?::\d+)?:[ \t]*(.+)$`)
	diagramPlantUMLPosition = regexp.MustCompile(`(?im)^ERROR\s*\n(\d+)\s*\n([^\n]+)`)
)

// diagramDiagnostic keeps renderer grammar details but presents a stable source
// location for repair. Unrecognized diagnostics remain useful without a position.
func diagramDiagnostic(diagnostic string) string {
	diagnostic = strings.TrimSpace(diagnostic)
	if len(diagnostic) > 4096 {
		diagnostic = diagnostic[:4096] + "…"
	}
	for _, pattern := range []*regexp.Regexp{diagramPlantUMLPosition, diagramLinePosition, diagramFilePosition} {
		input := diagnostic
		if pattern == diagramFilePosition {
			// JavaScript/Java stack frames describe the renderer, not the source.
			var lines []string
			for _, line := range strings.Split(diagnostic, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "at ") {
					lines = append(lines, line)
				}
			}
			input = strings.Join(lines, "\n")
		}
		match := pattern.FindStringSubmatch(input)
		if match == nil {
			continue
		}
		line, message := match[1], strings.TrimSpace(match[2])
		if line == "0" {
			line = "1"
		}
		// Mermaid places its useful grammar expectation after the source and caret.
		for _, part := range strings.Split(diagnostic, "\n") {
			if strings.HasPrefix(strings.TrimSpace(part), "Expecting ") {
				message = strings.TrimSpace(part)
				break
			}
		}
		if message == "" {
			for _, part := range strings.Split(diagnostic, "\n") {
				if strings.Contains(strings.ToLower(part), "syntax error") {
					message = strings.TrimSpace(part)
					break
				}
			}
		}
		if message == "" {
			message = firstLine(diagnostic)
		}
		if strings.Contains(strings.ToLower(diagnostic), "syntax error") && !strings.Contains(strings.ToLower(message), "syntax error") {
			message = "syntax error " + message
		}
		return fmt.Sprintf("line %s: %s", line, message)
	}
	return diagnostic
}
