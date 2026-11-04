package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the file-tool reliability items: the fingerprint guard,
// edit diagnostics, write_file content handling, structured-format
// checks, run-workdir path resolution and the vision backend contract.

func fileCall(t *testing.T, tool Tool, args map[string]any) Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: raw})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// readThenExternalChange reads path through the shared deps, then
// rewrites it outside the run, so the recorded fingerprint is stale.
func readThenExternalChange(t *testing.T, deps *FileDeps, path, external string) string {
	t.Helper()
	if res := fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path}); res.Status != StatusOK {
		t.Fatalf("read: %+v", res)
	}
	if err := os.WriteFile(path, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	return external
}

func TestWriteWithoutExpectedFPAcceptsRecordedState(t *testing.T) {
	path := writeTmp(t, "v1")
	deps := newFileTestDeps()
	if res := fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path}); res.Status != StatusOK {
		t.Fatalf("read: %+v", res)
	}
	// The file is unchanged since this run read it: no expected_fp needed.
	if res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "v2"}); res.Status != StatusOK {
		t.Fatalf("write over recorded state must apply: %+v", res)
	}
	// Consecutive writes keep working because each write records its result.
	if res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "v3"}); res.Status != StatusOK {
		t.Fatalf("second write must apply: %+v", res)
	}
}

func TestExplicitStaleFPWithUnchangedFileIsAccepted(t *testing.T) {
	path := writeTmp(t, "v1")
	deps := newFileTestDeps()
	fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path})
	// A typed or hand-copied fingerprint that does not match, while the
	// file still equals what the harness recorded, must not churn.
	res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "v2", "expected_fp": "deadbeef"})
	if res.Status != StatusOK {
		t.Fatalf("explicit stale fp over an unchanged file must apply: %+v", res)
	}
}

func TestPlaceholderFingerprintsCountAsAbsent(t *testing.T) {
	for _, placeholder := range []string{"*", "**", "UNAVAILABLE", "none", "unknown", "-"} {
		path := writeTmp(t, "v1")
		deps := newFileTestDeps()
		fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path})
		res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "v1", "new_text": "v2", "expected_fp": placeholder})
		if res.Status != StatusOK {
			t.Fatalf("placeholder %q over an unchanged file must apply: %+v", placeholder, res)
		}
	}
}

func TestCleanFingerprint(t *testing.T) {
	if CleanFingerprint("  ") != "" || CleanFingerprint("UNAVAILABLE") != "" || CleanFingerprint("*") != "" {
		t.Fatal("empty and placeholder values must clean to no fingerprint")
	}
	if got := CleanFingerprint(" abc123 "); got != "abc123" {
		t.Fatalf("real fingerprint must pass through trimmed, got %q", got)
	}
}

func TestExternalChangeRefusalGivesRetryFingerprint(t *testing.T) {
	path := writeTmp(t, "v1")
	deps := newFileTestDeps()
	readThenExternalChange(t, deps, path, "v2-external\nmore\n")
	res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "mine"})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "changed since read") {
		t.Fatalf("external change must refuse: %+v", res)
	}
	if !strings.Contains(res.Output, "v2-external") {
		t.Fatalf("refusal must show the current first lines: %q", res.Output)
	}
	current := FingerprintOf([]byte("v2-external\nmore\n"))
	if !strings.Contains(res.Output, `expected_fp "`+string(current)+`"`) {
		t.Fatalf("refusal must name the retry fingerprint: %q", res.Output)
	}
	// One retry with the named fingerprint goes through.
	retry := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "mine", "expected_fp": string(current)})
	if retry.Status != StatusOK {
		t.Fatalf("retry with the named fingerprint must apply: %+v", retry)
	}
}

func TestEditExternalChangeIsRefusedUnlessNamed(t *testing.T) {
	path := writeTmp(t, "alpha\nbeta\n")
	deps := newFileTestDeps()
	readThenExternalChange(t, deps, path, "alpha\nchanged\n")
	res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "alpha", "new_text": "A"})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "changed since read") {
		t.Fatalf("edit over an external change must refuse: %+v", res)
	}
}

func TestFingerprintSwitchOffRestoresExplicitCheck(t *testing.T) {
	t.Setenv("EXCALIBUR_FP_AUTO", "off")
	path := writeTmp(t, "v1")
	deps := newFileTestDeps()
	fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path})
	// Legacy: an explicit fingerprint that differs from the file refuses,
	// even when the file equals the harness's recorded state.
	res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "v2", "expected_fp": "deadbeef"})
	if res.Status != StatusToolError {
		t.Fatalf("legacy explicit mismatch must refuse: %+v", res)
	}
	// Legacy: the explicit "*" wildcard still skips the check with a note.
	res = fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": path, "content": "v2", "expected_fp": "*"})
	if res.Status != StatusOK || !strings.Contains(res.Output, "skipped") {
		t.Fatalf("legacy wildcard must skip with a note: %+v", res)
	}
}

func TestEditMissReportsNearestLines(t *testing.T) {
	content := "alpha beta\ngamma delta\nfunc main() {\n    fmt.Println(\"hi\")\n}\n"
	path := writeTmp(t, content)
	deps := newFileTestDeps()
	res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "fmt.Println(\"hello\")", "new_text": "x"})
	if res.Status != StatusToolError {
		t.Fatalf("miss must fail: %+v", res)
	}
	if !strings.Contains(res.Output, "line 4:") || !strings.Contains(res.Output, "most similar lines") {
		t.Fatalf("miss must name the most similar line with its number: %q", res.Output)
	}
	if strings.Count(res.Output, "\n  line ") > 3 {
		t.Fatalf("at most three similar lines: %q", res.Output)
	}
}

func TestEditAmbiguousListsEveryLineAndOccurrenceSelects(t *testing.T) {
	path := writeTmp(t, "dup\nmid\ndup\n")
	deps := newFileTestDeps()
	res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "dup", "new_text": "x"})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "(lines 1, 3)") || !strings.Contains(res.Output, "occurrence") {
		t.Fatalf("ambiguous match must list lines and offer occurrence: %+v", res)
	}
	res = fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "dup", "new_text": "x", "occurrence": 2})
	if res.Status != StatusOK {
		t.Fatalf("occurrence 2 must edit the second match: %+v", res)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "dup\nmid\nx\n" {
		t.Fatalf("only the second occurrence may change: %q", data)
	}
}

func TestEditOccurrenceOutOfRangeAndSwitchOff(t *testing.T) {
	path := writeTmp(t, "dup\nmid\ndup\n")
	deps := newFileTestDeps()
	res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "dup", "new_text": "x", "occurrence": 5})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "out of range") {
		t.Fatalf("occurrence beyond the matches must refuse: %+v", res)
	}
	t.Setenv("EXCALIBUR_EDIT_HINTS", "off")
	res = fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "dup", "new_text": "x", "occurrence": 2})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "multiple matches at lines") {
		t.Fatalf("switch off keeps the earlier ambiguity wording and ignores occurrence: %+v", res)
	}
}

func TestRelativePathsResolveAgainstRunWorkdir(t *testing.T) {
	dir := t.TempDir()
	deps := newFileTestDeps()
	deps.Workdir = dir
	if res := fileCall(t, &WriteFileTool{Deps: deps}, map[string]any{"path": "sub/notes.txt", "content": "hello world"}); res.Status != StatusOK {
		t.Fatalf("write relative: %+v", res)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "sub", "notes.txt")); err != nil || string(data) != "hello world" {
		t.Fatalf("relative write must land in the workdir: %v %q", err, data)
	}
	if res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": "sub/notes.txt", "old_text": "world", "new_text": "run"}); res.Status != StatusOK {
		t.Fatalf("edit relative must find the same file: %+v", res)
	}
	if res := fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": "sub/notes.txt"}); res.Status != StatusOK || !strings.Contains(res.Output, "hello run") {
		t.Fatalf("read relative must see the edit: %+v", res)
	}
}

func TestResolvePathSwitchAndAbsolute(t *testing.T) {
	workdir := t.TempDir()
	deps := &FileDeps{Workdir: workdir}
	if got := deps.resolvePath("a/b.txt"); got != filepath.Join(workdir, "a/b.txt") {
		t.Fatalf("relative path must join the workdir, got %q", got)
	}
	absolute := filepath.Join(t.TempDir(), "x.txt")
	if got := deps.resolvePath(absolute); got != absolute {
		t.Fatalf("absolute path must be kept, got %q", got)
	}
	t.Setenv("EXCALIBUR_RUN_WORKDIR", "off")
	if got := deps.resolvePath("a/b.txt"); got != filepath.FromSlash("a/b.txt") {
		t.Fatalf("switch off keeps the process working directory, got %q", got)
	}
	var none *FileDeps
	if got := none.resolvePath("a/b.txt"); got != filepath.FromSlash("a/b.txt") {
		t.Fatalf("nil deps resolve against the process directory, got %q", got)
	}
}

func TestWriteFileObjectContentIsSerialized(t *testing.T) {
	raw := NormalizeArgs("write_file", json.RawMessage(`{"path":"cfg.json","content":{"name":"x","list":[1,2]}}`), DefaultSurfaceTuning())
	var got struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"name\": \"x\",\n  \"list\": [\n    1,\n    2\n  ]\n}"
	if got.Content != want {
		t.Fatalf("object content must be indented with its key order kept:\n%s", got.Content)
	}
	raw = NormalizeArgs("write_file", json.RawMessage(`{"path":"list.json","content":["a","b"]}`), DefaultSurfaceTuning())
	if err := json.Unmarshal(raw, &got); err != nil || got.Content != "[\n  \"a\",\n  \"b\"\n]" {
		t.Fatalf("array content must be serialized: %v %q", err, got.Content)
	}
}

func TestWriteFileObjectContentThroughExecutor(t *testing.T) {
	deps := newFileTestDeps()
	reg := NewMapRegistry()
	if err := reg.Register(&WriteFileTool{Deps: deps}); err != nil {
		t.Fatal(err)
	}
	ex := NewExecutor(reg)
	path := filepath.Join(t.TempDir(), "settings.json")
	raw, _ := json.Marshal(map[string]any{"path": path, "content": map[string]any{"debug": true}})
	res, err := ex.Execute(context.Background(), "write_file", raw, ExecuteOptions{})
	if err != nil || res.Status != StatusOK {
		t.Fatalf("object content must be written, not refused by the schema: %v %+v", err, res)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "{\n  \"debug\": true\n}" {
		t.Fatalf("file content: %q", data)
	}
}

func TestLiteralEscapeTOMLIsRefusedBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.toml")
	content := `title = "demo"\n[owner]\nname = "x"`
	res := fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": content})
	if res.Status != StatusInputError || !strings.Contains(res.Output, `content has literal \n sequences; send real newlines`) {
		t.Fatalf("literal escapes must be refused: %+v", res)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a refused write must not create the file")
	}
	// The same text with real newlines writes.
	res = fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": "title = \"demo\"\n[owner]\nname = \"x\"\n"})
	if res.Status != StatusOK {
		t.Fatalf("real newlines must write: %+v", res)
	}
}

func TestLiteralEscapeInsideValidJSONStringIsAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "msg.json")
	res := fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": `{"msg":"line one\nline two"}`})
	if res.Status != StatusOK || strings.Contains(res.Output, "warning") {
		t.Fatalf("a literal \\n that parses as written must be written: %+v", res)
	}
}

func TestStructuredWriteWarnsWhenParseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	res := fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": "{\n  \"a\": 1,\n}\n"})
	if res.Status != StatusOK {
		t.Fatalf("a broken file is still written: %+v", res)
	}
	if !strings.Contains(res.Output, "warning:") || !strings.Contains(res.Output, "does not parse as JSON") {
		t.Fatalf("a parse failure must add a warning line: %q", res.Output)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("the file must exist after a warned write")
	}
}

func TestStructuredWriteCheckSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_STRUCTURED_WRITE_CHECK", "off")
	path := filepath.Join(t.TempDir(), "video.toml")
	res := fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": `title = "demo"\n[owner]`})
	if res.Status != StatusOK {
		t.Fatalf("switch off writes without the check: %+v", res)
	}
}

func TestLiteralEscapeShellScriptRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.sh")
	res := fileCall(t, &WriteFileTool{Deps: newFileTestDeps()}, map[string]any{"path": path, "content": `if [ 1 ]; then echo x\nfi`})
	if res.Status != StatusInputError || !strings.Contains(res.Output, "send real newlines") {
		t.Fatalf("a shell script with literal escapes must be refused when sh can check it: %+v", res)
	}
}

func TestINIChecks(t *testing.T) {
	if err := checkINI("[core]\nkey = value\n; comment\n# other\n"); err != nil {
		t.Fatalf("valid ini: %v", err)
	}
	if err := checkINI("[core\nkey = value"); err == nil {
		t.Fatal("malformed section header must fail")
	}
	if err := checkINI("just words"); err == nil {
		t.Fatal("a line without a separator must fail")
	}
}

func TestVisionWithoutModelReturnsError(t *testing.T) {
	img := writeTemp(t, "shot.png", "fake-png-bytes")
	res := fileCall(t, &VisionExtractTool{Deps: &MediaDeps{}}, map[string]any{"input": img})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "vision_extract: no image model is configured in this run") {
		t.Fatalf("no model configured must be an explicit error: %+v", res)
	}
	if strings.Contains(res.Output, "placeholder") {
		t.Fatal("no placeholder text may be returned")
	}
}

func TestVisionSwitchOffRestoresPlaceholder(t *testing.T) {
	t.Setenv("EXCALIBUR_VISION_HONEST", "off")
	img := writeTemp(t, "shot.png", "fake-png-bytes")
	res := fileCall(t, &VisionExtractTool{Deps: &MediaDeps{}}, map[string]any{"input": img})
	if res.Status != StatusOK || !strings.Contains(res.Output, "placeholder") {
		t.Fatalf("switch off keeps the placeholder backend: %+v", res)
	}
}

func TestEditAfterDeletionSaysFileIsGone(t *testing.T) {
	path := writeTmp(t, "v1")
	deps := newFileTestDeps()
	fileCall(t, &ReadFileTool{Deps: deps}, map[string]any{"path": path})
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	res := fileCall(t, &EditFileTool{Deps: deps}, map[string]any{"path": path, "old_text": "v1", "new_text": "v2"})
	if res.Status != StatusToolError || !strings.Contains(res.Output, "no longer exists") || strings.Contains(res.Output, `expected_fp ""`) {
		t.Fatalf("a deleted file must be reported as gone, not as a fingerprint with an empty retry value: %+v", res)
	}
}
