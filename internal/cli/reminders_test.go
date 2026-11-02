package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

func TestEmbeddedReminderProfiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("EXCALIBUR_HOME", home)
	svc, closeDB, err := embeddedReminders(context.Background(), ProfileEval)
	if err != nil || svc != nil {
		t.Fatalf("eval %v %v", svc, err)
	}
	closeDB()
	if _, err := os.Stat(filepath.Join(home, "state")); !os.IsNotExist(err) {
		t.Fatal("eval opened persistent state")
	}
	emb, err := wireengine.BuildEmbedded(wireengine.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer emb.Close()
	tool, _ := emb.Registry.Get("reminders")
	res, err := tool.Invoke(context.Background(), tools.Invocation{Arguments: json.RawMessage(`{"action":"create","text":"eval only","when":"tomorrow 08:00"}`)})
	if err != nil || res.Status != tools.StatusOK {
		t.Fatalf("memory tool: %+v %v", res, err)
	}
	for _, profile := range []ProfileName{ProfileDefault, ProfileDelegate} {
		svc, closeDB, err = embeddedReminders(context.Background(), profile)
		if err != nil || svc == nil {
			t.Fatalf("%s: %v", profile, err)
		}
		closeDB()
	}
	t.Setenv("EXCALIBUR_PERSISTENT_REMINDERS", "0")
	svc, closeDB, err = embeddedReminders(context.Background(), ProfileDefault)
	if err != nil || svc != nil {
		t.Fatal("off path", err)
	}
	closeDB()
}
