package tasks

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStrictAbsolutePathsSymlinkRoot(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	real, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	roots := DeliverableRoots
	t.Cleanup(func() { DeliverableRoots = roots })
	for _, workdir := range []string{"", alias} {
		DeliverableRoots = nil
		if workdir == "" {
			DeliverableRoots = []string{alias}
		}
		output := filepath.Join(real, "future", "out.txt")
		if got := StrictAbsolutePaths("Write "+output, workdir); !reflect.DeepEqual(got, []string{output}) {
			t.Fatalf("aliased root with workdir %q: %v", workdir, got)
		}
		if got := StrictAbsolutePaths("Write "+real+"-sibling/out.txt", workdir); len(got) != 0 {
			t.Fatalf("sibling prefix admitted: %v", got)
		}
	}
}

func TestLongArtifactSourceKeepsTrailingIntent(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, strings.Repeat("nested", 30), "result.csv")
	for _, suffix := range []string{" in place", " in place but never write it"} {
		request := "Edit the required file `" + output + "`" + suffix
		found := false
		for _, req := range ExtractArtifactRequirementsIn(request, 8, dir) {
			if req.Path != output {
				continue
			}
			found = true
			if req.IntentSource() != request {
				t.Fatalf("source lost request intent: %q", req.IntentSource())
			}
			if want := request[:119] + "…"; req.Source != want {
				t.Fatalf("display source = %q, want %q", req.Source, want)
			}
			if got, want := HardArtifactRequirement(req), suffix == " in place"; got != want {
				t.Fatalf("hard requirement for %q = %v, want %v", suffix, got, want)
			}
		}
		if !found {
			t.Fatalf("long absolute output not extracted: %q", output)
		}
	}
}

// Table tests for the strict deliverable path rule (D3a, items 77 and 100):
// absolute paths count only under a known root or the workdir, never when
// date-shaped, never as a fragment of a longer token such as a URL.
func TestStrictAbsolutePaths(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		workdir string
		want    []string
	}{
		{"root path", "Write the report to /app/out.txt.", "", []string{"/app/out.txt"}},
		{"home path", "Save it as /home/user/work/result.json", "", []string{"/home/user/work/result.json"}},
		{"date shaped under root", "Log to /tmp/2025/02/14/run.log", "", nil},
		{"date value after a name", "the row for data/2025/02/14 is kept", "", nil},
		{"url is not a path", "Fetch https://example.com/app/data and save /app/x.bin", "", []string{"/app/x.bin"}},
		{"installed root", "Install the tool at /usr/local/bin/tool", "", []string{"/usr/local/bin/tool"}},
		{"workdir outside roots", "Create /work/project/out.txt", "/work/project", []string{"/work/project/out.txt"}},
		{"workdir prefix is not a directory match", "Create /work/projectx/out.txt", "/work/project", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StrictAbsolutePaths(tc.text, tc.workdir)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("StrictAbsolutePaths(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// The requirement extractor uses the strict rule while the switch is on,
// and the legacy unrestricted extraction when it is off.
func TestExtractRequirementsStrictAndSwitchOff(t *testing.T) {
	text := "Save the table as /app/out.csv. The row date was data/2025/02/14 and the tool is /usr/local/bin/tool."
	t.Setenv("EXCALIBUR_DELIVERABLE_CHECKS", "on")
	for _, a := range ExtractArtifactRequirements(text, 8) {
		if a.Path == "/2025/02/14" || a.Path == "/usr/local/bin/tool" {
			t.Fatalf("strict extraction must not return %q: %+v", a.Path, a)
		}
	}
	t.Setenv("EXCALIBUR_DELIVERABLE_CHECKS", "off")
	found := false
	for _, a := range ExtractArtifactRequirements(text, 8) {
		if a.Path == "/usr/local/bin/tool" {
			found = true
		}
	}
	if !found {
		t.Fatal("switch off must restore the legacy unrestricted extraction")
	}
}
