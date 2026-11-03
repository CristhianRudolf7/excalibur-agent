package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func TestPrincipalTimezonePersistenceAndSources(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name) VALUES('local','Example')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := NewPrincipalService(db)
	initial, err := s.Get(ctx)
	if err != nil || initial.Timezone != "UTC" || initial.TimezoneSource != "default" {
		t.Fatalf("default: %+v %v", initial, err)
	}
	for _, tc := range []struct {
		zone, source         string
		enable               bool
		wantZone, wantSource string
	}{
		{"America/Lima", "auto", false, "America/Lima", "auto"},
		{"Europe/Madrid", "manual", false, "Europe/Madrid", "manual"},
		{"Asia/Tokyo", "auto", false, "Europe/Madrid", "manual"},
		{"Asia/Kathmandu", "auto", true, "Asia/Kathmandu", "auto"},
		{"UTC", "default", false, "UTC", "default"},
	} {
		got, err := s.Update(ctx, PrincipalTimezoneUpdate{Timezone: tc.zone, TimezoneSource: tc.source, EnableAuto: tc.enable})
		if err != nil || got.Timezone != tc.wantZone || got.TimezoneSource != tc.wantSource || got.Name != initial.Name || got.Language != initial.Language {
			t.Fatalf("update %+v: %+v %v", tc, got, err)
		}
		// A new service reads persisted values, not a process-local cache.
		reopened, err := NewPrincipalService(db).Get(ctx)
		if err != nil || reopened != got {
			t.Fatalf("persisted: %+v %v", reopened, err)
		}
	}
	before, _ := s.Get(ctx)
	for _, p := range []PrincipalTimezoneUpdate{
		{Timezone: "", TimezoneSource: "manual"},
		{Timezone: "Local", TimezoneSource: "manual"},
		{Timezone: "Mars/Olympus", TimezoneSource: "manual"},
		{Timezone: "UTC", TimezoneSource: "other"},
		{Timezone: "UTC", TimezoneSource: "manual", EnableAuto: true},
	} {
		if _, err := s.Update(ctx, p); !errors.Is(err, ErrInvalidPrincipalTimezone) {
			t.Fatalf("accepted %+v: %v", p, err)
		}
	}
	after, _ := s.Get(ctx)
	if after != before {
		t.Fatalf("invalid updates changed profile: %+v", after)
	}
}

func TestConcurrentAutomaticReportsCannotOverwriteManualChoice(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name) VALUES('local','Example')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 12)
	for i := 0; i < cap(done); i++ {
		source, zone := "auto", "Asia/Tokyo"
		if i == 5 {
			source, zone = "manual", "America/Lima"
		}
		safego.Go("principal-update-test", nil, func() {
			_, err := NewPrincipalService(db).Update(ctx, PrincipalTimezoneUpdate{Timezone: zone, TimezoneSource: source})
			done <- err
		})
	}
	for i := 0; i < cap(done); i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	p, err := NewPrincipalService(db).Get(ctx)
	if err != nil || p.Timezone != "America/Lima" || p.TimezoneSource != "manual" {
		t.Fatalf("manual preference overwritten: %+v %v", p, err)
	}
}
