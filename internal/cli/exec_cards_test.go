package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/engine"
	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
	providerfake "github.com/CristhianRudolf7/excalibur-agent/internal/providers/fake"
	"github.com/CristhianRudolf7/excalibur-agent/internal/skills"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// stubRunner is a canned engine.ToolRunner for the cards tests.
type stubRunner struct {
	res tools.Result
	err error
}

func (s *stubRunner) Definition(name string) (tools.Definition, error) {
	return tools.Definition{Name: name}, nil
}

func (s *stubRunner) Execute(context.Context, string, json.RawMessage, tools.ExecuteOptions) (tools.Result, error) {
	return s.res, s.err
}

func cardsTestIndex(t *testing.T) *skills.Index {
	t.Helper()
	ix := skills.NewBuiltinIndex()
	if err := ix.Scan(); err != nil {
		t.Fatal(err)
	}
	return ix
}

// Cards fire only on failed results, never on success, and never
// when the switch or the index disables them.
func TestSkillCardsRunner(t *testing.T) {
	ix := cardsTestIndex(t)
	ctx := context.Background()
	args := json.RawMessage(`{}`)
	failedExec := tools.Result{Status: tools.StatusOK,
		Output: "exit: 127  duration: 0.0s  session: sess-1  alive: false\n/usr/bin/bash: line 1: sqlite3: command not found\n"}
	cases := []struct {
		name       string
		tool       string
		res        tools.Result
		err        error
		errorCards bool
		nilIndex   bool
		wantCard   bool
		wantSkill  string
	}{
		{"missing binary fires", "exec", failedExec, nil, true, false, true, "sqlite-recipes"},
		{"switch off silences", "exec", failedExec, nil, false, false, false, ""},
		{"nil index silences", "exec", failedExec, nil, true, true, false, ""},
		{"success untouched", "exec", tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 0.1s  session: sess-1  alive: false\nPONG\n"}, nil, true, false, false, ""},
		{"success mentioning errors untouched", "exec", tools.Result{Status: tools.StatusOK, Output: "exit: 0  duration: 0.1s  session: sess-1  alive: false\nchecked permission denied handling: ok\n"}, nil, true, false, false, ""},
		{"running session untouched", "exec", tools.Result{Status: tools.StatusOK, Output: "exit: running  duration: 0.3s  session: sess-5  alive: true  next_offset: 0\n"}, nil, true, false, false, ""},
		{"non-exec error has no exec header", "write_file", tools.Result{Status: tools.StatusToolError, Output: "open /app/out: permission denied"}, nil, true, false, false, ""},
		{"runner error passes through", "exec", tools.Result{}, errTestCards, true, false, false, ""},
		{"clean failure fires nothing", "exec", tools.Result{Status: tools.StatusOK, Output: "exit: 1  duration: 0.1s  session: sess-1  alive: false\nassertion failed\n"}, nil, true, false, false, ""},
	}
	for _, c := range cases {
		idx := ix
		if c.nilIndex {
			idx = nil
		}
		r := &skillCardsRunner{base: &stubRunner{res: c.res, err: c.err}, index: idx, errorCards: c.errorCards}
		got, err := r.Execute(ctx, c.tool, args, tools.ExecuteOptions{})
		if c.err != nil {
			if err == nil {
				t.Errorf("%s: runner error must pass through", c.name)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		has := strings.Contains(got.Output, "[skill card — error-triggered]")
		if has != c.wantCard {
			t.Errorf("%s: card present=%v, want %v (output %q)", c.name, has, c.wantCard, got.Output)
		}
		if c.wantCard && !strings.Contains(got.Output, c.wantSkill) {
			t.Errorf("%s: card must name %q, got %q", c.name, c.wantSkill, got.Output)
		}
		if c.wantCard {
			added := strings.Split(strings.TrimPrefix(got.Output, c.res.Output), "\n")
			if len(added) > skills.CardMaxLines+3 {
				t.Errorf("%s: card adds %d lines, budget is %d", c.name, len(added), skills.CardMaxLines)
			}
		}
		// Success results stay byte-identical.
		if !c.wantCard && c.err == nil && got.Output != c.res.Output {
			t.Errorf("%s: non-firing result must pass through untouched", c.name)
		}
	}
}

var errTestCards = errTestCardsNew("boom")

type testCardsErr string

func (e testCardsErr) Error() string { return string(e) }

func errTestCardsNew(s string) error { return testCardsErr(s) }

// Cards mode end to end with a scripted provider: no L1 catalog and
// no suggestions ride the prompt (only the one-line pointer), the
// skills.json record pins mode cards with error_cards on, and a
// failed exec gains exactly one error-triggered card in the records.
func TestExecEngineSkillsCards(t *testing.T) {
	t.Setenv("EXCALIBUR_SKILLS", "cards")
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.txt")
	fake := providerfake.New("e2e-cards")
	makeArgs, _ := json.Marshal(map[string]string{"command": "make -C /tmp/definitely-missing-xyz-cards", "workdir": dir})
	args, _ := json.Marshal(map[string]string{"path": target, "content": "hello cards"})
	readArgs, _ := json.Marshal(map[string]string{"path": target})
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: makeArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c2", Name: "write_file", Arguments: args}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c3", Name: "read_file", Arguments: readArgs}}}},
	}
	// Explicit finish: the run ends ONLY through the two-turn
	// `finish` confirmation, never through a text-only turn.
	fake.Responses = append(fake.Responses, finishScript("wrote hello.txt", "write_file then read_file verified hello.txt")...)
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "build the project with make and write hello", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: dir, Provider: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.State != types.RunCompleted {
		t.Fatalf("state = %q, want completed", out.State)
	}
	// No catalog and no suggestions in the first model call: only
	// the one-line cards pointer.
	first := ""
	for _, msg := range fake.ChatRequests[0].Messages {
		first += string(msg.Role) + "\n" + msg.Content + "\n"
	}
	if strings.Contains(first, "- shell-power-use:") {
		t.Error("cards mode must not inject the L1 catalog")
	}
	if strings.Contains(first, "suggested skills:\n- ") {
		t.Error("cards mode must not inject suggestions")
	}
	if !strings.Contains(first, "On-demand skill cards") {
		t.Error("cards mode must inject the on-demand pointer")
	}
	// The failed make call gains the error-triggered card naming
	// the make recipe in the records.
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[skill card — error-triggered]") {
		t.Error("ATIF must show the error-triggered card")
	}
	if !strings.Contains(string(raw), "make-builds") {
		t.Error("ATIF card must name the make-builds recipe")
	}
	// The ablation record pins mode cards with error_cards on and
	// no injected suggestions.
	skillsRaw, err := os.ReadFile(filepath.Join(out.Dir, "skills.json"))
	if err != nil {
		t.Fatalf("skills.json missing: %v", err)
	}
	var rec struct {
		Mode       string `json:"mode"`
		ErrorCards bool   `json:"error_cards"`
		Suggested  []struct {
			Name string `json:"name"`
		} `json:"suggested"`
	}
	if err := json.Unmarshal(skillsRaw, &rec); err != nil {
		t.Fatalf("skills.json invalid: %v", err)
	}
	if rec.Mode != "cards" || !rec.ErrorCards || len(rec.Suggested) != 0 {
		t.Fatalf("skills.json must record cards/error_cards/no-suggestions, got %+v", rec)
	}
}

// The error-cards switch silences excerpts without changing the
// mode: the run still injects nothing and the records pin the
// switch off.
func TestExecEngineSkillsCardsErrorCardsOff(t *testing.T) {
	t.Setenv("EXCALIBUR_SKILLS", "cards")
	t.Setenv("EXCALIBUR_SKILLS_ERROR_CARDS", "0")
	// Legacy quick-pause path: the run never calls `finish`, so keep
	// the small step cap to avoid the 2000-step safety wait.
	t.Setenv("EXCALIBUR_TIME_BUDGET_ONLY", "0")
	dir := t.TempDir()
	fake := providerfake.New("e2e-cards-off")
	makeArgs, _ := json.Marshal(map[string]string{"command": "make -C /tmp/definitely-missing-xyz-cards", "workdir": dir})
	fake.Responses = []providers.ChatResponse{
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "exec", Arguments: makeArgs}}}},
		{Message: providers.Message{Role: providers.RoleAssistant, Content: "done"}},
	}
	logs := t.TempDir()
	out, err := RunExec(context.Background(), ExecOptions{
		Request: "build the project with make", LogsDir: logs,
		Profile: ProfileEval, ProjectDir: dir, Provider: fake, MaxSteps: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out.ATIFPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[skill card — error-triggered]") {
		t.Error("error_cards=0 must silence excerpts")
	}
	skillsRaw, err := os.ReadFile(filepath.Join(out.Dir, "skills.json"))
	if err != nil {
		t.Fatalf("skills.json missing: %v", err)
	}
	var rec struct {
		Mode       string `json:"mode"`
		ErrorCards bool   `json:"error_cards"`
	}
	if err := json.Unmarshal(skillsRaw, &rec); err != nil {
		t.Fatalf("skills.json invalid: %v", err)
	}
	if rec.Mode != "cards" || rec.ErrorCards {
		t.Fatalf("skills.json must record cards with error_cards off, got %+v", rec)
	}
}

var _ engine.ToolRunner = (*skillCardsRunner)(nil)

// execArgs builds the JSON arguments of one exec call.
func execArgs(t *testing.T, command string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const sqliteMissing = "exit: 127  duration: 0.0s  session: sess-1  alive: false\n/usr/bin/bash: line 1: sqlite3: command not found\n"

// fires runs one exec result through a fresh or given runner and
// reports whether the error card was appended.
func fires(t *testing.T, r *skillCardsRunner, command, output string) bool {
	t.Helper()
	r.base = &stubRunner{res: tools.Result{Status: tools.StatusOK, Output: output}}
	got, err := r.Execute(context.Background(), "exec", execArgs(t, command), tools.ExecuteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(got.Output, "[skill card — error-triggered]")
}

// Strict card rules (EXCALIBUR_CARDS_STRICT, default on): probes,
// successful installs of the named binary, and repeats within a run
// never fire a card.
func TestSkillCardsStrictRules(t *testing.T) {
	t.Setenv("EXCALIBUR_CARDS_STRICT", "")
	ix := cardsTestIndex(t)
	newRunner := func() *skillCardsRunner {
		return &skillCardsRunner{index: ix, errorCards: true}
	}

	t.Run("probe command never fires", func(t *testing.T) {
		probeOut := "exit: 127  duration: 0.0s  session: sess-1  alive: false\nsqlite3: command not found\n"
		for _, cmd := range []string{"command -v sqlite3", "which sqlite3 || true", "test -x /usr/bin/sqlite3", "grep -q sqlite3 /etc/x"} {
			if fires(t, newRunner(), cmd, probeOut) {
				t.Errorf("probe %q must not fire a card", cmd)
			}
		}
		if !fires(t, newRunner(), "sqlite3 app.db .tables", probeOut) {
			t.Error("a real command with the same error must still fire")
		}
		if !fires(t, newRunner(), "test -f app.db && sqlite3 app.db .tables", probeOut) {
			t.Error("a compound with a non-probe segment is not a probe")
		}
	})

	t.Run("successful install of the same binary suppresses the card", func(t *testing.T) {
		installed := sqliteMissing + "provision: detected missing command 'sqlite3' (evidence: command not found).\n" +
			"provision: installed sqlite3 via apt (exit 0, 3.1s). output tail: ok\n"
		if fires(t, newRunner(), "sqlite3 app.db .tables", installed) {
			t.Error("a card must not follow a successful install of the same binary")
		}
		verified := sqliteMissing + "provision: verified working command: `command -v sqlite3`. re-run your original command explicitly:\nsqlite3 app.db .tables\n"
		if fires(t, newRunner(), "sqlite3 app.db .tables", verified) {
			t.Error("a card must not follow a verified working command for the same binary")
		}
	})

	t.Run("install of another binary does not suppress", func(t *testing.T) {
		other := sqliteMissing + "provision: installed jq via apt (exit 0, 1.0s). output tail: ok\n"
		if !fires(t, newRunner(), "sqlite3 app.db .tables", other) {
			t.Error("an unrelated install must not suppress the sqlite card")
		}
	})

	t.Run("once per card name per run", func(t *testing.T) {
		r := newRunner()
		if !fires(t, r, "sqlite3 a.db .tables", sqliteMissing) {
			t.Fatal("first failure must fire the card")
		}
		if fires(t, r, "sqlite3 b.db .tables", sqliteMissing) {
			t.Error("the same card name must not fire twice in one run")
		}
		if !fires(t, newRunner(), "sqlite3 b.db .tables", sqliteMissing) {
			t.Error("a new run has its own once-per-name record")
		}
	})

	t.Run("switch off restores legacy firing", func(t *testing.T) {
		t.Setenv("EXCALIBUR_CARDS_STRICT", "off")
		r := newRunner()
		probeOut := "exit: 127  duration: 0.0s  session: sess-1  alive: false\nsqlite3: command not found\n"
		if !fires(t, r, "command -v sqlite3", probeOut) {
			t.Error("legacy rule fires on probes")
		}
		if !fires(t, r, "sqlite3 a.db .tables", sqliteMissing) || !fires(t, r, "sqlite3 b.db .tables", sqliteMissing) {
			t.Error("legacy rule has no once-per-run record")
		}
	})
}

func TestStrictCardsRequireNumericFailureHeader(t *testing.T) {
	t.Setenv("EXCALIBUR_CARDS_STRICT", "on")
	for _, output := range []string{
		"exit: 0  alive: false\nsqlite3: command not found\n",
		"exit: unknown  alive: false\nsqlite3: command not found\n",
		"sqlite3: command not found\n",
		"exit: 0  alive: false\nexit: 127  alive: false\nsqlite3: command not found\n",
	} {
		r := &skillCardsRunner{base: &stubRunner{res: tools.Result{Status: tools.StatusToolError, Output: output}}, index: cardsTestIndex(t), errorCards: true}
		result, err := r.Execute(context.Background(), "exec", json.RawMessage(`{"command":"sqlite3 app.db .tables"}`), tools.ExecuteOptions{})
		if err != nil || strings.Contains(result.Output, "[skill card") {
			t.Fatalf("unexpected card/error: %q, %v", result.Output, err)
		}
	}
}
