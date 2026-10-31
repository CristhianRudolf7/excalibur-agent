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

// stringSliceFlag collects repeatable --set key=value flags.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string { return strings.Join(*s, ",") }

func (s *stringSliceFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

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
		return fmt.Errorf("usage: excalibur bench <tb21|tb40> <run|smoke|status|report> [flags] | excalibur bench compare <runA> <runB> [flags]")
	}
	// Compare is dataset-independent: it pairs two finished run
	// directories (bench jobs or eval quick dirs).
	if strings.ToLower(args[0]) == "compare" {
		return runBenchCompare(args[1:])
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
	return eval.LoadRunTrials(runDir)
}

// runBenchCompare implements `excalibur bench compare <runA> <runB>`:
// the paired per-task comparison of Section 23.7 with the acceptance
// rule, over bench jobs or eval quick dirs alike.
func runBenchCompare(args []string) error {
	fs := flag.NewFlagSet("bench compare", flag.ContinueOnError)
	out := fs.String("out", "", "directory for compare.json and compare.md (default: <runB>)")
	k := fs.Int("k", 5, "pass@k horizon")
	reps := fs.Int("reps", 10000, "bootstrap resamples")
	seed := fs.Int64("seed", 7, "bootstrap seed (deterministic)")
	bar := fs.Float64("bar", eval.DefaultPracticalBar, "practical acceptance bar (fraction, default 0.03)")
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: excalibur bench compare <runA> <runB> [--out DIR] [--k N] [--reps N] [--seed N] [--bar F]")
	}
	rep, err := eval.CompareRuns(rest[0], rest[1], *k, *reps, *seed, *bar)
	if err != nil {
		return fmt.Errorf("bench compare: %w", err)
	}
	dest := *out
	if dest == "" {
		dest = rest[1]
	}
	if err := eval.WriteCompareReport(dest, rep); err != nil {
		return fmt.Errorf("bench compare: %w", err)
	}
	md := eval.RenderCompareMarkdown(rep)
	if err := os.WriteFile(filepath.Join(dest, "compare.md"), []byte(md), 0o644); err != nil {
		return fmt.Errorf("bench compare: %w", err)
	}
	fmt.Fprintf(os.Stdout, "%s: paired=%d success A=%.3f [%.3f, %.3f] B=%.3f [%.3f, %.3f] diff=%+.3f [%+.3f, %+.3f] pass@%d A=%.3f B=%.3f cost/success A=$%.4f B=$%.4f\n",
		rep.Verdict, len(rep.PairedTasks),
		rep.SuccessA.Mean, rep.SuccessA.Low, rep.SuccessA.High,
		rep.SuccessB.Mean, rep.SuccessB.Low, rep.SuccessB.High,
		rep.Diff.Mean, rep.Diff.Low, rep.Diff.High,
		rep.K, rep.PassAtKA, rep.PassAtKB,
		rep.CostPerSuccessA, rep.CostPerSuccessB)
	fmt.Fprintf(os.Stdout, "compare in %s/compare.json and compare.md\n", dest)
	return nil
}

func runBenchRun(dataset eval.BenchDataset, args []string) error {
	fs := flag.NewFlagSet("bench run", flag.ContinueOnError)
	credsFile := fs.String("credentials-file", "", "operator credentials JSON (base_url, api_key, model, effort)")
	provider := fs.String("provider", "", "provider kind (openai, anthropic, ...)")
	model := fs.String("model", "", "model id")
	effort := fs.String("effort", "", "reasoning effort (low, medium, ...)")
	baseURL := fs.String("base-url", "", "inference base URL override")
	armName := fs.String("arm", "", "named harness configuration (recorded in the manifest and profile hash)")
	switches := stringSliceFlag{}
	fs.Var(&switches, "set", "mechanism switch key=value (repeatable; forwarded as container env, recorded in manifest)")
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
	if err := eval.ValidateArm(*armName); err != nil {
		return fmt.Errorf("bench: %w", err)
	}
	switchMap, err := eval.ParseSwitches(switches)
	if err != nil {
		return fmt.Errorf("bench: %w", err)
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
	// Named arm: every mechanism switch travels to the trial
	// container verbatim as environment (additive: no switch means no
	// extra --ae entry and byte-identical behavior to pre-arm runs).
	// The arm name itself travels too so trial records stay
	// self-describing even outside the manifest.
	switchKeys := make([]string, 0, len(switchMap))
	for key := range switchMap {
		switchKeys = append(switchKeys, key)
	}
	sortStrings(switchKeys)
	if *armName != "" {
		harborArgs = append(harborArgs, "--ae", "EXCALIBUR_ARM="+*armName)
	}
	for _, key := range switchKeys {
		harborArgs = append(harborArgs, "--ae", key+"="+switchMap[key])
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
	// Never print keys: log a redacted preview only. Any --ae entry
	// whose key looks like a credential only ever prints redacted;
	// mechanism switches stay verbatim so arms are auditable.
	redacted := make([]string, len(harborArgs))
	copy(redacted, harborArgs)
	for i, a := range redacted {
		if idx := strings.Index(a, "="); idx >= 0 {
			key := a[:idx]
			upper := strings.ToUpper(key)
			if strings.Contains(upper, "API_KEY") || strings.Contains(upper, "SECRET") ||
				strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD") {
				redacted[i] = key + "=" + redact(a[idx+1:])
			}
		}
	}
	fmt.Fprintf(os.Stdout, "dataset=%s digest=%s model=%s effort=%s k=%d binary=%s sha256=%s\n",
		ref.HarborDataset, ref.Digest, harborModel, effEffort, *k, binPath, binSHA)
	if *armName != "" || len(switchMap) > 0 {
		fmt.Fprintf(os.Stdout, "arm=%s switches=%d profile_hash=%s arm_profile_hash=%s\n",
			*armName, len(switchMap),
			eval.DefaultEvalProfile().Hash(),
			eval.ArmProfileHash(eval.DefaultEvalProfile().Hash(), *armName, switchMap))
	}
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
	if err := finalizeBenchRun(*jobsDir, *jobName, dataset, ref, effProv, effModel, effEffort, *k, binSHA, *armName, switchMap); err != nil {
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
func finalizeBenchRun(jobsDir, jobName string, dataset eval.BenchDataset, ref eval.DatasetRef, provider, model, effort string, k int, binSHA, arm string, switches map[string]string) error {
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
		Arm:             arm,
		Switches:        switches,
		ProfileHash:     eval.DefaultEvalProfile().Hash(),
		ArmProfileHash:  eval.ArmProfileHash(eval.DefaultEvalProfile().Hash(), arm, switches),
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

// indexTrial builds one trials.jsonl row from a Harbor trial
// directory: every Appendix F per-trial field from the per-trial
// records (Section 23.2, defect w8-bench-fixes/3). The logic lives
// in internal/eval for unit testing; this stays a thin delegate so
// the binary and the tests share one implementation.
func indexTrial(trialDir string) (eval.BenchTrialIndex, error) {
	return eval.IndexTrial(trialDir)
}
