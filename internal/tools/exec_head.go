package tools

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// execHeadLimitNote inspects the final output stage, before harness clipping.
// Intermediate stages cannot be counted from the captured terminal output.
// The note is advisory for both interactive and autonomous callers.
func execHeadLimitNote(command, output string) string {
	if off("EXCALIBUR_EXEC_HEAD_LIMIT") || output == "" {
		return ""
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil || len(file.Stmts) != 1 {
		return ""
	}
	stmt := file.Stmts[0]
	piped := false
	for {
		binary, ok := stmt.Cmd.(*syntax.BinaryCmd)
		if !ok {
			break
		}
		if binary.Op != syntax.Pipe && binary.Op != syntax.PipeAll {
			return ""
		}
		piped = true
		stmt = binary.Y
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 || len(stmt.Redirs) != 0 {
		return ""
	}
	var args []string
	for _, word := range call.Args {
		arg, literal := literalShellWord(word)
		if !literal {
			return ""
		}
		args = append(args, arg)
	}
	name := filepath.Base(args[0])
	limit := 0
	switch name {
	case "head":
		if !piped {
			return ""
		}
		limit = 10
		for i := 1; i < len(args); i++ {
			arg := args[i]
			if arg == "-n" && i+1 < len(args) {
				i++
				limit, _ = strconv.Atoi(args[i])
			} else if strings.HasPrefix(arg, "-n") {
				limit, _ = strconv.Atoi(strings.TrimPrefix(arg, "-n"))
			} else if strings.HasPrefix(arg, "-") {
				limit, _ = strconv.Atoi(strings.TrimPrefix(arg, "-"))
			} else {
				return ""
			} // File operands can add headers and multiple limits.
		}
	case "grep":
		for i := 1; i < len(args); i++ {
			if args[i] == "--" {
				break
			}
			if args[i] == "-m" && i+1 < len(args) {
				i++
				limit, _ = strconv.Atoi(args[i])
			} else if strings.HasPrefix(args[i], "-m") {
				limit, _ = strconv.Atoi(strings.TrimPrefix(args[i], "-m"))
			}
		}
	default:
		return ""
	}
	lines := strings.Count(output, "\n")
	if !strings.HasSuffix(output, "\n") {
		lines++
	}
	if limit <= 0 || lines != limit {
		return ""
	}
	return fmt.Sprintf("note: output stopped at the head limit (%d lines); there may be more results — count them (grep -c, wc -l) or list files (grep -l) before relying on this list\n", limit)
}
