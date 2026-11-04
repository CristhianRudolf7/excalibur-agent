//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// CurrentUserName returns the account's full name, falling back to its login.
func CurrentUserName() (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", err
	}
	return accountName(account.Name, account.Username)
}

func accountName(fullName, login string) (string, error) {
	if name := strings.TrimSpace(strings.SplitN(fullName, ",", 2)[0]); name != "" {
		return name, nil
	}
	if name := strings.TrimSpace(login); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("platform: account has no name or login")
}

// LocalTimezone resolves the local IANA identifier without persisting a fixed offset.
func LocalTimezone() (string, error) {
	target, _ := filepath.EvalSymlinks("/etc/localtime")
	return darwinTimezone(os.Getenv("TZ"), target, time.Local.String()), nil
}

// darwinTimezone takes resolved inputs so symlink handling can be tested
// without modifying /etc/localtime. TZ takes precedence, as on Linux.
func darwinTimezone(tz, target, fallback string) string {
	zone := strings.TrimPrefix(tz, ":")
	if zone == "" {
		if i := strings.LastIndex(target, "/zoneinfo/"); i >= 0 {
			zone = target[i+len("/zoneinfo/"):]
		}
	}
	if zone == "" {
		zone = fallback
	}
	if zone == "Local" || strings.HasPrefix(zone, "/") {
		return "UTC"
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return "UTC"
	}
	return zone
}
