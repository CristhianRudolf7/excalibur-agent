package daemon

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func principalUserEnv() []string { return []string{"USER", "LOGNAME", "USERNAME"} }
func defaultPrincipalLanguage() string {
	var locale [85]uint16 // LOCALE_NAME_MAX_LENGTH
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultLocaleName")
	if err := proc.Find(); err != nil {
		return "en"
	}
	count, _, _ := proc.Call(uintptr(unsafe.Pointer(&locale[0])), uintptr(len(locale)))
	if count == 0 {
		return "en"
	}
	name := strings.ToLower(windows.UTF16ToString(locale[:]))
	if name == "es" || strings.HasPrefix(name, "es-") {
		return "es"
	}
	return "en"
}
