package cli

import (
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"os"
	"path/filepath"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
)

func TestParseProfile(t *testing.T) {
	for in, want := range map[string]ProfileName{
		"":         ProfileDefault,
		"default":  ProfileDefault,
		"delegate": ProfileDelegate,
		"eval":     ProfileEval,
		"DELEGATE": ProfileDelegate,
	} {
		got, err := ParseProfile(in)
		if err != nil || got != want {
			t.Fatalf("ParseProfile(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseProfile("nope"); err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestEphemeralRules(t *testing.T) {
	if err := ValidateEphemeral(ProfileDelegate, true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEphemeral(ProfileEval, true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEphemeral(ProfileDefault, false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateEphemeral(ProfileDefault, true); err == nil {
		t.Fatal("default+ephemeral must be rejected (Section 3.6)")
	}
}

func TestDataDirLayout(t *testing.T) {
	logs := t.TempDir()
	evalDir, err := DataDir(ProfileEval, "", logs)
	if err != nil {
		t.Fatal(err)
	}
	if evalDir != filepath.Join(logs, "excalibur") {
		t.Fatalf("eval data dir = %q", evalDir)
	}
	proj := t.TempDir()
	delDir, err := DataDir(ProfileDelegate, proj, logs)
	if err != nil {
		t.Fatal(err)
	}
	if delDir != filepath.Join(proj, ".excalibur") {
		t.Fatalf("delegate data dir = %q", delDir)
	}
	if _, err := DataDir("nope", "", logs); err == nil {
		t.Fatal("unknown profile must fail")
	}
}

func TestResolveLogsDir(t *testing.T) {
	explicit := t.TempDir()
	got, err := ResolveLogsDir(explicit)
	if err != nil || got != explicit {
		t.Fatalf("explicit logs dir = %q, %v", got, err)
	}
	t.Setenv("EXCALIBUR_LOGS", explicit)
	got, err = ResolveLogsDir("")
	if err != nil || got != explicit {
		t.Fatalf("env logs dir = %q, %v", got, err)
	}
}

func TestEvalDataReady(t *testing.T) {
	dir := t.TempDir()
	if err := EvalDataReady(dir, ""); err != nil {
		t.Fatalf("empty dir must pass: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EvalDataReady(dir, ""); err == nil {
		t.Fatal("non-empty dir without override must abort (Section 23.2)")
	}
	if err := EvalDataReady(dir, "declared"); err != nil {
		t.Fatalf("override must allow: %v", err)
	}
	if err := EvalDataReady(filepath.Join(dir, "missing"), ""); err != nil {
		t.Fatalf("missing dir must pass: %v", err)
	}
}

func TestEffectiveTools(t *testing.T) {
	core, deferred := EffectiveTools(ProfileEval)
	if len(core) == 0 || len(deferred) == 0 {
		t.Fatal("eval needs core and deferred tools")
	}
	// Item-41 trim: the eval core keeps the tools that act in a single
	// run; task_db and subagent are no longer exposed there.
	for _, want := range []string{"exec", "search_tools", "call_tool"} {
		found := false
		for _, tool := range core {
			if tool == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("eval core misses %q", want)
		}
	}
	defCore, _ := EffectiveTools(ProfileDefault)
	hasMaster := false
	for _, tool := range defCore {
		if tool == "send_message" {
			hasMaster = true
		}
	}
	if !hasMaster {
		t.Fatal("default profile must carry master tools; delegate/eval must not")
	}
	for _, tool := range core {
		if tool == "send_message" {
			t.Fatal("eval core must not carry master tools")
		}
	}
	if ToolsHash(ProfileEval) == ToolsHash(ProfileDefault) {
		t.Fatal("tool hashes must differ per profile")
	}
	again := ToolsHash(ProfileEval)
	if ToolsHash(ProfileEval) != again {
		t.Fatal("tool hash must be stable")
	}
}

func TestEvalToolAllowed(t *testing.T) {
	// Section 23.2 table: assistant tools hidden, project ops of
	// task_db rejected, task-internal planning enabled.
	for _, tool := range []string{"send_message", "ask_user", "email", "browser_navigate", "memory_search", "install_skill"} {
		if err := EvalToolAllowed(tool, ""); err == nil {
			t.Fatalf("tool %q must be hidden in eval", tool)
		}
	}
	for _, op := range []string{"register_project", "update_project", "archive_project"} {
		if err := EvalToolAllowed("task_db", op); err == nil {
			t.Fatalf("task_db %q must be rejected in eval", op)
		}
	}
	for _, op := range []string{"create_task", "add_criterion", "record_evidence", "query"} {
		if err := EvalToolAllowed("task_db", op); err != nil {
			t.Fatalf("task_db %q must stay enabled: %v", op, err)
		}
	}
	if err := EvalToolAllowed("exec", ""); err != nil {
		t.Fatalf("exec must stay enabled: %v", err)
	}
}

// TestEvalSurfaceMatchesProfileTable pins the Section 23.8 control:
// the tool surface --profile eval exposes must coincide exactly with
// the frozen Section 23.2 table — same core, same deferred, same
// skill groups, same recorded catalog hash. Any drift fails here
// before it can silently change a benchmark arm (Appendix F).
func TestEvalSurfaceMatchesProfileTable(t *testing.T) {
	want := eval.DefaultEvalProfile()
	core, deferred := EffectiveTools(ProfileEval)
	if !sameSet(core, want.EnabledTools) {
		t.Fatalf("eval core %v != profile table %v", core, want.EnabledTools)
	}
	if !sameSet(deferred, want.DeferredTools) {
		t.Fatalf("eval deferred %v != profile table %v", deferred, want.DeferredTools)
	}
	if want.SkillsCatalogHash != eval.EvalSkillsCatalogHash() {
		t.Fatal("profile must record the frozen eval catalog hash")
	}
	ix := skills.NewEvalBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	if ix.CatalogHash() != want.SkillsCatalogHash {
		t.Fatal("eval must run with the frozen skill catalog it records")
	}
}

func sameSet(have, want []string) bool {
	if len(have) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, h := range have {
		seen[h]++
	}
	for _, w := range want {
		seen[w]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}
