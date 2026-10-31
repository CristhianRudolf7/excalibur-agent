package main

import (
	"flag"
	"strings"
)

// parseCmd parses command flags regardless of position: flags may
// come before or after positional arguments
// (e.g. `task wait T45 --timeout 300`, as the delegate skill shows).
// A `--` separator forces everything after it to be positional, so a
// petition starting with `-` still works
// (`exec -- --weird request`).
func parseCmd(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, posArgs []string
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		if name, value, ok := flagName(a); ok && fs.Lookup(name) != nil {
			flagArgs = append(flagArgs, a)
			if !ok || value == "" {
				if !isBoolFlag(fs.Lookup(name).Value) && i+1 < len(args) {
					flagArgs = append(flagArgs, args[i+1])
					i++
				}
			}
			i++
			continue
		}
		posArgs = append(posArgs, a)
		i++
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
	s := strings.TrimLeft(a, "-")
	if s == "" {
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
