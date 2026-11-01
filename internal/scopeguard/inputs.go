package scopeguard

import (
	"bytes"
	"os"
	"strings"
)

// namedInputs includes existing extensionless names as well as explicit paths.
func (g *Guard) namedInputs(instruction string) []string {
	ps := pathNames(instruction)
	seen := map[string]bool{}
	for _, p := range ps {
		seen[p] = true
	}
	for _, word := range strings.Fields(instruction) {
		p := strings.Trim(word, "`\"'(),;:.")
		if p == "" || seen[p] {
			continue
		}
		if info, err := os.Stat(g.resolve(p)); err == nil && info.Mode().IsRegular() {
			ps = append(ps, p)
			seen[p] = true
		}
	}
	return ps
}

func dataInput(path string) bool {
	low := strings.ToLower(path)
	for _, ext := range []string{".db", ".sqlite", ".sqlite3", "-wal", "-journal", ".wal", ".journal", ".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".7z", ".bin", ".elf"} {
		if strings.HasSuffix(low, ext) {
			return true
		}
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 512)
	n, _ := f.Read(b)
	b = b[:n]
	return info.Mode().Perm()&0111 != 0 || bytes.IndexByte(b, 0) >= 0 || bytes.HasPrefix(b, []byte("\x7fELF")) || bytes.HasPrefix(b, []byte("MZ")) || bytes.HasPrefix(b, []byte("PK\x03\x04")) || bytes.HasPrefix(b, []byte("!<arch>\n")) || bytes.HasPrefix(b, []byte{0x1f, 0x8b})
}
