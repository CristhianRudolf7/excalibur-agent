package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

// EvalProfile is the frozen benchmark profile of Section 23.2. The
// profile is versioned and hashed; any deviation needs an explicit
// --profile-override recorded in metadata and ATIF.
const EvalProfileVersion = "eval@1"

// EvalProfileConfig is the effective profile content (no secrets).
type EvalProfileConfig struct {
	Version string `json:"version"`
	// EnabledTools are the core tools available in eval.
	EnabledTools []string `json:"enabled_tools"`
	// DeferredTools load on demand without occupying the prefix.
	DeferredTools []string `json:"deferred_tools"`
	// EnabledSkillGroups are the integral plus software-engineering
	// groups with a frozen catalog.
	EnabledSkillGroups []string `json:"enabled_skill_groups"`
	SkillsCatalogHash  string   `json:"skills_catalog_hash"`
	// SingleModel forces every role onto the evaluated model.
	SingleModel bool `json:"single_model"`
	// Model is the evaluated model id (metadata only).
	Model string `json:"model,omitempty"`
	// Override records a --profile-override deviation; trials with a
	// non-empty override never compare against clean trials.
	Override string `json:"override,omitempty"`
}

// DefaultEvalProfile returns the frozen Section 23.2 table: internal
// planning on, multiprocject off, children <= min(2, task CPUs),
// core+deferred tools per the table, assistant skills off,
// single-model mode, telemetry on.
func DefaultEvalProfile() EvalProfileConfig {
	return EvalProfileConfig{
		Version: EvalProfileVersion,
		EnabledTools: []string{
			"read_file", "write_file", "edit_file", "exec", "task_db",
			"history_search", "get_tool_output", "pin_observation",
			"leave_continuation_note", "ack_directive", "status", "clock",
			"search_skills", "load_skill", "search_tools", "call_tool", "subagent",
		},
		DeferredTools: []string{
			"lsp", "code_graph", "file_history", "revert_edit",
			"workspace_snapshot", "worktree", "integration_queue",
			"document_read", "pdf_ops", "file_convert",
			"diagram_render", "chart_render", "vision_extract",
		},
		EnabledSkillGroups: []string{"builtin", "software-engineering"},
		SkillsCatalogHash:  "frozen-placeholder",
		SingleModel:        true,
	}
}

// Hash computes the profile hash recorded in ATIF and metrics.
func (c EvalProfileConfig) Hash() string {
	tools := append([]string(nil), c.EnabledTools...)
	sort.Strings(tools)
	def := append([]string(nil), c.DeferredTools...)
	sort.Strings(def)
	groups := append([]string(nil), c.EnabledSkillGroups...)
	sort.Strings(groups)
	h := sha256.New()
	fmt.Fprintf(h, "version=%s\nsingle=%v\nskills=%s\nmodel=%s\noverride=%s\n",
		c.Version, c.SingleModel, c.SkillsCatalogHash, c.Model, c.Override)
	for _, t := range tools {
		fmt.Fprintf(h, "tool=%s\n", t)
	}
	for _, t := range def {
		fmt.Fprintf(h, "deferred=%s\n", t)
	}
	for _, g := range groups {
		fmt.Fprintf(h, "group=%s\n", g)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CheckDataDir aborts the trial when the data dir is not empty and no
// override is declared (Section 23.2 control).
func CheckDataDir(dir string, override string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("eval: read data dir: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	if override == "" {
		return fmt.Errorf("eval: data dir %s not empty; refusing eval start (use --profile-override to declare a deviation)", dir)
	}
	return nil
}

// BlockedBenchmarkSource reports whether a URL belongs to the benchmark
// itself (repos, Harbor Hub, tbench.ai): consulting them is reward
// hacking and is blocked and logged (Section 23.2). It delegates to
// the shared capabilities blocklist so web tools and the eval gate
// can never drift apart.
func BlockedBenchmarkSource(url string) bool {
	return capabilities.BlockedBenchmarkSource(url)
}

// WebToolsVisible reports whether web tools stay enabled under the
// task network policy signal from Harbor.
func WebToolsVisible(networkRestricted bool) bool { return !networkRestricted }

// CheckSingleModel verifies only the evaluated model was called.
func CheckSingleModel(calledModels []string, evaluated string) error {
	seen := map[string]bool{}
	for _, m := range calledModels {
		seen[m] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("eval: no model calls recorded")
	}
	if len(seen) > 1 || !seen[evaluated] {
		return fmt.Errorf("eval: single-model violation: called %v, evaluated %q", calledModels, evaluated)
	}
	return nil
}

// WriteProfile writes the effective config (no secrets) and its hash
// for the Appendix F minimum records.
func WriteProfile(dir string, cfg EvalProfileConfig) error {
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("eval: marshal profile: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("eval: mkdir profile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), append(buf, '\n'), 0o644); err != nil {
		return fmt.Errorf("eval: write profile: %w", err)
	}
	hash := cfg.Hash()
	if err := os.WriteFile(filepath.Join(dir, "profile.sha256"), []byte(hash+"\n"), 0o644); err != nil {
		return fmt.Errorf("eval: write profile hash: %w", err)
	}
	return nil
}
