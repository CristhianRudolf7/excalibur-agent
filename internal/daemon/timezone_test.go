package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/automations"
	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

func TestProfileZoneFlowsToMessagesAndNextAssistantTurn(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	ctx := context.Background()
	db := principalDB(t, filepath.Join(t.TempDir(), "profile.db"))
	principal, _ := NewPrincipalService(ctx, db)
	_, conversations, err := OpenAgentConversations(ctx, db, "Test", nil)
	if err != nil {
		t.Fatal(err)
	}
	m := master.New(master.Config{}, conversations, nil, nil, nil, nil, nil)
	b := NewDaemonBackend(m, nil, nil)
	b.AttachServices(BackendServices{Principal: principal})
	a := &assistantTurns{backend: b, services: &Services{}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = m.Turns().Stop(ctx)
	})
	for i, zone := range []string{"America/Lima", "Europe/Madrid"} {
		if _, err := b.Passthrough(ctx, api.MethodPrincipalUpdate, json.RawMessage(`{"timezone":"`+zone+`","timezone_source":"manual"}`)); err != nil {
			t.Fatal(err)
		}
		id, err := m.ReceiveDeferred(ctx, "client", zone, "conv", "remind me tomorrow at 8 am", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
			var stored, raw string
			if err := q.QueryRow(ctx, `SELECT user_tz,normalized_json FROM messages WHERE id=?`, string(id)).Scan(&stored, &raw); err != nil {
				return err
			}
			var normalized master.Normalized
			if err := json.Unmarshal([]byte(raw), &normalized); err != nil {
				return err
			}
			if stored != zone || normalized.UserTimezone != zone {
				t.Fatalf("message %d: %s %+v", i, stored, normalized)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		profile := a.snapshot(ctx, "conv", master.Normalized{Text: "Hello"}).AgentProfile
		if !strings.Contains(profile, "timezone: "+zone+";") {
			t.Fatalf("stale assistant profile: %s", profile)
		}
	}
}

func TestProfileTimezoneNeverMovesExistingAutomation(t *testing.T) {
	t.Setenv("EXCALIBUR_PRINCIPAL_ENABLED", "1")
	ctx := context.Background()
	db := principalDB(t, filepath.Join(t.TempDir(), "profile.db"))
	principal, _ := NewPrincipalService(ctx, db)
	manager, err := automations.NewManager(automations.Config{DB: db, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	at := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	original, err := manager.Create(ctx, automations.Spec{Name: "Lima reminder", Schedule: automations.Schedule{Kind: automations.ScheduleOnce, Timezone: "America/Lima", At: &at}, Steps: []automations.Step{{ID: "n", Type: automations.StepNotify, Title: "Notify", Message: "Reminder"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Compare durable records as well as the manager's cached view.
	stored := func() string {
		t.Helper()
		var data, versions string
		if err := db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
			return q.QueryRow(ctx, `SELECT a.data_json, v.spec_json FROM automations a JOIN automation_versions v ON v.automation_id=a.id WHERE a.id=? AND v.version=1`, original.ID).Scan(&data, &versions)
		}); err != nil {
			t.Fatal(err)
		}
		return data + "\n" + versions
	}
	before := stored()
	for _, zone := range []string{"Europe/Madrid", "Asia/Tokyo"} {
		if _, err := principal.Update(ctx, api.PrincipalUpdateParams{Timezone: zone, TimezoneSource: "manual"}); err != nil {
			t.Fatal(err)
		}
		if after := stored(); after != before {
			t.Fatal("profile update rewrote stored automation or version")
		}
		got, _, err := manager.Get(ctx, original.ID)
		if err != nil || got.Schedule.Timezone != "America/Lima" || !got.Schedule.At.Equal(at) || !got.NextRunAt.Equal(*original.NextRunAt) || got.Version != original.Version {
			t.Fatalf("profile update moved schedule: %+v %v", got, err)
		}
	}
}
