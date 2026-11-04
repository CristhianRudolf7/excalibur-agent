package tools

import "testing"

func TestDiagramFileURLFlavors(t *testing.T) {
	for _, tc := range []struct {
		path    string
		windows bool
		want    string
	}{
		{"/tmp/a b#%.svg", false, "file:///tmp/a%20b%23%25.svg"},
		{`/tmp/a\b.svg`, false, "file:///tmp/a%5Cb.svg"},
		{`C:\x\a b#%.svg`, true, "file:///C:/x/a%20b%23%25.svg"},
		{`c:/x/é.svg`, true, "file:///c:/x/%C3%A9.svg"},
		{`\\server\share\a b.svg`, true, "file://server/share/a%20b.svg"},
	} {
		if got := fileURL(tc.path, tc.windows); got != tc.want {
			t.Errorf("fileURL(%q, %v) = %q; want %q", tc.path, tc.windows, got, tc.want)
		}
	}
}
