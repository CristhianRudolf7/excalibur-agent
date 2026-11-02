package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/journal"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

func artifactFixture(t *testing.T, name string) tools.ArtifactRef {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name+".svg")
	source := filepath.Join(dir, name+".mmd")
	if err := os.WriteFile(path, []byte("<svg>"+name+"</svg>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("graph TD; A-->B"), 0600); err != nil {
		t.Fatal(err)
	}
	return tools.ArtifactRef{Kind: "diagram", Title: name, Path: path, Format: "svg", Dialect: "mermaid", SourcePath: source, Group: "overview"}
}

func TestArtifactCollectionOrderDedupeAndRestart(t *testing.T) {
	ctx := context.Background()
	store := journal.NewMemStore()
	s := &artifactStore{journal: store}
	a, b := artifactFixture(t, "first"), artifactFixture(t, "second")
	added, err := s.collect(ctx, "conv", "run", "call1", []tools.ArtifactRef{a, a, b})
	if err != nil || len(added) != 2 {
		t.Fatalf("added=%+v err=%v", added, err)
	}
	if added[0].Title != "first" || added[1].Title != "second" || added[0].ToolCallID != "call1" {
		t.Fatal(added)
	}
	s = &artifactStore{journal: store}
	added, err = s.collect(ctx, "conv", "run", "call2", []tools.ArtifactRef{b})
	if err != nil || len(added) != 0 {
		t.Fatalf("duplicate after restart=%+v %v", added, err)
	}
	records, err := s.records(ctx, "conv", "run")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v %v", records, err)
	}
	if records[0].Source != "graph TD; A-->B" || string(records[0].Content) != "<svg>first</svg>" {
		t.Fatal(records[0])
	}
	if records[0].Artifact.MIME != "image/svg+xml" || records[0].Artifact.Size != 16 || records[0].Artifact.CreatedAt == "" {
		t.Fatal(records[0].Artifact)
	}
	other, err := s.records(ctx, "other-conversation", "run")
	if err != nil || len(other) != 0 {
		t.Fatalf("other conversation: %+v %v", other, err)
	}
}

func TestArtifactCollectionLimits(t *testing.T) {
	ctx := context.Background()
	s := &artifactStore{journal: journal.NewMemStore()}
	var refs []tools.ArtifactRef
	for i := 0; i < 25; i++ {
		refs = append(refs, artifactFixture(t, fmt.Sprint(i)))
	}
	added, err := s.collect(ctx, "conv", "run", "call", refs)
	if err != nil || len(added) != maxTurnArtifacts {
		t.Fatalf("count=%d err=%v", len(added), err)
	}
	more, err := s.collect(ctx, "conv", "run", "next", []tools.ArtifactRef{artifactFixture(t, "extra")})
	if err != nil || len(more) != 0 {
		t.Fatalf("extra=%+v err=%v", more, err)
	}

	for _, test := range []struct {
		name            string
		content, source int
		want            int
	}{
		{"file", maxArtifactBytes + 1, 0, 0},
		{"source", 1, maxArtifactSourceBytes + 1, 0},
		{"turn", maxArtifactBytes, 0, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &artifactStore{journal: journal.NewMemStore()}
			var refs []tools.ArtifactRef
			for i := 0; i < 3; i++ {
				r := artifactFixture(t, fmt.Sprint(i))
				if err := os.WriteFile(r.Path, []byte(strings.Repeat("x", test.content)), 0600); err != nil {
					t.Fatal(err)
				}
				r.SourcePath = ""
				if test.source > 0 {
					r.SourcePath = filepath.Join(filepath.Dir(r.Path), "source")
					if err := os.WriteFile(r.SourcePath, []byte(strings.Repeat("x", test.source)), 0600); err != nil {
						t.Fatal(err)
					}
				}
				refs = append(refs, r)
			}
			added, err := s.collect(ctx, "conv", "run", "call", refs)
			if len(added) != test.want {
				t.Fatalf("count=%d want=%d", len(added), test.want)
			}
			if test.want == 0 && err == nil {
				t.Fatal("expected size error")
			}
		})
	}
}

func TestArtifactCollectionRejectsInvalidFiles(t *testing.T) {
	for _, name := range []string{"relative", "traversal", "symlink", "directory", "format", "source", "utf8"} {
		t.Run(name, func(t *testing.T) {
			r := artifactFixture(t, "diagram")
			switch name {
			case "relative":
				r.Path = "diagram.svg"
			case "traversal":
				r.Path = filepath.Dir(r.Path) + "/../" + filepath.Base(filepath.Dir(r.Path)) + "/diagram.svg"
			case "symlink":
				link := filepath.Join(filepath.Dir(r.Path), "link.svg")
				if err := os.Symlink(r.Path, link); err != nil {
					t.Fatal(err)
				}
				r.Path = link
			case "directory":
				r.Path = filepath.Dir(r.Path)
			case "format":
				r.Format = "html"
			case "source":
				r.SourcePath = "/missing/source"
			case "utf8":
				if err := os.WriteFile(r.Path, []byte{255}, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s := &artifactStore{journal: journal.NewMemStore()}
			added, err := s.collect(context.Background(), "conv", "run", "call", []tools.ArtifactRef{r})
			if err == nil || len(added) != 0 {
				t.Fatalf("accepted invalid reference: %+v %v", added, err)
			}
		})
	}
}
