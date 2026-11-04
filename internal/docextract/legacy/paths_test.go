package legacy

import "testing"

func TestOfficeFileURLFlavors(t *testing.T) {
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
		if got := officeFileURL(tc.path, tc.windows); got != tc.want {
			t.Errorf("officeFileURL(%q, %v) = %q; want %q", tc.path, tc.windows, got, tc.want)
		}
	}
}
