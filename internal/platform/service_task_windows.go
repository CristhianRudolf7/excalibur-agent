package platform

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

// schtasks reads XML files as UTF-16. Keep the declaration consistent with
// the little-endian BOM and encode non-ASCII executable paths without loss.
func windowsTaskFile(executable, sid string) []byte {
	document := strings.Replace(windowsTaskXML(executable, sid), `encoding="UTF-8"`, `encoding="UTF-16"`, 1)
	words := utf16.Encode([]rune(document))
	raw := make([]byte, 2+2*len(words))
	binary.LittleEndian.PutUint16(raw, 0xfeff)
	for i, word := range words {
		binary.LittleEndian.PutUint16(raw[2+2*i:], word)
	}
	return raw
}
