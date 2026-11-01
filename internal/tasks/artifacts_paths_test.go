package tasks

import (
	"reflect"
	"testing"
)

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
		{"unknown root", "Install the tool at /usr/local/bin/tool", "", nil},
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
