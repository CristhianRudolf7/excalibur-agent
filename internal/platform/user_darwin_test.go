//go:build darwin

package platform

import "testing"

func TestCurrentUserNameAndTimezone(t *testing.T) {
	name, err := CurrentUserName()
	if err != nil || name == "" {
		t.Fatalf("account name %q: %v", name, err)
	}
	for _, tc := range []struct{ env, want string }{{"America/Bogota", "America/Bogota"}, {"invalid/zone", "UTC"}, {"UTC", "UTC"}} {
		t.Setenv("TZ", tc.env)
		zone, err := LocalTimezone()
		if err != nil || zone != tc.want {
			t.Fatalf("%s: %q %v", tc.env, zone, err)
		}
	}
}

func TestAccountNameFullNameThenLogin(t *testing.T) {
	for _, tc := range []struct{ full, login, want string }{{" Full Name ,Office,Phone", "login", "Full Name"}, {"", " login ", "login"}, {" ,Office", "login", "login"}} {
		name, err := accountName(tc.full, tc.login)
		if err != nil || name != tc.want {
			t.Fatalf("name=%q err=%v, want %q", name, err, tc.want)
		}
	}
	if _, err := accountName("", ""); err == nil {
		t.Fatal("missing account must report lookup failure for the caller's fallback")
	}
}

func TestDarwinTimezoneSymlinks(t *testing.T) {
	for _, tc := range []struct{ tz, target, fallback, want string }{
		{"", "/var/db/timezone/zoneinfo/America/Lima", "Local", "America/Lima"},
		{"", "/private/var/db/timezone/zoneinfo/2026/zoneinfo/Europe/London", "Local", "Europe/London"},
		{":America/New_York", "/var/db/timezone/zoneinfo/America/Lima", "Local", "America/New_York"},
		{"invalid/zone", "/var/db/timezone/zoneinfo/America/Lima", "Local", "UTC"},
		{"", "/other/file", "Local", "UTC"},
		{"", "", "Europe/Paris", "Europe/Paris"},
		{"/absolute/path", "", "Local", "UTC"},
	} {
		if got := darwinTimezone(tc.tz, tc.target, tc.fallback); got != tc.want {
			t.Fatalf("%+v: %q", tc, got)
		}
	}
}
