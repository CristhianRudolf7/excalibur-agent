package platform

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf16"
)

// windowsEnvironmentBlock builds a sorted, double-NUL-terminated Unicode
// environment. Names are case insensitive and the last duplicate wins, as in
// os/exec. Drive-current-directory entries (=C:=...) keep their leading '='.
func windowsEnvironmentBlock(env []string) ([]uint16, error) {
	values := make(map[string]string)
	for _, entry := range env {
		if strings.ContainsRune(entry, 0) {
			return nil, errors.New("platform: environment contains NUL")
		}
		start := 0
		if strings.HasPrefix(entry, "=") {
			start = 1
		}
		i := strings.IndexByte(entry[start:], '=')
		if i < 0 || i+start == 0 {
			continue
		}
		values[strings.ToUpper(entry[:i+start])] = entry
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var block []uint16
	for _, k := range keys {
		block = append(block, utf16.Encode([]rune(values[k]))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	return append(block, 0), nil
}
