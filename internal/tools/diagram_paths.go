package tools

import (
	"net/url"
	"strings"
)

// fileURL encodes an absolute path using the selected filesystem flavor.
func fileURL(p string, windows bool) string {
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
