package vault

import (
	"context"
	"strings"
	"testing"
)

// TestDetect_ChatPaste covers 20.2 row 4: pasting an API key and a
// password in a chat message. The persisted message holds no values,
// the user gets a keep question (draft), and discarding removes it.
func TestDetect_ChatPaste(t *testing.T) {
	msg := "hey, my key is ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123 and\npassword: S3cret-Banco-99\nthanks"
	sanitized, findings := SanitizeMessage(msg)
	if len(findings) < 2 {
		t.Fatalf("expected >=2 findings, got %d", len(findings))
	}
	for _, f := range findings {
		if strings.Contains(sanitized, f.Value) {
			t.Fatalf("sanitized message leaks %q", f.Value)
		}
	}
	if !strings.Contains(sanitized, "‹secret:") {
		t.Fatalf("no markers in sanitized message: %q", sanitized)
	}

	v, _ := testVault(t, nil)
	ctx := context.Background()
	var ids []string
	for _, f := range findings {
		id, err := v.SaveDraft(ctx, []byte(f.Value), "chat-paste")
		if err != nil {
			t.Fatalf("save draft: %v", err)
		}
		ids = append(ids, id)
	}
	drafts, err := v.ListDrafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != len(ids) {
		t.Fatalf("drafts = %d, want %d", len(drafts), len(ids))
	}
	// Values must not appear in drafts table dump.
	dump := dumpTables(t, v)
	for _, f := range findings {
		if strings.Contains(dump, f.Value) {
			t.Fatalf("draft value in clear in db: %q", f.Value)
		}
	}
	// Confirm the first draft under a name: the value lands encrypted.
	md, err := v.ConfirmDraft(ctx, ids[0], SetParams{Name: "pasted-key", Type: TypeAPIKey})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if md.Name != "pasted-key" {
		t.Fatalf("confirm name = %q", md.Name)
	}
	res, err := v.Resolve(ctx, ResolveRequest{Ref: "secret://pasted-key", Tool: "exec"})
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Value) != findings[0].Value {
		t.Fatal("confirmed draft value mismatch")
	}
	ZeroBytes(res.Value)
	// Discard the second: it disappears entirely.
	if err := v.DiscardDraft(ctx, ids[1]); err != nil {
		t.Fatal(err)
	}
	drafts, _ = v.ListDrafts(ctx)
	if len(drafts) != 0 {
		t.Fatalf("drafts after confirm+discard = %d, want 0", len(drafts))
	}
}

// TestDetect_LockedDraft covers drafts captured while locked: held in
// memory, encrypted on unlock.
func TestDetect_LockedDraft(t *testing.T) {
	v, _ := testVaultLocked(t)
	ctx := context.Background()
	id, err := v.SaveDraft(ctx, []byte("ghp_LOCKEDDRAFTKEY0123456789ABCD"), "chat-paste")
	if err != nil {
		t.Fatalf("locked save: %v", err)
	}
	drafts, err := v.ListDrafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != id {
		t.Fatalf("pending drafts = %+v", drafts)
	}
	if err := v.Unlock(ctx, nil); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	drafts, err = v.ListDrafts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 {
		t.Fatalf("drafts after unlock = %d, want 1 (moved to encrypted table)", len(drafts))
	}
	if err := v.DiscardDraft(ctx, id); err != nil {
		t.Fatal(err)
	}
}

// TestDetect_NoSecrets passes clean text through untouched.
func TestDetect_NoSecrets(t *testing.T) {
	msg := "please list the project files and run the tests"
	sanitized, findings := SanitizeMessage(msg)
	if len(findings) != 0 {
		t.Fatalf("findings = %+v", findings)
	}
	if sanitized != msg {
		t.Fatalf("clean message altered: %q", sanitized)
	}
}
