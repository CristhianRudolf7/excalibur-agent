package automations

import (
	"context"
	"errors"
	"fmt"
	"github.com/CristhianRudolf7/excalibur-agent/internal/agenda"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store/migrations"
)

type agentFunc func(context.Context, Automation, Step, map[string]string) (string, error)

func (f agentFunc) RunAgentStep(c context.Context, a Automation, s Step, p map[string]string) (string, error) {
	return f(c, a, s, p)
}

type toolFunc func(context.Context, string, map[string]any) (string, error)

func (f toolFunc) InvokeTool(c context.Context, n string, a map[string]any) (string, error) {
	return f(c, n, a)
}

type notifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *notifier) Notify(_ context.Context, s string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, s)
	return nil
}
func (n *notifier) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string{}, n.messages...)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

var bg = context.Background()

func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func spec() Spec {
	return Spec{Name: "Morning", Schedule: Schedule{Kind: ScheduleInterval, EveryMinutes: 5}, Steps: []Step{{ID: "s1", Type: StepAgent, Title: "Read", Instruction: "Read"}}, Edges: []Edge{}}
}
func manager(t *testing.T, cfg Config) *Manager {
	t.Helper()
	if cfg.WorkDir == "" {
		cfg.WorkDir = t.TempDir()
	}
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	return m
}
func create(t *testing.T, m *Manager, s Spec) Automation {
	t.Helper()
	a, err := m.Create(bg, s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func runFinished(t *testing.T, m *Manager, id string) Run {
	t.Helper()
	run, err := m.RunNow(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); _, ok := m.running[id]; return !ok })
	runs, err := m.Runs(bg, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Fatalf("runs: %+v", runs)
	}
	return runs[0]
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{"empty name", func(s *Spec) { s.Name = " " }, "name"},
		{"no steps", func(s *Spec) { s.Steps = nil }, "1 to 30"},
		{"too many", func(s *Spec) { s.Steps = make([]Step, 31) }, "1 to 30"},
		{"duplicate", func(s *Spec) { s.Steps = append(s.Steps, s.Steps[0]) }, "duplicate"},
		{"empty id", func(s *Spec) { s.Steps[0].ID = "" }, "id"},
		{"unknown type", func(s *Spec) { s.Steps[0].Type = "bad" }, "step type"},
		{"unknown edge", func(s *Spec) { s.Edges = []Edge{{From: "s1", To: "bad"}} }, "unknown"},
		{"unknown from", func(s *Spec) { s.Edges = []Edge{{From: "bad", To: "s1"}} }, "unknown"},
		{"cycle", func(s *Spec) { s.Edges = []Edge{{From: "s1", To: "s1"}} }, "cycle"},
		{"unreachable", func(s *Spec) { s.Steps = append(s.Steps, Step{ID: "other", Type: StepNotify}) }, "reachable"},
		{"bad script", func(s *Spec) { s.Steps[0].Type = StepScript; s.Steps[0].Language = "ruby" }, "language"},
		{"missing tool", func(s *Spec) { s.Steps[0].Type = StepTool }, "tool name"},
		{"branch missing", func(s *Spec) { s.Steps[0].Type = StepCondition }, "yes and one no"},
		{"multiple outgoing", func(s *Spec) { s.Edges = []Edge{{From: "s1", To: "s1"}, {From: "s1", To: "s1"}} }, "outgoing"},
		{"label on normal", func(s *Spec) { s.Edges = []Edge{{From: "s1", To: "s1", Label: "yes"}} }, "condition edges"},
		{"interval", func(s *Spec) { s.Schedule.EveryMinutes = 4 }, "at least 5"},
		{"timezone", func(s *Spec) { s.Schedule.Timezone = "not/a/zone" }, "time zone"},
		{"kind", func(s *Spec) { s.Schedule.Kind = "event" }, "schedule kind"},
		{"once", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleOnce} }, "at"},
		{"daily time", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleDaily, Time: "24:00"} }, "HH:MM"},
		{"short time", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleDaily, Time: "2:30"} }, "HH:MM"},
		{"weekly absent", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleWeekly, Time: "12:00"} }, "weekdays"},
		{"weekly range", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleWeekly, Time: "12:00", Weekdays: []int{7}} }, "0..6"},
		{"monthly", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleMonthly, Time: "12:00", DayOfMonth: 32} }, "1..31"},
		{"cron", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleCron, Cron: "65 * * * *"} }, "cron"},
		{"cron seconds", func(s *Spec) { s.Schedule = Schedule{Kind: ScheduleCron, Cron: "0 * * * * *"} }, "5 fields"},
		{"non json args", func(s *Spec) { s.Steps[0].Args = map[string]any{"bad": make(chan int)} }, "JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := spec()
			tt.edit(&s)
			err := ValidateSpec(s)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
	if err := ValidateSpec(spec()); err != nil {
		t.Fatal(err)
	}
}
func TestNextRun(t *testing.T) {
	at := instant("2026-01-02T12:00:00Z")
	cases := []struct {
		name        string
		s           Schedule
		after, want string
	}{
		{"once", Schedule{Kind: ScheduleOnce, At: &at}, "2026-01-01T00:00:00Z", "2026-01-02T12:00:00Z"},
		{"once elapsed", Schedule{Kind: ScheduleOnce, At: &at}, "2026-01-02T12:00:00Z", ""},
		{"interval", Schedule{Kind: ScheduleInterval, EveryMinutes: 5}, "2026-01-01T00:00:00Z", "2026-01-01T00:05:00Z"},
		{"daily Lima", Schedule{Kind: ScheduleDaily, Timezone: "America/Lima", Time: "08:30"}, "2026-01-01T00:00:00Z", "2026-01-01T13:30:00Z"},
		{"daily strict", Schedule{Kind: ScheduleDaily, Time: "08:30"}, "2026-01-01T08:30:00Z", "2026-01-02T08:30:00Z"},
		{"weekly", Schedule{Kind: ScheduleWeekly, Timezone: "Asia/Tokyo", Time: "09:00", Weekdays: []int{1}}, "2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z"},
		{"monthly clamp", Schedule{Kind: ScheduleMonthly, Time: "12:00", DayOfMonth: 31}, "2026-02-01T00:00:00Z", "2026-02-28T12:00:00Z"},
		{"monthly leap", Schedule{Kind: ScheduleMonthly, Time: "12:00", DayOfMonth: 31}, "2028-02-01T00:00:00Z", "2028-02-29T12:00:00Z"},
		{"cron", Schedule{Kind: ScheduleCron, Timezone: "America/Lima", Cron: "30 8 * * 1"}, "2026-01-01T00:00:00Z", "2026-01-05T13:30:00Z"},
		{"DST gap", Schedule{Kind: ScheduleDaily, Timezone: "America/New_York", Time: "02:30"}, "2026-03-08T05:00:00Z", "2026-03-08T07:00:00Z"},
		{"DST gap consumed", Schedule{Kind: ScheduleDaily, Timezone: "America/New_York", Time: "02:30"}, "2026-03-08T07:00:00Z", "2026-03-09T06:30:00Z"},
		{"DST overlap first", Schedule{Kind: ScheduleDaily, Timezone: "America/New_York", Time: "01:30"}, "2026-11-01T04:00:00Z", "2026-11-01T05:30:00Z"},
		{"DST overlap once", Schedule{Kind: ScheduleDaily, Timezone: "America/New_York", Time: "01:30"}, "2026-11-01T05:30:00Z", "2026-11-02T06:30:00Z"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			next, err := NextRun(tt.s, instant(tt.after))
			if err != nil {
				t.Fatal(err)
			}
			if tt.want == "" {
				if next != nil {
					t.Fatal(next)
				}
				return
			}
			if next == nil || !next.Equal(instant(tt.want)) || next.Location() != time.UTC {
				t.Fatalf("got %v, want %s", next, tt.want)
			}
		})
	}
}

func TestGraphBranchesTemplatesAndEvents(t *testing.T) {
	for _, yes := range []bool{true, false} {
		t.Run(fmt.Sprint(yes), func(t *testing.T) {
			n := &notifier{}
			var toolArgs map[string]any
			m := manager(t, Config{Notifier: n, Language: "en", AgentRunner: agentFunc(func(_ context.Context, _ Automation, s Step, p map[string]string) (string, error) {
				if s.ID == "s1" {
					return fmt.Sprintf(`{"ok":%t}`, yes), nil
				}
				return s.Instruction, nil
			}), ToolInvoker: toolFunc(func(_ context.Context, _ string, args map[string]any) (string, error) {
				toolArgs = clone(args)
				return "tool output", nil
			})})
			events := make(chan Event, 100)
			m.Subscribe(func(e Event) { _, _, _ = m.Get(bg, e.ID); events <- e })
			s := spec()
			s.Steps = []Step{{ID: "s1", Type: StepAgent}, {ID: "c", Type: StepCondition, Expression: "steps.s1.json.ok == true"}, {ID: "yes", Type: StepTool, Tool: "echo", Args: map[string]any{"nested": []any{map[string]any{"v": "{{steps.s1.output}}"}}}}, {ID: "no", Type: StepAgent, Instruction: "input={{steps.s1.output}}"}, {ID: "end", Type: StepNotify, Message: "done {{steps.c.output}}"}}
			s.Edges = []Edge{{From: "s1", To: "c"}, {From: "c", To: "yes", Label: "yes"}, {From: "c", To: "no", Label: "no"}, {From: "yes", To: "end"}, {From: "no", To: "end"}}
			a := create(t, m, s)
			run := runFinished(t, m, a.ID)
			if run.Status != RunSucceeded {
				t.Fatalf("%+v", run)
			}
			if run.Steps[2].Status != map[bool]StepRunStatus{true: StepSucceeded, false: StepSkipped}[yes] || run.Steps[3].Status != map[bool]StepRunStatus{true: StepSkipped, false: StepSucceeded}[yes] {
				t.Fatalf("branches %+v", run.Steps)
			}
			if yes && toolArgs["nested"].([]any)[0].(map[string]any)["v"] != `{"ok":true}` {
				t.Fatal(toolArgs)
			}
			if !yes && run.Steps[3].OutputPreview != `input={"ok":false}` {
				t.Fatal(run.Steps[3])
			}
			if run.Summary != fmt.Sprint("done ", yes) {
				t.Fatal(run.Summary)
			}
			// The run ended with a successful notify step, so no completion echo follows.
			waitFor(t, func() bool { return len(n.all()) >= 1 })
			time.Sleep(50 * time.Millisecond)
			if got := n.all(); len(got) != 1 || strings.HasPrefix(got[0], "✅ Automation") {
				t.Fatal(got)
			}
			var received []Event
			waitFor(t, func() bool {
				for {
					select {
					case e := <-events:
						received = append(received, e)
					default:
						return len(received) > 1 && received[len(received)-1].Kind == EventUpdated
					}
				}
			})
			if received[0].Kind != EventUpdated || received[1].Kind != EventRun {
				t.Fatalf("events %+v", received)
			}
			previous := map[string]StepRunStatus{}
			terminals := 0
			for _, e := range received {
				if e.Kind != EventRun {
					continue
				}
				if e.Run.Status != RunRunning {
					terminals++
				}
				for _, step := range e.Run.Steps {
					old := previous[step.StepID]
					if old == StepSucceeded || old == StepFailed || old == StepSkipped {
						if old != step.Status {
							t.Fatalf("invalid transition %s -> %s", old, step.Status)
						}
					}
					previous[step.StepID] = step.Status
				}
			}
			if terminals != 1 {
				t.Fatalf("terminal events=%d", terminals)
			}
		})
	}
}

func TestFailuresTimeoutAndPause(t *testing.T) {
	for _, mode := range []string{"error", "timeout", "panic"} {
		t.Run(mode, func(t *testing.T) {
			m := manager(t, Config{Notifier: &notifier{}, AgentRunner: agentFunc(func(ctx context.Context, _ Automation, _ Step, _ map[string]string) (string, error) {
				switch mode {
				case "timeout":
					<-ctx.Done()
					return "", ctx.Err()
				case "panic":
					panic("broken")
				}
				return "", errors.New("broken")
			})})
			m.runDuration = 15 * time.Millisecond
			a := create(t, m, spec())
			for i := 1; i <= 3; i++ {
				r := runFinished(t, m, a.ID)
				if r.Status != RunFailed {
					t.Fatal(r)
				}
				if mode == "timeout" && !strings.Contains(r.Error, "deadline exceeded") {
					t.Fatal(r.Error)
				}
			}
			a, _, _ = m.Get(bg, a.ID)
			if a.Status != StatusPaused || a.PausedReason != "3 consecutive failures" || a.ConsecutiveFailures != 3 || a.NextRunAt != nil {
				t.Fatal(a)
			}
			a, err := m.Resume(bg, a.ID)
			if err != nil || a.ConsecutiveFailures != 0 || a.NextRunAt == nil || a.PausedReason != "" {
				t.Fatalf("resume %+v %v", a, err)
			}
		})
	}
}
func TestCancellation(t *testing.T) {
	for _, action := range []string{"delete", "pause", "stop"} {
		t.Run(action, func(t *testing.T) {
			entered := make(chan struct{})
			m := manager(t, Config{Notifier: &notifier{}, AgentRunner: agentFunc(func(ctx context.Context, _ Automation, _ Step, _ map[string]string) (string, error) {
				close(entered)
				<-ctx.Done()
				return "", ctx.Err()
			})})
			events := make(chan Event, 100)
			m.Subscribe(func(e Event) { events <- e })
			a := create(t, m, spec())
			run, err := m.RunNow(bg, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			<-entered
			if _, err = m.RunNow(bg, a.ID); !errors.Is(err, ErrAlreadyRunning) {
				t.Fatal(err)
			}
			err = nil
			switch action {
			case "delete":
				err = m.Delete(bg, a.ID)
			case "pause":
				_, err = m.Pause(bg, a.ID, "")
			case "stop":
				m.Stop()
			}
			if err != nil {
				t.Fatal(err)
			}
			waitFor(t, func() bool {
				for {
					select {
					case e := <-events:
						if e.Kind == EventRun && e.Run.ID == run.ID && e.Run.Status == RunCancelled {
							return true
						}
					default:
						return false
					}
				}
			})
			if action == "delete" {
				if _, _, err = m.Get(bg, a.ID); !errors.Is(err, ErrNotFound) {
					t.Fatal(err)
				}
			} else {
				a, _, _ = m.Get(bg, a.ID)
				if action == "pause" && (a.Status != StatusPaused || a.PausedReason != "user") {
					t.Fatal(a)
				}
			}
		})
	}
}

func TestVersionsAndIsolation(t *testing.T) {
	m := manager(t, Config{})
	s := spec()
	a := create(t, m, s)
	s.Steps[0].Instruction = "mutated"
	paused, err := m.Pause(bg, a.ID, "agent")
	if err != nil || paused.PausedReason != "agent" {
		t.Fatal(err)
	}
	s.Name = "Updated"
	updated, err := m.Update(bg, a.ID, s, "changed")
	if err != nil || updated.Version != 2 || updated.Status != StatusPaused || updated.NextRunAt != nil {
		t.Fatalf("%+v %v", updated, err)
	}
	restored, err := m.RestoreVersion(bg, a.ID, 1)
	if err != nil || restored.Version != 3 || restored.Name != "Morning" || restored.Steps[0].Instruction != "Read" {
		t.Fatalf("%+v %v", restored, err)
	}
	restored.Steps[0].Instruction = "changed returned copy"
	got, versions, err := m.Get(bg, a.ID)
	if err != nil || got.Steps[0].Instruction != "Read" || len(versions) != 3 || versions[2].Note != "restored v1" {
		t.Fatalf("%+v %+v %v", got, versions, err)
	}
	if _, err = m.RestoreVersion(bg, a.ID, 999); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatal(err)
	}
	b := spec()
	b.Name = "A"
	create(t, m, b)
	list, err := m.List(bg)
	if err != nil || list[0].Name != "A" {
		t.Fatal(list, err)
	}
}

func TestScripts(t *testing.T) {
	for _, language := range []string{"python", "bash", "node"} {
		t.Run(language, func(t *testing.T) {
			binary := map[string]string{"python": "python3", "bash": "bash", "node": "node"}[language]
			binary, _, err := scriptInvocation(binary)
			if err != nil {
				t.Skip(err)
			}
			if _, err := exec.LookPath(binary); err != nil {
				t.Skip(err)
			}
			dir := t.TempDir()
			m := manager(t, Config{WorkDir: dir, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) { return "seed", nil })})
			code := map[string]string{"python": `import os,json
assert json.loads(os.environ['AUTOMATION_INPUT'])['s1']=='seed'
open('persisted','a').write('x')
print('{{steps.s1.output}}')`, "bash": `test "$AUTOMATION_INPUT" = '{"s1":"seed"}' || exit 1
printf x >> persisted
printf '{{steps.s1.output}}'`, "node": `const fs=require('fs');if(JSON.parse(process.env.AUTOMATION_INPUT).s1!=='seed')process.exit(1);fs.appendFileSync('persisted','x');console.log('{{steps.s1.output}}')`}[language]
			s := spec()
			s.Steps = append(s.Steps, Step{ID: "script", Type: StepScript, Language: language, Code: code})
			s.Edges = []Edge{{From: "s1", To: "script"}}
			a := create(t, m, s)
			for i := 0; i < 2; i++ {
				run := runFinished(t, m, a.ID)
				if run.Status != RunSucceeded || strings.TrimSpace(run.Steps[1].OutputPreview) != "seed" {
					t.Fatalf("%+v", run)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, a.ID, "persisted"))
			if err != nil || string(data) != "xx" {
				t.Fatalf("%q %v", data, err)
			}
		})
	}
}
func TestScriptFailureAndTimeout(t *testing.T) {
	for _, code := range []string{"echo failure >&2; exit 4", "sleep 30 & wait"} {
		t.Run(code, func(t *testing.T) {
			m := manager(t, Config{})
			m.runDuration = 30 * time.Millisecond
			if strings.Contains(code, "exit") {
				m.runDuration = 10 * time.Second
			}
			s := spec()
			s.Steps = []Step{{ID: "script", Type: StepScript, Language: "bash", Code: code}}
			a := create(t, m, s)
			r := runFinished(t, m, a.ID)
			if r.Status != RunFailed {
				t.Fatal(r)
			}
			if strings.Contains(code, "exit") && !strings.Contains(r.Error, "failure") {
				t.Fatal(r.Error)
			}
		})
	}
}

func openDB(t *testing.T, path string) *store.DB {
	t.Helper()
	opts := store.DefaultOptions()
	opts.BlobDir = filepath.Join(t.TempDir(), "blobs")
	db, err := store.Open(bg, path, opts, migrations.Main())
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestPersistenceAndCatchUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auto.db")
	db := openDB(t, path)
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	n := &notifier{}
	cfg := Config{DB: db, Now: clock.Now, Notifier: n, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) { return "ok", nil })}
	m := manager(t, cfg)
	a := create(t, m, spec())
	s := spec()
	s.Name = "New"
	if _, err := m.Update(bg, a.ID, s, "edit"); err != nil {
		t.Fatal(err)
	}
	run := runFinished(t, m, a.ID)
	m.Stop()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	clock.Add(24 * time.Hour)
	db = openDB(t, path)
	defer db.Close()
	cfg.DB = db
	m2 := manager(t, cfg)
	restored, versions, err := m2.Get(bg, a.ID)
	if err != nil || restored.Version != 2 || len(versions) != 2 || restored.LastRun.ID != run.ID {
		t.Fatalf("%+v %+v %v", restored, versions, err)
	}
	if err = m2.Start(bg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { r, _ := m2.Runs(bg, a.ID, 20); return len(r) == 2 && r[0].Status == RunSucceeded })
	runs, _ := m2.Runs(bg, a.ID, 20)
	if runs[0].Trigger != TriggerCatchUp {
		t.Fatal(runs)
	}
	restored, _, _ = m2.Get(bg, a.ID)
	if restored.NextRunAt == nil || !restored.NextRunAt.Equal(clock.Now().Add(5*time.Minute)) {
		t.Fatal(restored.NextRunAt)
	}
	m2.Stop()
	runs, _ = m2.Runs(bg, a.ID, 20)
	if len(runs) != 2 {
		t.Fatal(runs)
	}
	if err = m2.Delete(bg, a.ID); err != nil {
		t.Fatal(err)
	}
	err = db.Read(bg, func(ctx context.Context, q store.Queryer) error {
		for _, table := range []string{"automations", "automation_versions", "automation_runs"} {
			var count int
			if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return fmt.Errorf("%s retains %d rows after deletion", table, count)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m3 := manager(t, cfg)
	defer m3.Stop()
	list, err := m3.List(bg)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}

func TestOnceCatchUpAndRetention(t *testing.T) {
	var calls atomic.Int32
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	m := manager(t, Config{Now: clock.Now, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) {
		if calls.Add(1) == 1 {
			return strings.Repeat("界", 2500), nil
		}
		return "ok", nil
	})})
	s := spec()
	at := clock.Now().Add(-time.Hour)
	s.Schedule = Schedule{Kind: ScheduleOnce, At: &at}
	a := create(t, m, s)
	if err := m.Start(bg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { a, _, _ := m.Get(bg, a.ID); return a.LastRun != nil && a.LastRun.Status == RunSucceeded })
	got, _, _ := m.Get(bg, a.ID)
	if got.Status != StatusActive || got.NextRunAt != nil || got.LastRun.Trigger != TriggerCatchUp || len([]rune(got.LastRun.Summary)) != 300 || len([]rune(got.LastRun.Steps[0].OutputPreview)) != 2000 {
		t.Fatal(got)
	}
	for i := 0; i < 204; i++ {
		runFinished(t, m, a.ID)
	}
	m.mu.Lock()
	retained := len(m.records[a.ID].Runs)
	m.mu.Unlock()
	if retained != 200 {
		t.Fatal(retained)
	}
	runs, _ := m.Runs(bg, a.ID, 1000)
	if len(runs) != 100 {
		t.Fatal(len(runs))
	}
	runs, _ = m.Runs(bg, a.ID, 0)
	if len(runs) != 20 {
		t.Fatal(len(runs))
	}
}
func TestUnknownIDs(t *testing.T) {
	m := manager(t, Config{})
	calls := []func() error{func() error { _, _, e := m.Get(bg, "missing"); return e }, func() error { _, e := m.Runs(bg, "missing", 0); return e }, func() error { _, e := m.RunNow(bg, "missing"); return e }, func() error { _, e := m.Pause(bg, "missing", ""); return e }, func() error { _, e := m.Resume(bg, "missing"); return e }, func() error { return m.Delete(bg, "missing") }, func() error { _, e := m.Update(bg, "missing", spec(), ""); return e }, func() error { _, e := m.RestoreVersion(bg, "missing", 1); return e }}
	for _, call := range calls {
		if err := call(); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
}
func TestNoDoubleCompletionNotify(t *testing.T) {
	n := &notifier{}
	m := manager(t, Config{Notifier: n, Language: "en"})
	s := spec()
	s.Steps = []Step{{ID: "notify", Type: StepNotify, Message: "hello"}}
	a := create(t, m, s)
	r := runFinished(t, m, a.ID)
	if r.Status != RunSucceeded {
		t.Fatal(r)
	}
	waitFor(t, func() bool { return len(n.all()) >= 1 })
	time.Sleep(50 * time.Millisecond)
	if got := n.all(); !reflect.DeepEqual(got, []string{"hello"}) {
		t.Fatal(got)
	}
	// A successful run ending in notify never posts a completion echo.
	n.mu.Lock()
	n.messages = nil
	n.mu.Unlock()
	m.finish(a, r, StepNotify, "✅ Automation «Morning»: hello")
	if !reflect.DeepEqual(n.all(), []string{}) {
		t.Fatal(n.all())
	}
}

func TestSchedulerRearmsAndFinishedOnce(t *testing.T) {
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	m := manager(t, Config{Now: clock.Now, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) { return "ok", nil })})
	if err := m.Start(bg); err != nil {
		t.Fatal(err)
	}
	s := spec()
	at := clock.Now().Add(time.Hour)
	s.Schedule = Schedule{Kind: ScheduleOnce, At: &at}
	a := create(t, m, s)
	// A manual execution of a once schedule completes it, even before its slot.
	runFinished(t, m, a.ID)
	got, _, _ := m.Get(bg, a.ID)
	if got.NextRunAt != nil || got.Status != StatusActive {
		t.Fatal(got)
	}
	b := create(t, m, spec())
	clock.Add(6 * time.Minute)
	signal(m.wake)
	waitFor(t, func() bool {
		got, _, _ := m.Get(bg, b.ID)
		return got.LastRun != nil && got.LastRun.Status == RunSucceeded
	})
	got, _, _ = m.Get(bg, b.ID)
	if got.LastRun.Trigger != TriggerSchedule || got.NextRunAt == nil || !got.NextRunAt.Equal(clock.Now().Add(5*time.Minute)) {
		t.Fatal(got)
	}
	if _, err := m.Pause(bg, b.ID, ""); err != nil {
		t.Fatal(err)
	}
	clock.Add(time.Hour)
	signal(m.wake)
	if _, err := m.Resume(bg, b.ID); err != nil {
		t.Fatal(err)
	}
	clock.Add(6 * time.Minute)
	signal(m.wake)
	waitFor(t, func() bool { runs, _ := m.Runs(bg, b.ID, 10); return len(runs) == 2 && runs[0].Status == RunSucceeded })
}

func TestInterruptedRunRecovery(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "restart.db"))
	defer db.Close()
	m := manager(t, Config{DB: db})
	a := create(t, m, spec())
	m.mu.Lock()
	r := clone(m.records[a.ID])
	run := Run{ID: "interrupted", AutomationID: a.ID, Version: 1, Trigger: TriggerManual, Status: RunRunning, StartedAt: m.now(), Steps: []StepRun{{StepID: "s1", Status: StepRunning}}}
	r.Runs = []Run{run}
	r.Automation.LastRun = &run
	err := m.persistLocked(bg, r)
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	m.Stop()
	m2 := manager(t, Config{DB: db})
	defer m2.Stop()
	got, _, err := m2.Get(bg, a.ID)
	if err != nil || got.LastRun.Status != RunCancelled || got.LastRun.Steps[0].Status != StepFailed || got.LastRun.FinishedAt == nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestSuccessResetsFailuresAndLocalizedSummary(t *testing.T) {
	var calls atomic.Int32
	n := &notifier{}
	m := manager(t, Config{Notifier: n, Language: "es", AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) {
		if calls.Add(1) == 1 {
			return "", errors.New("broken")
		}
		return "done", nil
	})})
	a := create(t, m, spec())
	runFinished(t, m, a.ID)
	runFinished(t, m, a.ID)
	waitFor(t, func() bool { return len(n.all()) == 2 })
	got, _, _ := m.Get(bg, a.ID)
	if got.ConsecutiveFailures != 0 || got.Status != StatusActive {
		t.Fatal(got)
	}
	messages := n.all()
	hasFailure, hasSuccess := false, false
	for _, text := range messages {
		hasFailure = hasFailure || strings.Contains(text, "falló: broken")
		hasSuccess = hasSuccess || strings.Contains(text, "✅ Automatización «Morning»: done")
	}
	if !hasFailure || !hasSuccess {
		t.Fatal(messages)
	}
}

func TestSQLRunOrderWithinSecond(t *testing.T) {
	db := openDB(t, filepath.Join(t.TempDir(), "order.db"))
	defer db.Close()
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	cfg := Config{DB: db, Now: clock.Now, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) { return "done", nil })}
	m := manager(t, cfg)
	a := create(t, m, spec())
	runFinished(t, m, a.ID)
	clock.Add(100 * time.Millisecond)
	newest := runFinished(t, m, a.ID)
	m.Stop()
	restored := manager(t, cfg)
	defer restored.Stop()
	runs, err := restored.Runs(bg, a.ID, 20)
	if err != nil || len(runs) != 2 || runs[0].ID != newest.ID {
		t.Fatalf("%+v %v", runs, err)
	}
}

func TestResumePendingOnceAndUpdatePastOnce(t *testing.T) {
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	m := manager(t, Config{Now: clock.Now, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) { return "ok", nil })})
	s := spec()
	at := clock.Now().Add(time.Hour)
	s.Schedule = Schedule{Kind: ScheduleOnce, At: &at}
	a := create(t, m, s)
	if _, err := m.Pause(bg, a.ID, ""); err != nil {
		t.Fatal(err)
	}
	clock.Add(2 * time.Hour)
	resumed, err := m.Resume(bg, a.ID)
	if err != nil || resumed.NextRunAt == nil || !resumed.NextRunAt.Equal(at) {
		t.Fatalf("%+v %v", resumed, err)
	}
	runFinished(t, m, a.ID)
	resumed, err = m.Resume(bg, a.ID)
	if err != nil || resumed.NextRunAt != nil {
		t.Fatalf("%+v %v", resumed, err)
	}
	updated, err := m.Update(bg, a.ID, s, "new once")
	if err != nil || updated.NextRunAt == nil {
		t.Fatalf("%+v %v", updated, err)
	}
}

func TestScriptBackgroundChildrenAreStopped(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	m := manager(t, Config{WorkDir: dir})
	s := spec()
	s.Steps = []Step{{ID: "script", Type: StepScript, Language: "bash", Code: "(sleep 0.4; printf escaped > escaped) & exit 0"}}
	a := create(t, m, s)
	r := runFinished(t, m, a.ID)
	if r.Status != RunFailed {
		t.Fatalf("unclosed child pipes should fail: %+v", r)
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, a.ID, "escaped")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("background script child escaped: %v", err)
	}
}

func TestFailedRunStillPostsCompletionAndStepTimesDiffer(t *testing.T) {
	n := &notifier{}
	clock := &fakeClock{t: instant("2026-01-01T00:00:00Z")}
	m := manager(t, Config{Notifier: n, Language: "en", Now: clock.Now, AgentRunner: agentFunc(func(context.Context, Automation, Step, map[string]string) (string, error) {
		clock.Add(time.Minute)
		return "", fmt.Errorf("boom")
	})})
	s := spec()
	s.Steps = []Step{{ID: "a", Type: StepAgent, Title: "A", Instruction: "x"}}
	s.Edges = nil
	a := create(t, m, s)
	r := runFinished(t, m, a.ID)
	if r.Status != RunFailed {
		t.Fatal(r)
	}
	if st := r.Steps[0]; st.StartedAt == nil || st.FinishedAt == nil || !st.FinishedAt.After(*st.StartedAt) {
		t.Fatalf("step times %+v", st)
	}
	waitFor(t, func() bool { return len(n.all()) == 1 })
	if !strings.HasPrefix(n.all()[0], "⚠️ Automation «") {
		t.Fatal(n.all())
	}
}

func TestNotifyDecodesEscapedNewlines(t *testing.T) {
	n := &notifier{}
	m := manager(t, Config{Notifier: n})
	s := spec()
	s.Steps = []Step{{ID: "n", Type: StepNotify, Title: "N", Message: `line1\nline2`}}
	s.Edges = nil
	a := create(t, m, s)
	runFinished(t, m, a.ID)
	waitFor(t, func() bool { return len(n.all()) >= 1 })
	if n.all()[0] != "line1\nline2" {
		t.Fatalf("%q", n.all()[0])
	}
}

func TestJSONFieldReferences(t *testing.T) {
	prior := map[string]string{"gen": `{"numbers":[24,1,85],"sum":110}` + "\n"}
	if got := render("sum {{steps.gen.output.sum}} nums {{steps.gen.output.numbers}} first {{steps.gen.output.numbers.0}}", prior); got != "sum 110 nums [24,1,85] first 24" {
		t.Fatal(got)
	}
	data := conditionData(prior)
	for expr, want := range map[string]bool{"steps.gen.output.sum > 150": false, "steps.gen.output.sum > 100": true} {
		if err := checkConditionRefs(expr, data); err != nil {
			t.Fatal(err)
		}
		got, err := agenda.EvalCondition(expr, data)
		if err != nil || got != want {
			t.Fatalf("%s = %v %v", expr, got, err)
		}
	}
	if err := checkConditionRefs("steps.gen.output.missing > 1", data); err == nil {
		t.Fatal("missing reference accepted")
	}
	if err := checkConditionRefs(`steps.other.output != ""`, data); err == nil {
		t.Fatal("unknown step accepted")
	}
}

func TestSilentBranchWithNotifyStepsPostsNothing(t *testing.T) {
	n := &notifier{}
	m := manager(t, Config{Notifier: n})
	s := spec()
	s.Steps = []Step{
		{ID: "v", Type: StepScript, Title: "V", Language: "bash", Code: "echo 5"},
		{ID: "c", Type: StepCondition, Title: "C", Expression: "steps.v.output > 10"},
		{ID: "n", Type: StepNotify, Title: "N", Message: "hi"},
		{ID: "q", Type: StepScript, Title: "Q", Language: "bash", Code: "echo quiet"},
	}
	s.Edges = []Edge{{From: "v", To: "c"}, {From: "c", To: "n", Label: "yes"}, {From: "c", To: "q", Label: "no"}}
	a := create(t, m, s)
	if r := runFinished(t, m, a.ID); r.Status != RunSucceeded {
		t.Fatal(r)
	}
	time.Sleep(100 * time.Millisecond)
	if got := n.all(); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestCreateDeduplicatesIdenticalSpec(t *testing.T) {
	m := manager(t, Config{})
	a := create(t, m, spec())
	b := create(t, m, spec())
	if a.ID != b.ID {
		t.Fatalf("duplicate created: %s vs %s", a.ID, b.ID)
	}
	other := spec()
	other.Name = "Different"
	if c := create(t, m, other); c.ID == a.ID {
		t.Fatal("distinct spec merged")
	}
}

func TestCreateDeduplicatesRewordedRepeat(t *testing.T) {
	m := manager(t, Config{})
	a := create(t, m, spec())
	again := spec()
	again.Description = "reworded description"
	if b := create(t, m, again); b.ID != a.ID {
		t.Fatal("reworded repeat created a copy")
	}
}
