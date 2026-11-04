package tools

import (
	"sort"
	"strings"
)

func normalizeEnvKey(key string, windows bool) string {
	if windows {
		return strings.ToUpper(key)
	}
	return key
}

func mergeEnvForOS(base []string, over map[string]string, extra []string, windows bool) []string {
	out := append([]string(nil), base...)
	idx := map[string]int{}
	if windows {
		out = nil
	}
	for i, kv := range base {
		if k, _, ok := splitEnvironmentEntry(kv, windows); ok {
			key := normalizeEnvKey(k, windows)
			if windows {
				if j, exists := idx[key]; exists {
					out[j] = kv
					continue
				}
				idx[key] = len(out)
				out = append(out, kv)
			} else {
				idx[key] = i
			}
		} else if windows {
			out = append(out, kv)
		}
	}
	// Map iteration is deliberately unchanged for POSIX. Windows resolves
	// aliases deterministically if callers supply both Path and PATH.
	keys := make([]string, 0, len(over))
	for k := range over {
		keys = append(keys, k)
	}
	if windows {
		sort.Strings(keys)
	}
	for _, k := range keys {
		key := normalizeEnvKey(k, windows)
		if i, ok := idx[key]; ok {
			out[i] = k + "=" + over[k]
		} else {
			out = append(out, k+"="+over[k])
			idx[key] = len(out) - 1
		}
	}
	for _, kv := range extra {
		if k, v, ok := splitEnvironmentEntry(kv, windows); ok {
			key := normalizeEnvKey(k, windows)
			if i, dup := idx[key]; dup {
				out[i] = k + "=" + v
			} else {
				out = append(out, kv)
				idx[key] = len(out) - 1
			}
		}
	}
	return out
}

func splitEnvironmentEntry(entry string, windows bool) (string, string, bool) {
	if windows && strings.HasPrefix(entry, "=") {
		if index := strings.IndexByte(entry[1:], '='); index >= 0 {
			return entry[:index+1], entry[index+2:], true
		}
	}
	return strings.Cut(entry, "=")
}
