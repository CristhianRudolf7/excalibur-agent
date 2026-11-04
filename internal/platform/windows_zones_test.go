package platform

import (
	"testing"
	"time"
	_ "time/tzdata"
)

func TestWindowsZonesWorldwideDefaults(t *testing.T) {
	for key, want := range map[string]string{
		"SA Pacific Standard Time":   "America/Bogota",
		"Pacific Standard Time":      "America/Los_Angeles",
		"Mountain Standard Time":     "America/Denver",
		"Central Standard Time":      "America/Chicago",
		"Eastern Standard Time":      "America/New_York",
		"GMT Standard Time":          "Europe/London",
		"Romance Standard Time":      "Europe/Paris",
		"W. Europe Standard Time":    "Europe/Berlin",
		"Tokyo Standard Time":        "Asia/Tokyo",
		"India Standard Time":        "Asia/Calcutta",
		"China Standard Time":        "Asia/Shanghai",
		"AUS Eastern Standard Time":  "Australia/Sydney",
		"New Zealand Standard Time":  "Pacific/Auckland",
		"South Africa Standard Time": "Africa/Johannesburg",
		"UTC":                        "Etc/UTC",
	} {
		if got := windowsZones[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if len(windowsZones) != 139 {
		t.Fatalf("CLDR 48 worldwide keys: got %d, want 139", len(windowsZones))
	}
	for key, zone := range windowsZones {
		if _, err := time.LoadLocation(zone); err != nil {
			t.Errorf("%s maps to invalid IANA zone %q: %v", key, zone, err)
		}
	}
	if _, ok := windowsZones["unknown registry key"]; ok {
		t.Fatal("unknown Windows key must not map to a fixed offset")
	}
}
