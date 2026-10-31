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
		if !isFlag || f == nil {
			posArgs = append(posArgs, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		// Consume a value for non-boolean flags, but not another recognized
		// flag. This preserves missing-value errors and positional values.
		if !strings.Contains(a, "=") && !isBoolFlag(f.Value) && i+1 < len(args) {
			nextName, _, nextIsFlag := flagName(args[i+1])
			if args[i+1] != "--" && (!nextIsFlag || fs.Lookup(nextName) == nil) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
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
