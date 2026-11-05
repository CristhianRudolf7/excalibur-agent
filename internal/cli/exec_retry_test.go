package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// flakyProvider fails the first fails Chat calls with kind, then
// answers with final text. It proves at the exec level which error
// classes the Section 19.5 retry policy actually retries.
type flakyProvider struct {
	mu         sync.Mutex
	fails      int
	kind       providers.ErrorKind
	calls      int
	retryAfter time.Duration
}

func (f *flakyProvider) Name() string { return "flaky-test" }

func (f *flakyProvider) Chat(_ context.Context, req providers.ChatRequest) (providers.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.fails {
		return providers.ChatResponse{}, &providers.ProviderError{
			Kind: f.kind, Provider: f.Name(), Model: req.Model, RetryAfter: f.retryAfter,
			Err: fmt.Errorf("flaky %s failure", f.kind),
		}
	}
	// Explicit finish: trivial runs still end ONLY through `finish`.
	// After the transient retries clear, serve the two-turn finish
	// confirmation (first accepted, second completes).
	finished := f.calls - f.fails
	args, _ := json.Marshal(map[string]string{"summary": "done", "evidence": "no tool work needed; request verified by inspection"})
	id := fmt.Sprintf("f%d", finished)
	if finished > 2 {
		id = fmt.Sprintf("f%d", finished)
	}
	return providers.ChatResponse{
		Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: id, Name: "finish", Arguments: args}}},
	}, nil
}

func (f *flakyProvider) Stream(context.Context, providers.ChatRequest, func(providers.StreamEvent) error) error {
	return nil
}

func (f *flakyProvider) Close() error { return nil }

func readEvalMetrics(t *testing.T, logsDir string) telemetry.Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(logsDir, "excalibur", "metrics.json"))
	if err != nil {
		t.Fatalf("metrics.json: %v", err)
	}
	var snap telemetry.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("metrics.json: %v", err)
	}
	return snap
}

// TestExecRetriesTransientProviderErrors proves the Section 19.5
// retry policy is actually applied in exec: a transient that clears
// completes the run, and the retries are recorded (defect
// w8-bench-fixes/2).
func TestExecRetriesTransientProviderErrors(t *testing.T) {
	fake := &flakyProvider{fails: 2, kind: providers.KindTransient}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "say hi", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed", out.State)
	}
	// Two transient failures retried inside the first model turn (3
	// provider calls), then the two-turn finish confirmation (one more
	// provider call for the second finish).
	if fake.calls != 4 {
		t.Fatalf("provider calls = %d, want 4 (initial + 2 retries + second finish)", fake.calls)
	}
	snap := readEvalMetrics(t, logs)
	if snap.ModelRetries != 2 {
		t.Fatalf("model_retries = %d, want 2", snap.ModelRetries)
	}
	if snap.CallsByRole["executor"] != 2 {
		t.Fatalf("calls_by_role = %v, want two executor calls (finish confirmation round)", snap.CallsByRole)
	}
	if snap.ModelSeconds <= 0 {
		t.Fatalf("model_seconds = %v, want the real call latency", snap.ModelSeconds)
	}
}

// TestExecTLSConfigErrorFailsFast proves a TLS verification failure
// (task image without CA certificates) is a non-retryable
// configuration error in exec: one attempt, immediate honest
// failure, no burned retries (defect w8-bench-fixes/2).
func TestExecTLSConfigErrorFailsFast(t *testing.T) {
	fake := &flakyProvider{fails: 99, kind: providers.KindInput}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "say hi", LogsDir: logs, Profile: ProfileEval,
		Provider: fake,
	})
	if err != nil {
		t.Fatalf("RunExec: %v", err)
	}
	if out.State != types.RunFailed {
		t.Fatalf("state = %q, want failed", out.State)
	}
	if fake.calls != 1 {
		t.Fatalf("provider calls = %d, want 1 (no retry of config errors)", fake.calls)
	}
	snap := readEvalMetrics(t, logs)
	if snap.ModelRetries != 0 {
		t.Fatalf("model_retries = %d, want 0", snap.ModelRetries)
	}
}

// Overload retries belong to one model turn and preserve trial wait totals.
func TestExecRateLimitWaitRecords(t *testing.T) {
	fake := &flakyProvider{fails: 4, kind: providers.KindRateLimited, retryAfter: time.Millisecond}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{Request: "say hi", LogsDir: logs, Profile: ProfileEval, Provider: fake, MaxSteps: 2, TimeoutSecs: 10})
	if err != nil || out.State != types.RunCompleted {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	raw, err := os.ReadFile(filepath.Join(logs, "excalibur", "model_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("model turns=%d, want 2", len(lines))
	}
	var record eval.ModelCall
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatal(err)
	}
	if record.RetryWaitCount != 4 || record.RetryWaitSeconds <= 0 || record.Retries != 4 {
		t.Fatalf("wait record %+v", record)
	}
}

func TestExecRateLimitsExpireWallBudget(t *testing.T) {
	fake := &flakyProvider{fails: 100000, kind: providers.KindRateLimited, retryAfter: time.Millisecond}
	logs := t.TempDir()
	store := journal.NewMemStore()
	out, err := RunExec(context.Background(), ExecOptions{Request: "say hi", LogsDir: logs, Profile: ProfileEval, Provider: fake, TimeoutSecs: 30, JournalStore: store})
	// W12 exposes timeout records while W13 preserves the durable budget pause.
	if err != nil || out.State != stoppedByTimeout || out.ExitCode != ExitPaused {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	if fake.calls <= 3 {
		t.Fatalf("calls=%d, want persistent retry", fake.calls)
	}
	entries, err := store.List(context.Background(), string(out.RunID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Type == journal.TypeStateChange && entry.State == string(types.RunPaused) {
			var change journal.StateChangePayload
			if err := json.Unmarshal(entry.Payload, &change); err != nil {
				t.Fatal(err)
			}
			if change.Reason != "budget" {
				t.Fatalf("pause reason=%q, want budget", change.Reason)
			}
			return
		}
	}
	t.Fatal("deadline must persist a durable budget pause")
}

func TestExecQuotaExhaustionIsInfrastructure(t *testing.T) {
	t.Setenv("EXCALIBUR_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("EXCALIBUR_FALLBACK_API_KEY", "")
	t.Setenv("EXCALIBUR_EXHAUSTED_KEYS", "1")
	t.Setenv("EXCALIBUR_BUDGET_RETRIES", "1")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"code":"insufficient_quota","message":"You have no credits remaining"}}`)
	}))
	defer srv.Close()
	p, err := models.NewProvider(context.Background(), registry.ProviderDef{Kind: "openai", APIKey: "primary", FallbackKey: "fallback", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{Request: "write hello.txt", LogsDir: logs, Profile: ProfileEval, Provider: p})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunWaiting || out.Reason != "provider_quota_exhausted" {
		t.Fatalf("outcome: %+v", out)
	}
	// One failed call plus one confirmation probe for each credential.
	if calls.Load() != 4 {
		t.Fatalf("calls: %d", calls.Load())
	}
	raw, err := os.ReadFile(filepath.Join(logs, "excalibur", "final.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"reason": "provider_quota_exhausted"`)) {
		t.Fatalf("final: %s", raw)
	}
	raw, err = os.ReadFile(filepath.Join(logs, "excalibur", "model_calls.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"provider_error_class":"provider_quota_exhausted"`)) {
		t.Fatalf("model record: %s", raw)
	}
}
