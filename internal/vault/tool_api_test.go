package vault

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretsTool_NeverValues drives every secrets-tool op and proves
// no result carries a secret value.
func TestSecretsTool_NeverValues(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "tool-key", Type: TypeAPIKey, Value: []byte("tool-key-value-007")}); err != nil {
		t.Fatal(err)
	}
	id, err := v.SaveDraft(ctx, []byte("ghp_TOOLCHATDRAFT01234567890123"), "chat-paste")
	if err != nil {
		t.Fatal(err)
	}
	ops := []SecretsArgs{
		{Op: SecretsOpList},
		{Op: SecretsOpDescribe, Name: "tool-key"},
		{Op: SecretsOpGenerate, Name: "gen-key", Type: TypeToken, Length: 24},
		{Op: SecretsOpRequest, Name: "new-key", Type: TypeAPIKey},
		{Op: SecretsOpRequestChange, Name: "tool-key", Scopes: []string{"proj-z"}},
		{Op: SecretsOpRequestDelete, Name: "tool-key"},
		{Op: SecretsOpUsage, Name: "tool-key"},
		{Op: SecretsOpListDrafts},
		{Op: SecretsOpRedact, Text: "has tool-key-value-007 inside"},
		{Op: SecretsOpConfirmDraft, DraftID: id, Name: "chat-key", Type: TypeAPIKey},
	}
	for _, args := range ops {
		res, err := v.Dispatch(ctx, args)
		if err != nil {
			t.Fatalf("op %s: %v", args.Op, err)
		}
		raw := MarshalResult(res)
		for _, secret := range []string{"tool-key-value-007", "ghp_TOOLCHATDRAFT01234567890123"} {
			if strings.Contains(raw, secret) {
				t.Fatalf("op %s leaked a value", args.Op)
			}
		}
	}
	// Generated entry exists with metadata only in the result.
	got, err := v.Describe(ctx, "gen-key")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != TypeToken {
		t.Fatalf("gen type = %q", got.Type)
	}
	if _, err := v.Dispatch(ctx, SecretsArgs{Op: "nope"}); err == nil {
		t.Fatal("unknown op must fail")
	}
}

// TestAPI_Core drives the vault.* handler core: create/update/list/
// describe/lock/unlock/delete/reveal/edit/rotate/usage/generate, with
// the agent-blind reveal rule (Reveal is user-terminal only).
func TestAPI_Core(t *testing.T) {
	v, _ := testVault(t, nil)
	ctx := context.Background()
	md, err := v.Create(ctx, CreateParams{Name: "api-key", Type: TypeAPIKey, Value: []byte("api-core-value-1"), Metadata: map[string]string{"service": "s"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if md.Name != "api-key" {
		t.Fatalf("name = %q", md.Name)
	}
	if _, err := v.Create(ctx, CreateParams{Name: "api-key", Type: TypeAPIKey, Value: []byte("dup")}); err == nil {
		t.Fatal("duplicate create must fail")
	}
	list, err := v.ListMetadata(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	md, err = v.Update(ctx, UpdateParams{Name: "api-key", Type: TypeAPIKey, Value: []byte("api-core-value-2")})
	if err != nil {
		t.Fatalf("update value: %v", err)
	}
	if md.EntryVersion != 2 {
		t.Fatalf("entry version = %d, want 2", md.EntryVersion)
	}
	if _, err := v.Update(ctx, UpdateParams{Name: "api-key", Type: TypeNote}); err == nil {
		t.Fatal("type change without value must fail")
	}
	scopes := []string{"proj-q"}
	if _, err := v.EditPolicy(ctx, "api-key", PolicyParams{Scopes: &scopes}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	val, err := v.Reveal(ctx, "api-key")
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if string(val) != "api-core-value-2" {
		t.Fatal("reveal mismatch")
	}
	ZeroBytes(val)
	uses, err := v.UsageOf(ctx, "api-key", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(uses) == 0 {
		t.Fatal("reveal should log a use")
	}
	if _, _, err := v.RotateKeys(ctx, nil); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	v.LockVault()
	if !v.IsLocked() {
		t.Fatal("expected locked")
	}
	if err := v.UnlockVault(ctx, nil); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if err := v.DeleteEntry(ctx, "api-key", false); err == nil {
		t.Fatal("unconfirmed delete must fail")
	}
	if err := v.DeleteEntry(ctx, "api-key", true); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := v.DescribeEntry(ctx, "api-key"); err == nil {
		t.Fatal("deleted entry still visible")
	}
	gmd, err := v.GenerateEntry(ctx, GenerateParams{Name: "gen-api", Length: 16})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if gmd.Name != "gen-api" {
		t.Fatalf("gen name = %q", gmd.Name)
	}
}

// TestInject_Paths covers env/header/file injection and ScrubArgs.
func TestInject_Paths(t *testing.T) {
	v, dir := testVault(t, nil)
	ctx := context.Background()
	if _, err := v.Set(ctx, SetParams{Name: "tok", Type: TypeToken, Value: []byte("inject-value-009")}); err != nil {
		t.Fatal(err)
	}
	env, cleanup, err := v.InjectEnv(ctx, map[string]string{"TOKEN": "secret://tok", "PLAIN": "x"}, "exec", "run_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if env["TOKEN"] != "inject-value-009" || env["PLAIN"] != "x" {
		t.Fatalf("env = %+v", env)
	}
	cleanup()
	hv, hcleanup, err := v.InjectHeader(ctx, "secret://tok", "exec", "run_1", "")
	if err != nil {
		t.Fatal(err)
	}
	if hv != "inject-value-009" {
		t.Fatal("header mismatch")
	}
	hcleanup()
	path, fcleanup, err := v.InjectFile(ctx, "secret://tok", "exec", "run_1", "", filepath.Join(dir, "run"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "inject-value-009" {
		t.Fatal("file content mismatch")
	}
	assertPrivateFile(t, path)
	fcleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temp secret file not removed")
	}
	scrubbed := ScrubArgs(map[string]string{"TOKEN": "secret://tok", "PLAIN": "x"})
	if scrubbed["TOKEN"] != "‹secret:tok›" || scrubbed["PLAIN"] != "x" {
		t.Fatalf("scrubbed = %+v", scrubbed)
	}
	// Form policy through the check path.
	if _, err := v.CheckFormPolicy(ctx, "secret://tok", "browser", "run_1", "", "example.com"); err != nil {
		t.Fatalf("form check: %v", err)
	}
}

// TestGenerate_Basics covers length/charset validation.
func TestGenerate_Basics(t *testing.T) {
	for _, n := range []int{0, -1, 5000} {
		if _, err := Generate(n, ""); err == nil {
			t.Fatalf("length %d must fail", n)
		}
	}
	b, err := Generate(32, "")
	if err != nil || len(b) != 32 {
		t.Fatalf("default generate: %v %d", err, len(b))
	}
	ZeroBytes(b)
	b, err = Generate(16, "ab")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range string(b) {
		if c != 'a' && c != 'b' {
			t.Fatalf("charset violated: %q", string(b))
		}
	}
	ZeroBytes(b)
	if _, err := Generate(16, "x"); err == nil {
		t.Fatal("tiny charset must fail")
	}
}
