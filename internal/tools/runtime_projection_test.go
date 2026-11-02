package tools

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/envsnap"
)

func TestProgressFraction(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   float64
	}{
		{"25%", .25}, {"12.5%", .125}, {"done 3/12 items", .25},
		{"epoch 2/8", .25}, {"iter 2 of 8", .25}, {"iteration 2 of 8", .25},
		{"25%|██▌       | 3/12 [00:02<00:06]", .25},
		{"[####------------]", .25}, {"[==>         ]", .25}, {"[██░░░░░░]", .25},
		{"\x1b[32mepoch 2/8\x1b[0m", .25},
		{"10%\r20%\r25%", .25}, {"100%\nepoch 1/4", .25},
		{"100%", 1}, {"0%", 0}, {"0/8", 0}, {"3/0", 0}, {"9/8", 0},
		{"100%\nepoch 0/8", 0}, {"101%", 0}, {"-25%", 0},
		{"working quietly", 0}, {"/tmp/3/12/file", 0}, {"2026/11/02", 0},
		{"[........]", 0}, {"[========]", 0},
		{"25%\n" + strings.Repeat("x", projectionScanBytes), 0},
		{strings.Repeat("x", projectionScanBytes) + "\n25%", .25},
	} {
		t.Run(tc.output[:min(len(tc.output), 40)], func(t *testing.T) {
			if got := progressFraction(tc.output); math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("fraction = %g, want %g", got, tc.want)
			}
		})
	}
}

func projectionManager(t *testing.T, now time.Time) *SessionManager {
	t.Helper()
	root := t.TempDir()
	for path, data := range map[string]string{
		"proc/meminfo": "MemTotal: 8388608 kB\n", "sys/devices/system/cpu/online": "0-7\n",
		"proc/self/cgroup": "0::/job\n", "sys/fs/cgroup/job/cpu.max": "100000 100000\n",
		"sys/fs/cgroup/job/memory.max": "2147483648\n",
	} {
		p := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := NewSessionManager(TerminalDeps{Resources: &envsnap.ResourceReader{ProcRoot: filepath.Join(root, "proc"), SysRoot: filepath.Join(root, "sys")}})
	m.now = func() time.Time { return now }
	return m
}

func TestRuntimeProjectionThresholdBudgetAndScope(t *testing.T) {
	start := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name     string
		elapsed  time.Duration
		budget   time.Duration
		disabled bool
		finished bool
		want     string
	}{
		{name: "three minute boundary", elapsed: 3 * time.Minute},
		{name: "ten percent boundary", elapsed: 6 * time.Minute, budget: time.Hour},
		{name: "unknown", elapsed: 4 * time.Minute, want: "remaining budget unknown"},
		{name: "explicit", elapsed: 7 * time.Minute, budget: time.Hour, want: "remaining budget 53m0s"},
		{name: "finished", elapsed: 7 * time.Minute, finished: true},
		{name: "disabled", elapsed: 7 * time.Minute, disabled: true},
		{name: "expired", elapsed: 7 * time.Minute, budget: 6 * time.Minute, want: "remaining budget 0s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(RuntimeProjectionSwitch, "")
			if tc.disabled {
				t.Setenv(RuntimeProjectionSwitch, "off")
			}
			m := projectionManager(t, start.Add(tc.elapsed))
			s := &Session{Started: start, exited: tc.finished}
			ctx := WithRuntimeBudget(context.Background(), start, tc.budget)
			got := m.runtimeProjection(ctx, s)
			if tc.want == "" {
				if got != "" {
					t.Fatal(got)
				}
				return
			}
			for _, want := range []string{tc.want, "effective CPUs 1; memory 2GiB", "time a small sample", "elapsed "} {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q: %s", want, got)
				}
			}
			if strings.Contains(got, "estimate") || strings.Count(got, "\n") != 1 {
				t.Fatal(got)
			}
			if got := m.runtimeProjection(context.Background(), s); got != "" {
				t.Fatalf("assistant advice: %s", got)
			}
		})
	}
}

func TestRuntimeProjectionPollsAndOffsets(t *testing.T) {
	start := time.Unix(1700000000, 0)
	m := projectionManager(t, start.Add(10*time.Minute))
	s := &Session{ID: "sess-projection", Started: start, done: make(chan struct{})}
	s.appendOut([]byte("epoch 1/4\n"))
	m.sessions[s.ID] = s
	m.order = append(m.order, s.ID)
	ioTool := &ExecIOTool{Sessions: m}
	// The command started after the run: remaining time is measured from the
	// run start, while the completion estimate uses the command's own elapsed.
	ctx := WithRuntimeBudget(context.Background(), start.Add(-20*time.Minute), 40*time.Minute)
	for _, action := range []string{"read", "wait_for", "timeout"} {
		args := map[string]any{"action": action, "session": string(s.ID), "offset": s.full.Len(), "timeout_s": .001}
		if action == "wait_for" {
			args["offset"], args["pattern"] = 0, "epoch"
		}
		if action == "timeout" {
			args["action"], args["pattern"] = "wait_for", "never"
		}
		raw, _ := json.Marshal(args)
		res, err := ioTool.Invoke(ctx, Invocation{Arguments: raw})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"next_offset", "remaining budget 10m0s", "linear estimate 25.0%: finish in 30m0s", "projected to finish after the time limit"} {
			if !strings.Contains(res.Output, want) {
				t.Fatalf("%s missing %q: %+v", action, want, res)
			}
		}
		if strings.Count(res.Output, "runtime:") != 1 || len(res.Output) > 1000 {
			t.Fatal(res.Output)
		}
	}
	// A fitting estimate has no over-budget flag; unknown budgets never imply
	// a deadline. Completion suppresses projection on later reads.
	for _, budget := range []time.Duration{time.Hour, 0} {
		got := m.runtimeProjection(WithRuntimeBudget(context.Background(), start, budget), s)
		if !strings.Contains(got, "linear estimate") || strings.Contains(got, "after the time limit") {
			t.Fatal(got)
		}
	}
	s.markDone(nil)
	if got := m.runtimeProjection(ctx, s); got != "" {
		t.Fatal(got)
	}
}

func TestRuntimeProjectionInitialYield(t *testing.T) {
	tool := execTestTool(t)
	defer closeSessions(tool.Sessions)
	start := time.Unix(1700000000, 0)
	clockCalls := 0
	tool.Sessions.now = func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return start
		}
		return start.Add(4 * time.Minute)
	}
	ctx := WithRuntimeBudget(context.Background(), start, 20*time.Minute)
	raw := json.RawMessage(`{"command":"sleep 5","yield_ms":250}`)
	res, err := tool.Invoke(ctx, Invocation{Arguments: raw})
	if err != nil || !strings.Contains(res.Output, "runtime: elapsed 4m0s; remaining budget 16m0s") {
		t.Fatalf("yield: %+v %v", res, err)
	}
}

func TestProjectionSpanBound(t *testing.T) {
	if got := projectionSpan(1e30); got != ">365d" {
		t.Fatal(got)
	}
}
