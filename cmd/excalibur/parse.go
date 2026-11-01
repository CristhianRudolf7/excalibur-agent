package main

import (
	"flag"
	"strings"
)

// parseCmd parses command flags regardless of position: flags may
// come before or after positional arguments. A `--` separator makes
// everything after it positional.
func parseCmd(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, posArgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		name, _, isFlag := flagName(a)
		f := fs.Lookup(name)
		if !isFlag {
			posArgs = append(posArgs, a)
			continue
		}
		if f == nil {
			// Preserve the standard flag package's unknown-flag error instead
			// of silently treating a misspelled option as positional input.
			flagArgs = append(flagArgs, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		// Match flag.FlagSet's value handling: non-boolean flags consume the
		// next token even when it begins with a dash (for example, -1).
		if !strings.Contains(a, "=") && !isBoolFlag(f.Value) && i+1 < len(args) && args[i+1] != "--" {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return append(fs.Args(), posArgs...), nil
}

func flagName(a string) (name, value string, ok bool) {
	if len(a) < 2 || a[0] != '-' {
		return "", "", false
	}
	s := strings.TrimPrefix(a[1:], "-")
	if s == "" || strings.HasPrefix(s, "-") {
		return "", "", false
	}
	if idx := strings.Index(s, "="); idx >= 0 {
		return s[:idx], s[idx+1:], true
	}
	return s, "", true
}

func isBoolFlag(v flag.Value) bool {
	b, ok := v.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
