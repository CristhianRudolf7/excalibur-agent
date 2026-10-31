package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func validDefinition() Definition {
	return Definition{
		Name:        "read_file",
		Description: "read a file window",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Effect:      "read_only",
		Idempotency: "pure",
		Cancel:      "immediate",
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"read_file", "exec", "mcp__srv__tool", "a", "x1_y2"} {
		if err := ValidateName(ok); err != nil {
			t.Fatalf("name %q should validate: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Read", "read-file", "read.file", "1abc", "a!", "with space", string(make([]byte, 65))} {
		if err := ValidateName(bad); err == nil {
			t.Fatalf("name %q should not validate", bad)
		}
	}
	long := make([]byte, 64)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateName(string(long)); err != nil {
		t.Fatalf("64-char name should validate: %v", err)
	}
}

func TestDefinitionValidate(t *testing.T) {
	if err := validDefinition().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []func(*Definition){
		func(d *Definition) { d.Name = "Bad" },
		func(d *Definition) { d.Description = "" },
		func(d *Definition) { d.InputSchema = nil },
		func(d *Definition) { d.Effect = "nuke" },
		func(d *Definition) { d.Idempotency = "maybe" },
		func(d *Definition) { d.Cancel = "eventually" },
	}
	for i, c := range cases {
		d := validDefinition()
		c(&d)
		if err := d.Validate(); err == nil {
			t.Fatalf("case %d should not validate", i)
		}
	}
}

func TestMapRegistry(t *testing.T) {
	r := NewMapRegistry()
	if got := r.Names(); len(got) != 0 {
		t.Fatal("empty registry")
	}
	if _, err := r.Get("missing"); err == nil {
		t.Fatal("expected unknown tool error")
	}
	bad := validDefinition()
	bad.Name = "Bad"
	if err := r.Register(staticTool{def: bad}); err == nil {
		t.Fatal("expected invalid definition error")
	}
	if err := r.Register(staticTool{def: validDefinition()}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(staticTool{def: validDefinition()}); err == nil {
		t.Fatal("expected duplicate error")
	}
	if _, err := r.Get("read_file"); err != nil {
		t.Fatal(err)
	}
}

type staticTool struct{ def Definition }

func (s staticTool) Definition() Definition { return s.def }

func (s staticTool) Invoke(_ context.Context, _ Invocation) (Result, error) {
	return Result{}, nil
}

func (s staticTool) ResourceKeys(_ json.RawMessage) ([]string, error) {
	return nil, nil
}
