//go:build live

// Tiny live checks for the production model wiring (Sections 19.1,
// 19.2, 19.4, 10.8): excalibur exec against the real operator
// models through the registry and the gateway, one real embedding
// call and one real decider call. They run only with -tags live and
// EXCALIBUR_LIVE=1, never under plain go test. Credentials come from
// the operator file and travel in Authorization headers only; tests
// log shapes and counts, never keys.
package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/config"
	"github.com/CristhianRudolf7/excalibur-agent/internal/models"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// liveCredentialsPath reports the operator file for live checks: the
// explicit EXCALIBUR_CREDENTIALS_FILE or the default operator path.
// Empty means skip: no credentials, no live calls.
func liveCredentialsPath(t *testing.T) string {
	t.Helper()
	if os.Getenv("EXCALIBUR_LIVE") != "1" {
		t.Skip("set EXCALIBUR_LIVE=1 to run live checks")
	}
	// Clear the package-test stub pins so the full production
	// resolution runs: provider/model/keys from the operator file.
	// A pinned path that names no existing file is the test pin,
	// not an operator file, so it falls back to the default path.
	t.Setenv("EXCALIBUR_PROVIDER", "")
	t.Setenv("EXCALIBUR_MODEL", "")
	path := registry.CredentialsPathFromEnv()
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			path = ""
		}
	}
	if path == "" {
		path = registry.DefaultCredentialsPath()
	}
	if path == "" {
		t.Skip("no credentials path")
	}
	t.Setenv("EXCALIBUR_CREDENTIALS_FILE", path)
	raw, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		t.Skipf("no credentials file at %s", path)
	}
	return path
}

// liveSettings resolves the production wiring and skips when the
// operator file names no model.
func liveSettings(t *testing.T) models.ModelSettings {
	t.Helper()
	liveCredentialsPath(t)
	settings, _, err := ResolveSettings(config.LoadOptions{})
	if err != nil {
		t.Fatalf("resolve settings: %v", err)
	}
	if settings.Provider == "" || settings.Model == "" {
		t.Skipf("operator file names no provider/model")
	}
	return settings
}

func liveContext(t *testing.T, secs int64) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
}

// TestLiveExecHello runs excalibur exec end to end on a tiny task in
// a temporary git repository: create hello.py that prints hello and
// verify it runs. It asserts the completed state, the artifact and
// its runtime output, a valid ATIF trajectory and usage in metrics.
func TestLiveExecHello(t *testing.T) {
	settings := liveSettings(t)
	t.Logf("provider=%s model=%s effort=%s", settings.Provider, settings.Model, settings.ReasoningEffort)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	ctx, cancel := liveContext(t, 600)
	defer cancel()
	outcome, err := RunExec(ctx, ExecOptions{
		Request:    "Create hello.py that prints hello and verify it runs with python3.",
		ProjectDir: dir,
		MaxSteps:   15,
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if outcome.State != types.RunCompleted || outcome.ExitCode != ExitOK {
		t.Fatalf("state = %q exit = %d summary = %q", outcome.State, outcome.ExitCode, outcome.Summary)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "hello.py"))
	if err != nil {
		t.Fatalf("hello.py missing: %v", err)
	}
	runOut, err := exec.CommandContext(ctx, "python3", filepath.Join(dir, "hello.py")).CombinedOutput()
	if err != nil || strings.TrimSpace(string(runOut)) != "hello" {
		t.Fatalf("hello.py runs %q: %v", runOut, err)
	}
	_ = raw
	if err := telemetry.ValidateFile(outcome.ATIFPath); err != nil {
		t.Fatalf("ATIF invalid: %v", err)
	}
	snap := readMetrics(t, outcome.MetricsPath)
	if snap.Model != settings.Model || snap.Provider != settings.Provider {
		t.Fatalf("metrics identity = %s/%s, want %s/%s", snap.Provider, snap.Model, settings.Provider, settings.Model)
	}
	if snap.Usage.Input <= 0 {
		t.Fatalf("metrics usage = %+v, want input tokens", snap.Usage)
	}
	t.Logf("state=%s tokens in=%d out=%d cost=%.6f atif=%s", outcome.State, snap.Usage.Input, snap.Usage.Output, snap.CostUSD, outcome.ATIFPath)
}

// TestLiveExecTerminal runs a task through the interactive terminal
// tool family with the real model: open a terminal session, echo a
// marker and report it back.
func TestLiveExecTerminal(t *testing.T) {
	settings := liveSettings(t)
	_ = settings
	dir := t.TempDir()
	ctx, cancel := liveContext(t, 600)
	defer cancel()
	outcome, err := RunExec(ctx, ExecOptions{
		Request:    "Open a terminal session, run `echo terminal-ok-42`, then report the exact command output in your final message.",
		ProjectDir: dir,
		MaxSteps:   15,
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if outcome.State != types.RunCompleted || outcome.ExitCode != ExitOK {
		t.Fatalf("state = %q exit = %d summary = %q", outcome.State, outcome.ExitCode, outcome.Summary)
	}
	if err := telemetry.ValidateFile(outcome.ATIFPath); err != nil {
		t.Fatalf("ATIF invalid: %v", err)
	}
	t.Logf("state=%s summary=%.120s", outcome.State, outcome.Summary)
}

// TestLiveEmbedding makes one real embedding call with the operator
// embedding model through the production embedder builder. The
// request never carries reasoning effort.
func TestLiveEmbedding(t *testing.T) {
	settings := liveSettings(t)
	emb, err := BuildEmbedder(settings, nil)
	if err != nil {
		t.Fatalf("build embedder: %v", err)
	}
	if emb == nil {
		t.Skip("no embedding endpoint configured")
	}
	if emb.Model() != settings.EmbeddingModel {
		t.Fatalf("embedder model = %q, want %q", emb.Model(), settings.EmbeddingModel)
	}
	ctx, cancel := liveContext(t, 120)
	defer cancel()
	vecs, err := emb.Embed(ctx, []string{"hello world", "goodbye world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) == 0 {
		t.Fatalf("vectors = %d x %d", len(vecs), len0(vecs))
	}
	t.Logf("model=%s dims=%d", emb.Model(), len(vecs[0]))
}

func len0(v [][]float32) int {
	if len(v) == 0 {
		return 0
	}
	return len(v[0])
}

// TestLiveDecider asks one bounded question through the decisions
// model over the production gateway (Section 19.4 LLM fallback).
func TestLiveDecider(t *testing.T) {
	settings := liveSettings(t)
	ctx, cancel := liveContext(t, 300)
	defer cancel()
	p, err := BuildProvider(ctx, settings, nil)
	if err != nil {
		t.Fatalf("build provider: %v", err)
	}
	defer p.Close()
	gw := BuildGateway(settings, config.Default(), p)
	defer gw.Close()
	d := BuildDecider(settings, gw)
	if d == nil {
		t.Fatal("decider must build")
	}
	q := models.Question{Kind: models.KindNoul, Prompt: "2+2 equals 4", Context: "arithmetic"}
	ans, err := d.Ask(ctx, q)
	if err != nil {
		t.Fatalf("decider ask: %v", err)
	}
	if ans.Prob < 0 || ans.Prob > 1 {
		t.Fatalf("probability = %v", ans.Prob)
	}
	t.Logf("decisions model=%s probability=%.3f", settings.DecisionsModel, ans.Prob)
}

// readMetrics decodes the metrics.json snapshot for assertions.
func readMetrics(t *testing.T, path string) metricsSnapshot {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	var snap metricsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	return snap
}

// metricsSnapshot mirrors the telemetry snapshot fields the live
// checks assert: identity, usage and cost.
type metricsSnapshot struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Usage    struct {
		Input  int64 `json:"input"`
		Output int64 `json:"output"`
	} `json:"usage"`
	CostUSD float64 `json:"cost_usd"`
}
