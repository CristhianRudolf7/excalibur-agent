package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, out string }{
		{"[[doc:d#c1]]", "[[doc:d#c1]]"},
		{"[doc: d # c1]", "[[doc:d#c1]]"},
		{"【doc:d#c1】", "[[doc:d#c1]]"},
		{"(doc: d#c1)", "[[doc:d#c1]]"},
		{"［［ doc: d # c1 ］］", "[[doc:d#c1]]"},
		{"[doc:d#c1, c3; #c5]", "[[doc:d#c1]][[doc:d#c3]][[doc:d#c5]]"},
		{"[doc:d#c1, d#c3; doc: e # c5]", "[[doc:d#c1]][[doc:d#c3]][[doc:e#c5]]"},
		{"[doc:d#c1–c3]", "[[doc:d#c1]][[doc:d#c2]][[doc:d#c3]]"},
		{"[doc:d#c1-3]", "[[doc:d#c1]][[doc:d#c2]][[doc:d#c3]]"},
		{"[ordinary] (text)", "[ordinary] (text)"},
		{"[doc: d # c\t1]", "[[doc:d#c1]]"},
	} {
		if got := Normalize(tc.in); got != tc.out {
			t.Errorf("Normalize(%q)=%q want %q", tc.in, got, tc.out)
		}
		if got := Normalize(tc.out); got != tc.out {
			t.Errorf("not idempotent: %q", got)
		}
	}
	if strings.Count(Normalize("[doc:d#c1-c99999999]"), "[[") != 1 {
		t.Fatal("unbounded range")
	}
}

func TestStreamHold(t *testing.T) {
	var h StreamHold
	for _, tc := range []struct{ in, out string }{{"hello [", "hello "}, {"[doc:d#", ""}, {"c1]", ""}, {"] done", "[[doc:d#c1]] done"}} {
		if got := h.Write(tc.in); got != tc.out {
			t.Fatalf("Write(%q)=%q want %q", tc.in, got, tc.out)
		}
	}
	if got := h.Write("[[" + strings.Repeat("界", 198)); got != "" {
		t.Fatal("released 200-rune marker")
	}
	if got := h.Write("x"); utf8Len(got) != 201 {
		t.Fatalf("did not release oversized marker: %q", got)
	}
	h.Write("[[tail")
	if h.Flush() != "[[tail" || h.Flush() != "" {
		t.Fatal("flush")
	}
}
func utf8Len(s string) int { return len([]rune(s)) }

type citeLibraryFake struct {
	tree []string
	fail bool
}

func (f *citeLibraryFake) ValidateCitations(_ context.Context, refs, tree []string) ([]Citation, []string) {
	f.tree = tree
	var valid []Citation
	var dropped []string
	for _, ref := range refs {
		d, c, ok := ParseCitationRef(ref)
		if !ok || c == "bad" || len(tree) == 0 {
			dropped = append(dropped, ref)
		} else {
			valid = append(valid, Citation{DocID: d, ChunkID: c})
		}
	}
	return valid, dropped
}
func (f *citeLibraryFake) CitationSource(_ context.Context, d, c string) (SourceRef, error) {
	if f.fail {
		return SourceRef{}, errors.New("gone")
	}
	return SourceRef{Title: "Book", Excerpt: strings.Repeat("界", 301)}, nil
}
func (f *citeLibraryFake) AnswerSources(context.Context, []string) ([]SourceEvent, error) {
	return []SourceEvent{{URL: "https://example.test/a", Title: "Page"}, {URL: "https://example.test/a"}}, nil
}

func TestCiteFlow(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "on")
	lib := &citeLibraryFake{}
	flow := CiteFlow{Library: lib}
	text, sources, dropped := flow.Process(context.Background(), "One [doc:d#z]. Two [[doc:d#a]][[doc:d#z]]. Bad [[doc:d#bad]]. Mixed [[doc:d#bad]][[doc:d#a]].", []string{"parent", "child"})
	want := "One [1]. Two [2][1]. Bad. [unsourced] Mixed [2]."
	if text != want {
		t.Fatalf("text=%q want %q", text, want)
	}
	if len(sources) != 3 || sources[0].ChunkID != "z" || sources[1].N != 2 || sources[2].Kind != "web" || len([]rune(sources[0].Excerpt)) != 300 || len(dropped) != 2 || len(lib.tree) != 2 {
		t.Fatalf("sources=%+v dropped=%v", sources, dropped)
	}
	text, _, _ = flow.Process(context.Background(), "Bad. [[doc:d#bad]] Next.", nil)
	if text != "Bad. [unsourced] Next." {
		t.Fatalf("post-punctuation refs: %q", text)
	}
	lib.fail = true
	text, sources, dropped = flow.Process(context.Background(), "Gone [[doc:d#a]].", []string{"run"})
	if !strings.Contains(text, "[unsourced]") || len(dropped) != 1 || len(sources) != 1 {
		t.Fatalf("metadata failure: %s %+v %v", text, sources, dropped)
	}
}
func TestCiteFlowSwitchOff(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "false")
	in := "Claim [doc:d#bad]."
	text, sources, dropped := (CiteFlow{}).Process(context.Background(), in, nil)
	if text != in || sources != nil || dropped != nil {
		t.Fatal("off path changed output")
	}
}

func TestCiteFlowDropsMalformedAndUnretrievedReferences(t *testing.T) {
	t.Setenv("EXCALIBUR_CITATIONS_PIPELINE", "on")
	text, sources, dropped := (CiteFlow{}).Process(context.Background(), "Unknown [doc:missing]. Empty [[doc:d#]].", nil)
	if text != "Unknown. [unsourced] Empty. [unsourced]" || len(sources) != 0 || len(dropped) != 2 {
		t.Fatalf("%q %+v %v", text, sources, dropped)
	}
	// Each bullet is a separate claim, including a ref placed after punctuation.
	flow := CiteFlow{Library: &citeLibraryFake{}}
	text, _, _ = flow.Process(context.Background(), "- One [[doc:d#c1]].\n- Bad. [[doc:d#bad]]\n- Mixed [[doc:d#bad]][[doc:d#c1]].", []string{"run"})
	if text != "- One [1].\n- Bad. [unsourced]\n- Mixed [1]." {
		t.Fatalf("bullets %q", text)
	}
}
