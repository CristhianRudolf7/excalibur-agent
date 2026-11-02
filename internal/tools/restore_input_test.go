package tools

import (
	"context"
	"testing"
)

func TestRestoreInputTool(t *testing.T) {
	called := false
	tool := &RestoreInputTool{Restore: func(p, d string) error { called = p == "input.dat" && d == "scratch.dat"; return nil }}
	if err := tool.Definition().Validate(); err != nil {
		t.Fatal(err)
	}
	res, err := tool.Invoke(context.Background(), Invocation{Arguments: []byte(`{"path":"input.dat","destination":"scratch.dat"}`)})
	if err != nil || res.Status != StatusOK || !called {
		t.Fatal(res, err)
	}
	t.Setenv("EXCALIBUR_INPUT_RESTORE", "off")
	called = false
	res, _ = tool.Invoke(context.Background(), Invocation{Arguments: []byte(`{"path":"input.dat"}`)})
	if res.Status == StatusOK || called {
		t.Fatal("disabled restore executed")
	}
}
