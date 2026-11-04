package daemon

import (
	"errors"
	"testing"
)

func TestWindowsPrincipalFallback(t *testing.T) {
	env := func(key string) string {
		if key == "USERNAME" {
			return "Windows User"
		}
		return ""
	}
	name, err := principalName(env, func() (string, error) { return "", errors.New("no profile") })
	if err != nil || name != "Windows User" {
		t.Fatalf("username fallback = %q, %v", name, err)
	}
	if got := defaultPrincipalLanguage(); got != "en" && got != "es" {
		t.Fatalf("unsupported locale mapping: %q", got)
	}
}
func TestWindowsNotifyNoop(t *testing.T) {
	if err := SendNotify("not-a-unix-socket", "WATCHDOG=1"); err != nil {
		t.Fatal(err)
	}
}
