package tools

import (
	"fmt"
	"testing"
	"time"
)

func TestSessionTombstonesSurviveCloseAndEviction(t *testing.T) {
	m := NewSessionManager(TerminalDeps{})
	s, _, _ := m.Start("owner", "exit 7", "", false, 250, nil)
	if code := waitSessionExit(t, s); code != 7 {
		t.Fatal(code)
	}
	if err := m.Close(s.ID); err != nil {
		t.Fatal(err)
	}
	assertDead := func(id SessionID, code bool) {
		t.Helper()
		for _, info := range m.ActiveSessions() {
			if info.ID == id {
				if info.Alive || (code && (info.ExitCode == nil || *info.ExitCode != 7)) {
					t.Fatal(info)
				}
				return
			}
		}
		t.Fatalf("no tombstone for %s", id)
	}
	assertDead(s.ID, true)
	// Exercise cache-pressure eviction without starting extra processes.
	for i := 0; i < maxSessionsGlobal; i++ {
		id := SessionID(fmt.Sprintf("sess-fixture-%d", i))
		fixture := &Session{ID: id, Owner: "owner", Started: time.Unix(int64(i), 0), lastUse: time.Unix(int64(i), 0), done: make(chan struct{})}
		fixture.markDone(nil)
		m.sessions[id] = fixture
		m.order = append(m.order, id)
	}
	m.evictIfNeeded("owner")
	assertDead("sess-fixture-0", false)
	t.Setenv("EXCALIBUR_SESSION_AUTHORITY", "0")
	for _, info := range m.ActiveSessions() {
		if info.ID == s.ID {
			t.Fatal("legacy switch includes tombstone")
		}
	}
}
