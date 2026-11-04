package codeintel

import (
	"path/filepath"
	"testing"
)

func TestURIPathFlavors(t *testing.T) {
	for _, tc := range []struct {
		path, uri string
		windows   bool
	}{
		{"/tmp/a b#c", "file:///tmp/a%20b%23c", false},
		{`C:\Users\A B\é#%20`, "file:///C:/Users/A%20B/%C3%A9%23%2520", true},
		{`\\server\share\A B`, "file://server/share/A%20B", true},
		{`c:/work`, "file:///c:/work", true},
		{`C:\`, "file:///C:/", true},
	} {
		if got := dirToURIFlavor(tc.path, tc.windows); got != tc.uri {
			t.Errorf("dirToURIFlavor(%q): %q; want %q", tc.path, got, tc.uri)
		}
	}
	for _, tc := range []struct {
		root, uri, want string
		windows         bool
	}{
		{"/tmp/project", "file:///tmp/project/a%20b.go", "a b.go", false},
		{`/tmp/a\b`, "file:///tmp/a%5Cb/x.go", "x.go", false},
		{"/tmp/project", "file:///tmp/project/a%2520b.go", "a b.go", false},
		{"/tmp/project", "file:///tmp/project2/a.go", "/tmp/project2/a.go", false},
		{`C:\Work\Project`, "file:///c:/work/project/a%20b.go", "a b.go", true},
		{`C:\Work\Project`, "file:///c:/Work/Project/a%2520b%23%C3%A9.go", "a%20b#é.go", true},
		{`C:\Work\Project`, "file:///C:/Work/Project2/a.go", "C:/Work/Project2/a.go", true},
		{`C:\Work\Project`, "file:///D:/Work/Project/a.go", "D:/Work/Project/a.go", true},
		{`C:\`, "file:///c:/a.go", "a.go", true},
		{"", "file:///C:/", "C:/", true},
		{`C:\`, "file:///C:/../a.go", "a.go", true},
		{`\\server\share`, "file://server/share/../a.go", "a.go", true},
		{`C:\Work`, "file:///C:/Work/../other/a.go", "C:/other/a.go", true},
		{`C:\Work`, "file:///C:/Work/..hidden/a.go", "..hidden/a.go", true},
		{`\\SERVER\Share\Project`, "file://server/share/project/a.go", "a.go", true},
		{`C:\Work`, "file://localhost/C:/Work/a.go", "a.go", true},
		{`C:\Work`, "untitled:x", "untitled:x", true},
		{`C:\Work`, "file:///C:/Work/%zz", "file:///C:/Work/%zz", true},
		{"", "file:///C:/Work/a.go", "C:/Work/a.go", true},
	} {
		if got := uriToRelFlavor(tc.root, tc.uri, tc.windows); got != tc.want {
			t.Errorf("uriToRelFlavor(%q,%q,%v): %q; want %q", tc.root, tc.uri, tc.windows, got, tc.want)
		}
	}
}

func TestToolPathFlavors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		windows bool
		want    string
	}{
		{`src\x\..\a.go`, true, "src/a.go"},
		{`src\x\..\a.go`, false, `src\x\..\a.go`},
		{`src/./a.go`, false, "src/a.go"},
	} {
		if got := cleanToolPathFlavor(tc.name, tc.windows); got != tc.want {
			t.Errorf("cleanToolPathFlavor(%q,%v): %q; want %q", tc.name, tc.windows, got, tc.want)
		}
	}
}

func TestDocumentURIRoots(t *testing.T) {
	for _, tc := range []struct {
		root, want string
		windows    bool
	}{
		{"/", "file:///a.go", false},
		{"/tmp", "file:///tmp/a.go", false},
		{`C:\`, "file:///C:/a.go", true},
		{`C:\work`, "file:///C:/work/a.go", true},
	} {
		if got := joinDocumentURI(dirToURIFlavor(tc.root, tc.windows), "a.go", tc.windows); got != tc.want {
			t.Errorf("root %q: %q, want %q", tc.root, got, tc.want)
		}
	}
}

func TestUnixRelativePathPreservesNativeBehavior(t *testing.T) {
	if windowsPaths() {
		t.Skip("compare against Unix filepath.Rel")
	}
	paths := []string{"", ".", "..", "../..", "../a", "../a/b", "../../a", "a", "a/b", "a/../b", "/", "/a", "/a/b", "/ab", `/a\b`}
	for _, base := range paths {
		for _, target := range paths {
			want, err := filepath.Rel(base, target)
			got, ok := unixRelativePath(base, target)
			if ok != (err == nil) || (ok && got != want) {
				t.Errorf("%q -> %q: %q/%v; want %q/%v", base, target, got, ok, want, err)
			}
		}
	}
}
