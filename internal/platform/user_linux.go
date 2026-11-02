//go:build linux

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
	zone := strings.TrimPrefix(os.Getenv("TZ"), ":")
	if zone == "" {
		if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
			if _, suffix, found := strings.Cut(path, "/zoneinfo/"); found {
				zone = suffix
			}
		}
	}
	if zone == "" {
		if data, err := os.ReadFile("/etc/timezone"); err == nil {
			zone = strings.TrimSpace(string(data))
		}
	}
	if zone == "" {
		zone = time.Local.String()
	}
	if zone == "Local" || strings.HasPrefix(zone, "/") {
		return "UTC", nil
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return "UTC", nil
	}
	return zone, nil
}
