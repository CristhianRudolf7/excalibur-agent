package platform

import (
	"testing"
	"time"
	"unsafe"
)

func TestWindowsCurrentUserName(t *testing.T) {
	if name, err := CurrentUserName(); err != nil || name == "" {
		t.Fatalf("name %q: %v", name, err)
	}
}

func TestWindowsLocalTimezone(t *testing.T) {
	for _, tt := range []struct{ input, want string }{{"America/Bogota", "America/Bogota"}, {":Europe/London", "Europe/London"}, {"invalid/zone", "UTC"}, {"UTC", "UTC"}, {"Local", "UTC"}} {
		t.Setenv("TZ", tt.input)
		if got, err := LocalTimezone(); err != nil || got != tt.want {
			t.Fatalf("TZ=%q: %q, %v", tt.input, got, err)
		}
	}
	t.Setenv("TZ", "")
	key, err := windowsTimezoneKey()
	if err != nil {
		t.Fatal(err)
	}
	got, err := LocalTimezone()
	if err != nil || got == "" || got != windowsZones[key] {
		t.Fatalf("native Windows timezone %q: %q, %v", key, got, err)
	}
	if _, err := time.LoadLocation(got); err != nil {
		t.Fatal(err)
	}
	if size := unsafe.Sizeof(dynamicTimezoneInformation{}); size != 432 {
		t.Fatalf("DYNAMIC_TIME_ZONE_INFORMATION size=%d", size)
	}
}
