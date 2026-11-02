package workspace

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestChangesNetAndAttribution(t *testing.T) {
	h := NewHistory()
	h.Record("/work/a", []byte("one\ntwo\n"), []byte("one\nthree\n"), "child", "")
	h.Record("/work/a", []byte("one\nthree\n"), []byte("one\nfinal\nextra\n"), "child", "")
	h.Record("/work/unrelated", nil, []byte("secret"), "other-turn", "")
	h.Record("/work/same", []byte("start"), []byte("changed"), "master", "")
	h.Record("/work/same", []byte("changed"), []byte("start"), "master", "")
	h.Record("/work/deleted", []byte("gone\n"), nil, "master", "")
	h.Record("/work/empty", nil, []byte{}, "master", "")
	h.Record("/work/new", nil, []byte("new\n"), "master", "")
	h.Record("/work/temporary", nil, []byte("temp"), "master", "")
	h.Record("/work/temporary", []byte("temp"), nil, "master", "")
	changes := h.Changes([]string{"master", "child"}, []string{"/work"})
	if len(changes) != 4 {
		t.Fatalf("changes: %+v", changes)
	}
	a := changes[0]
	if a.Path != "a" || a.Status != "modified" || a.Added != 2 || a.Removed != 1 || *a.Content != "one\nfinal\nextra\n" {
		t.Fatalf("net: %+v", a)
	}
	if a.Hunks[0].OldStart != 1 || a.Hunks[0].OldLines != 2 || a.Hunks[0].NewLines != 3 || strings.Join(a.Hunks[0].Lines, "\n") != " one\n-two\n+final\n+extra" {
		t.Fatalf("hunk: %+v", a.Hunks)
	}
	if changes[1].Status != "deleted" || changes[1].Content != nil || changes[1].Removed != 1 || changes[1].Hunks[0].NewStart != 0 {
		t.Fatalf("deleted: %+v", changes[1])
	}
	if changes[2].Status != "added" || changes[2].Content == nil || *changes[2].Content != "" {
		t.Fatalf("empty creation: %+v", changes[2])
	}
	if changes[3].Added != 1 || changes[3].Hunks[0].OldStart != 0 {
		t.Fatalf("added: %+v", changes[3])
	}
	if len(h.Changes(nil, nil)) != 0 {
		t.Fatal("empty tree leaked edits")
	}
}

func TestChangesLimits(t *testing.T) {
	h := NewHistory()
	for i := 0; i < 55; i++ {
		h.Record(fmt.Sprintf("/work/%02d", i), nil, []byte("x"), "run", "")
	}
	c := h.Changes([]string{"run"}, nil)
	if len(c) != 50 || c[0].Path != "/work/00" || c[49].Path != "/work/49" {
		t.Fatalf("file cap: %+v", c)
	}
	h = NewHistory()
	var pre, post strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&pre, "line %d\n", i)
		if i%10 == 0 {
			fmt.Fprintf(&post, "changed %d\n", i)
		} else {
			fmt.Fprintf(&post, "line %d\n", i)
		}
	}
	h.Record("many", []byte(pre.String()), []byte(post.String()), "run", "")
	h.Record("large", nil, []byte(strings.Repeat("x", 200*1024+1)), "run", "")
	c = h.Changes([]string{"run"}, nil)
	if c[0].Content != nil || !c[0].Truncated || len(c[0].Hunks) != 0 {
		t.Fatalf("content/hunk bytes cap: %+v", c[0])
	}
	if len(c[1].Hunks) != 40 || !c[1].Truncated || c[1].Added != 50 || c[1].Removed != 50 {
		t.Fatalf("hunk cap: %+v", c[1])
	}
}

func TestChangesFinalNewlineAndPath(t *testing.T) {
	h := NewHistory()
	h.Record("/work/sub/a", []byte("x"), []byte("x\n"), "run", "")
	c := h.Changes([]string{"run"}, []string{"/work", "/work/sub"})
	if c[0].Path != "a" || c[0].Added != 1 || c[0].Removed != 1 {
		t.Fatalf("newline/path: %+v", c)
	}
}

func TestBinaryChanges(t *testing.T) {
	for _, tc := range []struct {
		name      string
		pre, post []byte
		binary    bool
	}{
		{"utf8", []byte("old"), []byte("new λ"), false},
		{"nul", nil, []byte("a\x00b"), true},
		{"invalid", []byte{255}, []byte("text"), true},
		{"deleted", []byte{0}, nil, true},
		{"late-invalid", nil, append(bytes.Repeat([]byte("a"), 8192), 255), true},
		{"late-nul", nil, append(bytes.Repeat([]byte("a"), 8192), 0), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHistory()
			h.Record(tc.name, tc.pre, tc.post, "run", "")
			changes := h.Changes([]string{"run"}, nil)
			if len(changes) != 1 {
				t.Fatalf("%+v", changes)
			}
			c := changes[0]
			if c.Binary != tc.binary {
				t.Fatalf("%+v", c)
			}
			if c.Binary && (c.Content != nil || len(c.Hunks) != 0 || c.Added != 0 || c.Removed != 0) {
				t.Fatalf("binary diff leaked: %+v", c)
			}
		})
	}
}
