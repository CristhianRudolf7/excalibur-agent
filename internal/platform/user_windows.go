package platform

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"
	_ "time/tzdata" // Windows installations need not provide an IANA database.
	"unsafe"

	"golang.org/x/sys/windows"
)

// CurrentUserName returns the Windows account's full name, falling back to login.
func CurrentUserName() (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", err
	}
	if name := strings.TrimSpace(account.Name); name != "" {
		return name, nil
	}
	if name := strings.TrimSpace(account.Username); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("platform: account has no name or login")
}

// LocalTimezone resolves TZ or the native Windows zone's CLDR worldwide IANA
// default. Invalid explicit TZ settings retain the Linux UTC fallback.
func LocalTimezone() (string, error) {
	zone := strings.TrimPrefix(os.Getenv("TZ"), ":")
	if zone == "" {
		key, err := windowsTimezoneKey()
		if err != nil {
			return "", err
		}
		if name, ok := windowsZones[key]; ok {
			return name, nil
		}
		return "", fmt.Errorf("platform: unmapped Windows time zone %q: %w", key, errors.ErrUnsupported)
	}
	if strings.HasPrefix(zone, "/") || zone == "Local" {
		return "UTC", nil
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return "UTC", nil
	}
	return zone, nil
}

// dynamicTimezoneInformation matches DYNAMIC_TIME_ZONE_INFORMATION, including
// the registry key (StandardName is localized and must not be used for lookup).
type dynamicTimezoneInformation struct {
	Bias                        int32
	StandardName                [32]uint16
	StandardDate                windows.Systemtime
	StandardBias                int32
	DaylightName                [32]uint16
	DaylightDate                windows.Systemtime
	DaylightBias                int32
	TimeZoneKeyName             [128]uint16
	DynamicDaylightTimeDisabled uint8
}

func windowsTimezoneKey() (string, error) {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDynamicTimeZoneInformation")
	if err := proc.Find(); err != nil {
		return "", err
	}
	var info dynamicTimezoneInformation
	result, _, err := proc.Call(uintptr(unsafe.Pointer(&info)))
	if uint32(result) == 0xffffffff {
		return "", fmt.Errorf("platform: GetDynamicTimeZoneInformation: %w", err)
	}
	return windows.UTF16ToString(info.TimeZoneKeyName[:]), nil
}
