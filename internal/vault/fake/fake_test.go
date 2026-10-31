package fake

import (
	"context"
	"strings"
	"testing"
)

func TestFake_PolicyAndRedact(t *testing.T) {
	v := New()
	v.Set("tok", "fake-value-123")
	v.SetPolicy("tok", []string{"proj-a"}, []string{"exec"}, nil)
	ctx := context.Background()
	if _, err := v.Resolve(ctx, "secret://tok", "exec", "proj-a", ""); err != nil {
		t.Fatalf("own scope: %v", err)
	}
	if _, err := v.Resolve(ctx, "tok", "exec", "proj-b", ""); err == nil {
		t.Fatal("other project resolved")
	}
	if _, err := v.Resolve(ctx, "tok", "browser", "proj-a", ""); err == nil {
		t.Fatal("disallowed tool resolved")
	}
	v.SetLocked(true)
	if _, err := v.Resolve(ctx, "tok", "exec", "proj-a", ""); err == nil {
		t.Fatal("locked fake resolved")
	}
	v.SetLocked(false)
	if got := v.Redact("x fake-value-123 y"); !strings.Contains(got, "‹secret:tok›") {
		t.Fatalf("redact = %q", got)
	}
	if v.Uses("tok") != 1 {
		t.Fatalf("uses = %d", v.Uses("tok"))
	}
}
