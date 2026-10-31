package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

// version identifies the binary in manifests and ATIF metadata. The
// release build overrides it with -ldflags -X main.version=....
var version = "dev"

func nowUTC() time.Time { return time.Now().UTC() }

// bench credentials file shape (operator file, Section 23.2: keys only
// via environment, never written to logs).
type benchCreds struct {
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key"`
	FallbackAPIKey  string `json:"fallback_api_key"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	EmbeddingModel  string `json:"embedding_model"`
	DecisionsModel  string `json:"decisions_model"`
}

func loadBenchCreds(path string) (benchCreds, error) {
	var c benchCreds
	if path == "" {
		return c, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("bench: read credentials file: %w", err)
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("bench: parse credentials file: %w", err)
	}
	return c, nil
}

func redact(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "****" + s[len(s)-4:]
}

// runBench implements `excalibur bench <tb21|tb40> <run|smoke|status|report>`.
// The run subcommand wraps the static binary build, the installed-agent
// Harbor route, the frozen eval profile and the Appendix F fixed
// conditions. Keys travel to the container only via -ae environment
// variables and are never printed, logged or written to any artifact.
func runBench(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: excalibur bench <tb21|tb40> <run|smoke|status|report> [flags]")
	}
	ds := strings.ToLower(args[0])
	var dataset eval.BenchDataset
	switch ds {
	case "tb21":
		dataset = eval.DatasetTB21
	case "tb40":
		dataset = eval.DatasetTB40
	default:
		return fmt.Errorf("bench: dataset must be tb21 or tb40, got %q", args[0])
	}
	if len(args) < 2 {
		return fmt.Errorf("usage: excalibur bench %s <run|smoke|status|report> [flags]", ds)
	}
	sub := strings.ToLower(args[1])
	rest := args[2:]
	switch sub {
	case "run":
		return runBenchRun(dataset, rest)
	case "smoke":
		return runBenchSmoke(dataset, rest)
	case "status":
		return runBenchStatus(dataset, rest)
	case "report":
		return runBenchReport(dataset, rest)
	default:
		return fmt.Errorf("bench %s: unknown subcommand %q (want run|smoke|status|report)", ds, sub)
	}
}

func benchDatasetRef(d eval.BenchDataset) eval.DatasetRef {
	return eval.PinnedDatasets()[d]
}

func runBenchSmoke(dataset eval.BenchDataset, args []string) error {
	fs := flag.NewFlagSet("bench smoke", flag.ContinueOnError)
	dir := fs.String("dir", "", "output directory for smoke trials")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	out := *dir
	if out == "" {
		tmp, err := os.MkdirTemp("", "excalibur-bench-smoke-")
		if err != nil {
			return fmt.Errorf("bench smoke: %w", err)
		}
		out = tmp
	}
	runner := eval.SmokeRunner{Dir: out, Profile: eval.DefaultEvalProfile()}
	results, err := runner.Run(eval.DefaultSmokeTasks())
	if err != nil {
		return fmt.Errorf("bench smoke: %w", err)
	}
	failed := 0
	for _, res := range results {
		status := "passed"
		if !res.Passed {
			status = "failed"
			failed++
		}
		fmt.Fprintf(os.Stdout, "%s: %s (%s)\n", res.Name, status, res.Detail)
	}
	ref := benchDatasetRef(dataset)
	fmt.Fprintf(os.Stdout, "dataset=%s digest=%s smoke trials in %s\n", ref.HarborDataset, ref.Digest, out)
	if failed > 0 {
		return fmt.Errorf("bench smoke: %d task(s) failed", failed)
	}
	return nil
}

func runBenchStatus(dataset eval.BenchDataset, args []string) error {
	fs := flag.NewFlagSet("bench status", flag.ContinueOnError)
	jobsDir := fs.String("jobs-dir", "jobs", "bench jobs directory")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	ref := benchDatasetRef(dataset)
	entries, err := os.ReadDir(*jobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stdout, "dataset=%s no runs in %s\n", ref.HarborDataset, *jobsDir)
			return nil
		}
		return fmt.Errorf("bench status: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(*jobsDir, e.Name(), "manifest.json")
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		var m eval.RunManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		if m.Dataset != ref.HarborDataset && m.Dataset != string(dataset) {
			continue
		}
		fmt.Fprintf(os.Stdout, "%s: model=%s/%s k=%d started=%s\n", e.Name(), m.Provider, m.Model, m.K, m.StartedAt.Format("2006-01-02T15:04:05Z"))
	}
	return nil
}

func runBenchReport(dataset eval.BenchDataset, args []string) error {
	fs := flag.NewFlagSet("bench report", flag.ContinueOnError)
	out := fs.String("out", "", "directory for report.json and report.md (default: <run-dir>)")
	k := fs.Int("k", 5, "pass@k horizon")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: excalibur bench %s report <run-dir> [--out DIR] [--k N]", dataset)
	}
	runDir := rest[0]
	trials, err := loadBenchTrials(runDir)
	if err != nil {
		return err
	}
	rep, err := eval.Aggregate(trials, *k)
	if err != nil {
		return fmt.Errorf("bench report: %w", err)
	}
	dest := *out
	if dest == "" {
		dest = runDir
	}
	if err := eval.WriteReport(dest, rep); err != nil {
		return fmt.Errorf("bench report: %w", err)
	}
	// Markdown companion (Section 23.7 statistics in both formats).
	var manifest eval.RunManifest
	if raw, err := os.ReadFile(filepath.Join(runDir, "manifest.json")); err == nil {
		_ = json.Unmarshal(raw, &manifest)
	}
	md := eval.RenderReportMarkdown(rep, manifest)
	if err := os.WriteFile(filepath.Join(dest, "report.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("bench report: %w", err)
	}
	fmt.Fprintf(os.Stdout, "trials=%d tasks=%d success=%.3f [%.3f, %.3f] pass@%d=%.3f pass^%d=%.3f cost/success=$%.4f\n",
		rep.Trials, rep.Tasks, rep.Success.Mean, rep.Success.Low, rep.Success.High,
		rep.K, rep.PassAtK, rep.K, rep.PassPowK, rep.CostPerSuccess)
	fmt.Fprintf(os.Stdout, "report in %s/report.json and report.md\n", dest)
	return nil
}

// loadBenchTrials reads the run-level trials.jsonl index when present,
// falling back to trial.json files (eval report shape).
func loadBenchTrials(runDir string) ([]eval.TrialOutcome, error) {
	indexPath := filepath.Join(runDir, "trials.jsonl")
	if raw, err := os.ReadFile(indexPath); err == nil {
		var out []eval.TrialOutcome
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var row eval.BenchTrialIndex
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return nil, fmt.Errorf("bench report: parse trials.jsonl: %w", err)
			}
			out = append(out, eval.TrialOutcome{
				TrialID: row.Trial, TaskID: row.Task, Reward: row.Reward,
				Failed:       row.FailCategory == eval.FailHarness || row.FailCategory == eval.FailCrash,
				FailCategory: row.FailCategory,
				InputTokens:  row.InputTokens, CacheRead: row.CacheRead, CacheWrite: row.CacheWrite,
				OutputTokens: row.OutputTokens, CostUSD: row.CostUSD,
				WallSeconds: row.WallSecs, ModelSeconds: row.ModelSecs, ToolSeconds: row.ToolSecs,
				ChildPeakBytes: row.PeakRSS, ToolCalls: int64(sumToolCalls(row.ToolCalls)),
			})
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return eval.LoadTrials(runDir)
}

func sumToolCalls(m map[string]int64) int64 {
	var total int64
	for _, v := range m {
		total += v
	}
	return total
}

func runBenchRun(dataset eval.BenchDataset, args []string) error {
	fs := flag.NewFlagSet("bench run", flag.ContinueOnError)
	credsFile := fs.String("credentials-file", "", "operator credentials JSON (base_url, api_key, model, effort)")
	provider := fs.String("provider", "", "provider kind (openai, anthropic, ...)")
	model := fs.String("model", "", "model id")
	effort := fs.String("effort", "", "reasoning effort (low, medium, ...)")
	baseURL := fs.String("base-url", "", "inference base URL override")
	tasks := fs.String("tasks", "", "comma-separated task subset (Harbor include names)")
	concurrency := fs.Int("concurrency", 2, "concurrent trials (-n)")
	k := fs.Int("k", 5, "attempts per task (-k, default 5)")
	jobsDir := fs.String("jobs-dir", "jobs", "Harbor jobs directory (-o)")
	jobName := fs.String("job-name", "", "job name (default: timestamp)")
	binary := fs.String("binary", "", "host static binary (default: build)")
	noBuild := fs.Bool("no-build", false, "skip the static build (use --binary)")
	upload := fs.Bool("upload", false, "upload results to Harbor Hub (default: never)")
	dryRun := fs.Bool("dry-run", false, "print the Harbor command without running")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	ref := benchDatasetRef(dataset)
	creds, err := loadBenchCreds(*credsFile)
	if err != nil {
		return err
	}
	// Resolve provider/model/effort/baseURL: flags win, then the
	// credentials file. Missing model is an error; missing provider
	// defaults to openai.
	effProv := *provider
	if effProv == "" {
		effProv = "openai"
	}
	effModel := *model
	if effModel == "" {
		effModel = creds.Model
	}
	if effModel == "" {
		return fmt.Errorf("bench: no model (set --model or the credentials file model)")
	}
	effEffort := *effort
	if effEffort == "" {
		effEffort = creds.ReasoningEffort
	}
	if effEffort == "" {
		effEffort = "low"
	}
	effBaseURL := *baseURL
	if effBaseURL == "" {
		effBaseURL = creds.BaseURL
	}
	apiKey := creds.APIKey
	fallbackKey := creds.FallbackAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("EXCALIBUR_API_KEY")
	}
	if apiKey == "" && effProv == "openai" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("bench: no API key (credentials file api_key or EXCALIBUR_API_KEY/OPENAI_API_KEY)")
	}
	// Static binary build (cgo+musl, sha256), unless --no-build.
	binPath := *binary
	var binSHA string
	if !*noBuild {
		built, sha, err := buildStaticBinary("")
		if err != nil {
			return err
		}
		binPath = built
		binSHA = sha
	} else {
		if binPath == "" {
			return fmt.Errorf("bench: --no-build needs --binary")
		}
		raw, err := os.ReadFile(binPath)
		if err != nil {
			return fmt.Errorf("bench: read binary: %w", err)
		}
		sum := sha256.Sum256(raw)
		binSHA = hex.EncodeToString(sum[:])
	}
	if binSHA == "" && binPath != "" {
		raw, err := os.ReadFile(binPath)
		if err != nil {
			return fmt.Errorf("bench: read binary: %w", err)
		}
		sum := sha256.Sum256(raw)
		binSHA = hex.EncodeToString(sum[:])
	}
	// Appendix F: frozen conditions. Reject any timeout/resource
	// overrides; timeouts and resources always stay at the task
	// defaults (no --timeout-multiplier passthrough).
	for _, a := range args {
		if strings.Contains(a, "timeout-multiplier") || strings.Contains(a, "timeout_multiplier") {
			return fmt.Errorf("bench: timeout changes are forbidden (Appendix F)")
		}
	}
	harborModel := effModel
	if !strings.Contains(harborModel, "/") {
		harborModel = effProv + "/" + effModel
	}
	// Harbor command. The adapter is resolved by import path (verified
	// against Harbor 0.25.0: -a module.path:ClassName, not built-in).
	adapterDir, err := harborAdapterDir()
	if err != nil {
		return err
	}
	harborArgs := []string{
		"run", "-d", ref.HarborDataset,
		"-a", "excalibur_agent:ExcaliburAgent",
		"-m", harborModel,
		"-k", fmt.Sprintf("%d", *k),
		"-n", fmt.Sprintf("%d", *concurrency),
		"-o", *jobsDir,
		"--effort", effEffort,
		"--api-key-name", "EXCALIBUR_API_KEY",
		"--skip-credential-checks",
		"--ak", "binary=" + binPath,
		"--ak", "expected_sha256=" + binSHA,
		"--ae", "EXCALIBUR_PROVIDER=" + effProv,
		"--ae", "EXCALIBUR_MODEL=" + effModel,
		"--ae", "EXCALIBUR_API_KEY=" + apiKey,
		"--ae", "EXCALIBUR_BINARY=" + binPath,
		"--ae", "EXCALIBUR_SHA256=" + binSHA,
	}
	if effBaseURL != "" {
		harborArgs = append(harborArgs, "--base-url", effBaseURL)
	}
	if fallbackKey != "" {
		harborArgs = append(harborArgs, "--ae", "EXCALIBUR_FALLBACK_API_KEY="+fallbackKey)
	}
	if *jobName != "" {
		harborArgs = append(harborArgs, "--job-name", *jobName)
	}
	if *tasks != "" {
		for _, t := range strings.Split(*tasks, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			harborArgs = append(harborArgs, "-i", t)
		}
	}
	if *upload {
		harborArgs = append(harborArgs, "--upload")
	}
	env := os.Environ()
	// PYTHONPATH for the import-path adapter; PATH for the harbor CLI.
	pythonPath := adapterDir
	if prev := os.Getenv("PYTHONPATH"); prev != "" {
		pythonPath += string(os.PathListSeparator) + prev
	}
	env = appendEnv(env, "PYTHONPATH="+pythonPath)
	// Never print keys: log a redacted preview only.
	redacted := make([]string, len(harborArgs))
	copy(redacted, harborArgs)
	for i, a := range redacted {
		if strings.HasPrefix(a, "EXCALIBUR_API_KEY=") || strings.HasPrefix(a, "EXCALIBUR_FALLBACK_API_KEY=") {
			redacted[i] = strings.SplitN(a, "=", 2)[0] + "=" + redact(strings.SplitN(a, "=", 2)[1])
		}
	}
	fmt.Fprintf(os.Stdout, "dataset=%s digest=%s model=%s effort=%s k=%d binary=%s sha256=%s\n",
		ref.HarborDataset, ref.Digest, harborModel, effEffort, *k, binPath, binSHA)
	fmt.Fprintf(os.Stdout, "harbor %s\n", strings.Join(redacted, " "))
	if *dryRun {
		return nil
	}
	// Verify the dataset digest resolves to the pinned value before
	// spending on trials (Appendix F: same tasks in every arm).
	if err := verifyDatasetDigest(ref); err != nil {
		fmt.Fprintf(os.Stderr, "bench: warning: %v\n", err)
	}
	cmd := exec.Command("harbor", harborArgs...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bench: harbor run: %w", err)
	}
	// Post-run: write the run manifest and trials index next to the
	// Harbor job dir, then collect verifier outputs beside our records.
	if err := finalizeBenchRun(*jobsDir, *jobName, dataset, ref, effProv, effModel, effEffort, *k, binSHA); err != nil {
		fmt.Fprintf(os.Stderr, "bench: finalize: %v\n", err)
	}
	return nil
}

func appendEnv(env []string, kv string) []string {
	key := strings.SplitN(kv, "=", 2)[0]
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return append(out, kv)
}

func harborAdapterDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		// Fall back to the repo layout.
		if st, err2 := os.Stat("integrations/harbor"); err2 == nil && st.IsDir() {
			abs, _ := filepath.Abs("integrations/harbor")
			return abs, nil
		}
		return "", fmt.Errorf("bench: locate adapter: %w", err)
	}
	// <repo>/cmd/excalibur -> <repo>/integrations/harbor is not stable
	// for installed binaries; prefer the repo layout when present.
	for _, cand := range []string{"integrations/harbor", "../integrations/harbor", "../../integrations/harbor"} {
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			abs, _ := filepath.Abs(cand)
			return abs, nil
		}
	}
	// Last resort: the binary's directory (bench built from repo).
	dir := filepath.Dir(exe)
	cand := filepath.Join(dir, "..", "..", "integrations", "harbor")
	if st, err := os.Stat(cand); err == nil && st.IsDir() {
		abs, _ := filepath.Abs(cand)
		return abs, nil
	}
	return "", fmt.Errorf("bench: cannot locate integrations/harbor (run from the repo root)")
}

// buildStaticBinary compiles the Linux amd64 static binary (pure-Go,
// CGO_ENABLED=0, no system packages) and returns its path and sha256.
// The architecture document asks for cgo+musl; the pure-Go static
// build is used because the musl sysroot for sqlite-vec cgo is absent
// on this host, with identical portability (see docs/decisions).
func buildStaticBinary(out string) (string, string, error) {
	if out == "" {
		out = filepath.Join("dist", "bench", "excalibur-linux-amd64")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", "", fmt.Errorf("bench: mkdir dist: %w", err)
	}
	cmd := exec.Command("go", "build",
		"-ldflags", "-X main.version="+version,
		"-o", out, "./cmd/excalibur")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("bench: static build: %w", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		return "", "", fmt.Errorf("bench: read binary: %w", err)
	}
	sum := sha256.Sum256(raw)
	sha := hex.EncodeToString(sum[:])
	if err := os.WriteFile(out+".sha256", []byte(sha+"\n"), 0o644); err != nil {
		return "", "", fmt.Errorf("bench: write sha: %w", err)
	}
	fmt.Fprintf(os.Stdout, "binary=%s sha256=%s\n", out, sha)
	return out, sha, nil
}

// verifyDatasetDigest checks the Harbor registry digest against the
// pinned Appendix F value. Failures are warnings (offline registries)
// so --dry-run and local smoke never block on network.
func verifyDatasetDigest(ref eval.DatasetRef) error {
	out, err := exec.Command("harbor", "version", "show", ref.HarborDataset+"@latest", "--json").Output()
	if err != nil {
		return fmt.Errorf("cannot verify dataset digest (offline?): %w", err)
	}
	var doc struct {
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return fmt.Errorf("cannot parse dataset version: %w", err)
	}
	if doc.ContentHash != "" && doc.ContentHash != ref.Digest {
		return fmt.Errorf("dataset %s digest %s != pinned %s", ref.HarborDataset, doc.ContentHash, ref.Digest)
	}
	return nil
}

// finalizeBenchRun writes manifest.json after a Harbor job and builds
// the run-level trials.jsonl index. The Harbor job directory layout
// (<jobs>/<job-name> with trial subdirs) is discovered from the jobs
// dir; when --job-name is empty the newest directory wins. For every
// trial it collects Harbor's verifier reward/output and trial logs
// next to the excalibur records and classifies the failure category
// per Appendix F.
func finalizeBenchRun(jobsDir, jobName string, dataset eval.BenchDataset, ref eval.DatasetRef, provider, model, effort string, k int, binSHA string) error {
	dir := jobsDir
	if jobName != "" {
		dir = filepath.Join(jobsDir, jobName)
	} else {
		entries, err := os.ReadDir(jobsDir)
		if err != nil {
			return err
		}
		var latest string
		var latestTime int64
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Unix() >= latestTime {
				latestTime = info.ModTime().Unix()
				latest = filepath.Join(jobsDir, e.Name())
			}
		}
		if latest == "" {
			return fmt.Errorf("no job dirs in %s", jobsDir)
		}
		dir = latest
	}
	manifest := eval.RunManifest{
		ExcaliburCommit: version,
		BinarySHA256:    binSHA,
		HarborVersion:   eval.PinnedHarborVersion,
		HarborCommit:    eval.PinnedHarborCommit,
		Dataset:         ref.HarborDataset,
		DatasetDigest:   ref.Digest,
		Model:           model,
		Provider:        provider,
		Effort:          effort,
		K:               k,
		Budgets:         map[string]string{"model": "task-default", "timeout": "task-default (no multiplier)"},
		Machine:         eval.MachineCharacteristics(),
	}
	// started_at best-effort: manifest write time.
	manifest.StartedAt = nowUTC()
	if err := eval.WriteManifest(dir, manifest); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "manifest in %s/manifest.json\n", dir)
	// Index every trial subdir (those holding result.json).
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	indexPath := filepath.Join(dir, "trials.jsonl")
	_ = os.Remove(indexPath)
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		trialDir := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(trialDir, "result.json")); err != nil {
			continue
		}
		row, err := indexTrial(trialDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench: skip %s: %v\n", e.Name(), err)
			continue
		}
		if err := eval.AppendTrialIndex(dir, row); err != nil {
			return err
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "indexed %d trials in %s/trials.jsonl\n", count, dir)
	return nil
}

// indexTrial builds one trials.jsonl row from a Harbor trial directory.
func indexTrial(trialDir string) (eval.BenchTrialIndex, error) {
	var row eval.BenchTrialIndex
	raw, err := os.ReadFile(filepath.Join(trialDir, "result.json"))
	if err != nil {
		return row, err
	}
	var doc struct {
		TaskName  string `json:"task_name"`
		TrialName string `json:"trial_name"`
		TaskID    struct {
			Ref string `json:"ref"`
		} `json:"task_id"`
		AgentResult struct {
			NInput  *int64   `json:"n_input_tokens"`
			NCache  *int64   `json:"n_cache_tokens"`
			NOutput *int64   `json:"n_output_tokens"`
			Cost    *float64 `json:"cost_usd"`
		} `json:"agent_result"`
		VerifierResult struct {
			Rewards map[string]float64 `json:"rewards"`
		} `json:"verifier_result"`
		ExceptionInfo *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"exception_info"`
		AgentExecution *struct {
			StartedAt  string `json:"started_at"`
			FinishedAt string `json:"finished_at"`
		} `json:"agent_execution"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return row, err
	}
	row.Task = doc.TaskName
	if row.Task == "" {
		row.Task = filepath.Base(trialDir)
	}
	row.Trial = doc.TrialName
	if row.Trial == "" {
		row.Trial = filepath.Base(trialDir)
	}
	for _, v := range doc.VerifierResult.Rewards {
		row.Reward = v
		break
	}
	// Failure category per Appendix F.
	row.FailCategory = eval.FailVerifier
	if doc.ExceptionInfo != nil {
		msg := (doc.ExceptionInfo.Type + " " + doc.ExceptionInfo.Message)
		lower := strings.ToLower(msg)
		switch {
		case strings.Contains(lower, "timeout"):
			row.FailCategory = eval.FailTimeout
		case strings.Contains(lower, "oom") || strings.Contains(lower, "out of memory") || strings.Contains(lower, "memory"):
			row.FailCategory = eval.FailOOM
		case strings.Contains(lower, "budget"):
			row.FailCategory = eval.FailBudget
		case strings.Contains(lower, "cancel") || strings.Contains(lower, "interrupt"):
			row.FailCategory = eval.FailBlocked
		default:
			row.FailCategory = eval.FailHarness
		}
	} else if row.Reward > 0 {
		row.FailCategory = ""
	} else {
		// Reward 0 with no exception: check for harness markers in
		// the excalibur final state.
		if finalRaw, err := os.ReadFile(filepath.Join(trialDir, "agent", "excalibur", "final.json")); err == nil {
			var fin struct {
				State string `json:"state"`
			}
			if json.Unmarshal(finalRaw, &fin) == nil {
				switch fin.State {
				case "completed", "failed":
					row.FailCategory = eval.FailVerifier
				case "paused":
					row.FailCategory = eval.FailBudget
				case "blocked":
					row.FailCategory = eval.FailBlocked
				default:
					row.FailCategory = eval.FailHarness
				}
			}
		}
	}
	// Usage from Harbor agent_result, falling back to our metrics.json.
	if doc.AgentResult.NInput != nil {
		row.InputTokens = *doc.AgentResult.NInput
	}
	if doc.AgentResult.NCache != nil {
		row.CacheRead = *doc.AgentResult.NCache
	}
	if doc.AgentResult.NOutput != nil {
		row.OutputTokens = *doc.AgentResult.NOutput
	}
	if doc.AgentResult.Cost != nil {
		row.CostUSD = *doc.AgentResult.Cost
	}
	if metricsRaw, err := os.ReadFile(filepath.Join(trialDir, "agent", "excalibur", "metrics.json")); err == nil {
		var snap struct {
			Usage struct {
				Input      int64 `json:"input"`
				CacheRead  int64 `json:"cache_read"`
				CacheWrite int64 `json:"cache_write"`
				Output     int64 `json:"output"`
				Reasoning  int64 `json:"reasoning"`
			} `json:"usage"`
			CostUSD             float64          `json:"cost_usd"`
			WallSeconds         float64          `json:"wall_seconds"`
			ModelSeconds        float64          `json:"model_seconds"`
			ToolSeconds         float64          `json:"tool_seconds"`
			ChildPeakBytes      uint64           `json:"child_peak_bytes"`
			DaemonRSSTotalBytes uint64           `json:"daemon_rss_total_bytes"`
			CallsByTool         map[string]int64 `json:"calls_by_tool"`
		}
		if json.Unmarshal(metricsRaw, &snap) == nil {
			if row.InputTokens == 0 {
				row.InputTokens = snap.Usage.Input
				row.CacheRead = snap.Usage.CacheRead
				row.CacheWrite = snap.Usage.CacheWrite
				row.OutputTokens = snap.Usage.Output + snap.Usage.Reasoning
				row.CostUSD = snap.CostUSD
			}
			row.WallSecs = snap.WallSeconds
			row.ModelSecs = snap.ModelSeconds
			row.ToolSecs = snap.ToolSeconds
			row.PeakRSS = snap.ChildPeakBytes
			if snap.DaemonRSSTotalBytes > row.PeakRSS {
				row.PeakRSS = snap.DaemonRSSTotalBytes
			}
			row.ToolCalls = snap.CallsByTool
			row.Steps = countLines(filepath.Join(trialDir, "agent", "excalibur", "model_calls.jsonl"))
		}
	}
	// Collect verifier reward/output next to the excalibur records.
	exDir := filepath.Join(trialDir, "agent", "excalibur")
	for _, name := range []string{"reward.txt", "reward.json", "test-stdout.txt", "test-stderr.txt", "ctrf.json"} {
		src := filepath.Join(trialDir, "verifier", name)
		if raw, err := os.ReadFile(src); err == nil {
			_ = os.WriteFile(filepath.Join(exDir, "verifier_"+name), raw, 0o644)
		}
	}
	for _, name := range []string{"result.json", "trial.log", "config.json"} {
		if raw, err := os.ReadFile(filepath.Join(trialDir, name)); err == nil {
			_ = os.WriteFile(filepath.Join(exDir, "harbor_"+name), raw, 0o644)
		}
	}
	return row, nil
}

func countLines(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
