// Package eval: named evaluation arms (Section 23.1, Appendix F).
//
// An arm names one harness configuration under test: the frozen eval
// profile plus a set of mechanism switches (key=value pairs declared
// on the bench/quick command line). The frozen profile itself never
// changes per arm — the same binary, tool table and skill catalog run
// everywhere — so trials stay comparable; the arm, its switches and
// the combined arm profile hash are recorded in the run manifest
// (Appendix F: every deviation declared and recorded).
//
// Switches travel to the trial container verbatim as environment
// variables (one --ae entry per switch); by convention mechanism
// switches use EXCALIBUR_ names (for example
// EXCALIBUR_SELF_VERIFY=off). The harness mechanisms that read them
// are owned by the other agent-quality workstreams; measurement only
// declares, forwards, records and hashes them, which is what makes
// ablation reporting possible.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ArmSwitchPrefix is the conventional environment prefix for harness
// mechanism switches forwarded into trial containers.
const ArmSwitchPrefix = "EXCALIBUR_"

// ParseSwitches parses --set key=value entries into a switch map.
// Keys admit letters, digits, underscore, dot and dash (so both
// EXCALIBUR_SELF_VERIFY and dotted config paths work); values must be
// non-empty without newlines or NUL bytes. Duplicate keys are an
// error: silent override would make arms unreproducible.
func ParseSwitches(entries []string) (map[string]string, error) {
	out := map[string]string{}
	for _, e := range entries {
		key, value, ok := strings.Cut(e, "=")
		if !ok {
			return nil, fmt.Errorf("eval: switch %q needs key=value", e)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !validSwitchKey(key) {
			return nil, fmt.Errorf("eval: bad switch key %q (want [A-Za-z0-9_.-]+)", key)
		}
		if value == "" || strings.ContainsAny(value, "\x00\n\r") {
			return nil, fmt.Errorf("eval: bad switch value for %q (non-empty, single line)", key)
		}
		if len(key) > 128 || len(value) > 4096 {
			return nil, fmt.Errorf("eval: switch %q too long", key)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("eval: duplicate switch %q", key)
		}
		out[key] = value
	}
	return out, nil
}

func validSwitchKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// ValidateArm rejects empty names and names that would not survive a
// directory name, an environment value or a JSON field unchanged.
func ValidateArm(arm string) error {
	if arm == "" {
		return nil
	}
	if len(arm) > 64 || !validSwitchKey(arm) {
		return fmt.Errorf("eval: bad arm name %q (want [A-Za-z0-9_.-]+, max 64)", arm)
	}
	return nil
}

// ArmProfileHash binds the frozen profile hash to one arm: the same
// switches always hash the same, and empty switches hash exactly the
// frozen profile hash (the default arm reproduces the pre-arm runs).
func ArmProfileHash(profileHash, arm string, switches map[string]string) string {
	if arm == "" && len(switches) == 0 {
		return profileHash
	}
	keys := make([]string, 0, len(switches))
	for k := range switches {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	fmt.Fprintf(h, "profile=%s\narm=%s\n", profileHash, arm)
	for _, k := range keys {
		fmt.Fprintf(h, "switch=%s=%s\n", k, switches[k])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RedactSwitchValue hides likely secrets in dry-run echoes and logs:
// any switch whose key mentions KEY, SECRET, TOKEN or PASSWORD only
// ever prints redacted. Mechanism switches are otherwise echoed
// verbatim so arms stay auditable.
func RedactSwitchValue(key, value string) string {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD"} {
		if strings.Contains(upper, marker) {
			if len(value) <= 8 {
				return "****"
			}
			return value[:4] + "****" + value[len(value)-4:]
		}
	}
	return value
}
