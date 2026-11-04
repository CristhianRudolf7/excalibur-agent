package legacy

import (
	"net/url"
	"strings"
)

// officeFileURL encodes an absolute path using the selected filesystem flavor.
func officeFileURL(p string, windows bool) string {
	u := url.URL{Scheme: "file", Path: p}
	if windows {
		p = strings.ReplaceAll(p, `\`, "/")
		if strings.HasPrefix(p, "//") {
			host, rest, _ := strings.Cut(strings.TrimPrefix(p, "//"), "/")
			u.Host, u.Path = host, "/"+rest
		} else {
			u.Path = "/" + strings.TrimPrefix(p, "/")
		}
	}
	return u.String()
}
