package tasks

import (
	"strings"
	"testing"
)

func TestInstalledDeliverableProvenance(t *testing.T) {
	t.Setenv("EXCALIBUR_INSTALLED_DELIVERABLES", "on")
	t.Setenv("EXCALIBUR_DELIVERABLE_CHECKS", "on")
	for _, tc := range []struct {
		request, path string
		kind          ArtifactKind
	}{
		{"Build the tool into /usr/local/bin/mytool.", "/usr/local/bin/mytool", ArtifactExecutable},
		{"Place the plugin at /usr/local/lib/myplugin.so.", "/usr/local/lib/myplugin.so", ArtifactPath},
		{"Install the data into /usr/local/share/myapp/data.", "/usr/local/share/myapp/data", ArtifactPath},
		{"Create /opt/myapp/bin/runner.", "/opt/myapp/bin/runner", ArtifactExecutable},
		{"Build /opt/myapp/package.", "/opt/myapp/package", ArtifactPath},
		{"The tool must be installed at /usr/local/bin/mytool.", "/usr/local/bin/mytool", ArtifactExecutable},
		{"The installed tool is /usr/local/bin/mytool.", "/usr/local/bin/mytool", ""},
		{"The tool lives at `/usr/local/bin/mytool`.", "/usr/local/bin/mytool", ""},
		{"Do not install /usr/local/bin/mytool.", "/usr/local/bin/mytool", ""},
		{"Install a tool, for example /usr/local/bin/mytool.", "/usr/local/bin/mytool", ""},
		{"Create /usr/local/binary/mytool.", "/usr/local/binary/mytool", ""},
	} {
		t.Run(tc.request, func(t *testing.T) {
			reqs := ExtractArtifactRequirementsIn(tc.request, 8, "/work")
			if got := artifactKeys(reqs)[tc.path]; got != tc.kind {
				t.Fatalf("kind %q, want %q: %+v", got, tc.kind, reqs)
			}
			if tc.kind == "" && len(reqs) != 0 {
				t.Fatalf("mere mention or excluded path became requirements: %+v", reqs)
			}
		})
	}
	t.Setenv("EXCALIBUR_INSTALLED_DELIVERABLES", "off")
	if got := ExtractArtifactRequirements("Install /usr/local/bin/mytool.", 8); len(got) != 0 {
		t.Fatalf("switch off broadened roots: %+v", got)
	}
}

func TestInstalledDeliverableHardChecks(t *testing.T) {
	t.Setenv("EXCALIBUR_INSTALLED_DELIVERABLES", "on")
	for _, tc := range []struct {
		request string
		fs      ArtifactFS
		want    string
	}{
		{"Install /usr/local/bin/mytool.", ArtifactFS{}, "missing"},
		{"Install /usr/local/bin/mytool.", ArtifactFS{Exists: true}, "not executable"},
		{"Install /usr/local/bin/mytool.", ArtifactFS{Exists: true, Executable: true}, ""},
		{"Install /usr/local/bin/mytool.", ArtifactFS{Exists: true, IsDir: true}, ""},
		{"Install /usr/local/lib/myplugin.so.", ArtifactFS{}, "missing"},
		{"Install /usr/local/lib/myplugin.so.", ArtifactFS{Exists: true}, ""},
		{"Place /usr/local/share/myapp/data.", ArtifactFS{Exists: true}, ""},
		{"Build /opt/myapp/bin/runner.", ArtifactFS{Exists: true}, "not executable"},
		{"The tool lives at /usr/local/bin/mytool.", ArtifactFS{}, ""},
	} {
		t.Run(tc.request+tc.want, func(t *testing.T) {
			reqs := nativeFixtureRequirements(ExtractArtifactRequirements(tc.request, 8))
			checks := 0
			pending := CheckArtifactFiles(reqs, nil, "/work", func(path string) (ArtifactFS, error) {
				checks++
				return tc.fs, nil
			}, nil)
			if tc.want == "" && len(pending) != 0 || tc.want != "" && (len(pending) != 1 || !strings.Contains(pending[0], tc.want)) {
				t.Fatalf("pending: %v, want %q (reqs %+v)", pending, tc.want, reqs)
			}
			if strings.HasPrefix(tc.request, "The tool") && checks != 0 {
				t.Fatal("mere mention caused a filesystem check")
			}
		})
	}
}
