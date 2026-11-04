package codeintel

import (
	"net/url"
	"path"
	"strings"
)

// dirToURIFlavor accepts an absolute path, independent of the running OS.
func dirToURIFlavor(dir string, windows bool) string {
	u := url.URL{Scheme: "file", Path: dir}
	if windows {
		dir = strings.ReplaceAll(dir, `\`, "/")
		if strings.HasPrefix(dir, "//") {
			host, rest, _ := strings.Cut(strings.TrimPrefix(dir, "//"), "/")
			u.Host, u.Path = host, "/"+rest
		} else {
			u.Path = "/" + strings.TrimPrefix(dir, "/")
		}
	}
	if windows && len(u.Path) == 4 && u.Path[2] == ':' && u.Path[3] == '/' {
		return u.String()
	}
	return strings.TrimSuffix(u.String(), "/")
}

func uriToRelFlavor(rootDir, uri string, windows bool) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	if !windows {
		// Preserve the established Unix mapping behavior.
		p, err := url.PathUnescape(u.Path)
		if err != nil {
			return uri
		}
		rel, ok := unixRelativePath(rootDir, p)
		if !ok || strings.HasPrefix(rel, "..") {
			return p
		}
		return rel
	}
	// url.Parse already decodes escapes in Path. Decoding twice would corrupt
	// names containing literal percent sequences, such as report%20.txt.
	p := strings.ReplaceAll(u.Path, `\`, "/")
	if u.Opaque != "" {
		return uri
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		p = "//" + u.Host + "/" + strings.TrimPrefix(p, "/")
	} else if len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	p = cleanWindowsURIPath(p)
	if rootDir == "" {
		return p
	}
	root := cleanWindowsURIPath(rootDir)
	if strings.EqualFold(root, p) {
		return "."
	}
	prefix := strings.TrimRight(root, "/") + "/"
	if len(p) >= len(prefix) && strings.EqualFold(prefix, p[:len(prefix)]) {
		return p[len(prefix):]
	}
	return p
}

func cleanWindowsURIPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		// Clean only the rooted portion, so .. cannot consume a drive letter.
		return p[:2] + path.Clean(p[2:])
	}
	if strings.HasPrefix(p, "//") {
		// A UNC share is the filesystem root; .. cannot consume its share name.
		host, rest, _ := strings.Cut(p[2:], "/")
		share, rest, _ := strings.Cut(rest, "/")
		return "//" + host + "/" + share + strings.TrimSuffix(path.Clean("/"+rest), "/")
	}
	return path.Clean(p)
}

func cleanToolPath(name string) string { return cleanToolPathFlavor(name, windowsPaths()) }

func cleanToolPathFlavor(name string, windows bool) string {
	if windows {
		name = strings.ReplaceAll(name, `\`, "/")
	}
	return path.Clean(name)
}

func joinDocumentURI(rootURI, escapedRel string, windows bool) string {
	if windows {
		rootURI = strings.TrimSuffix(rootURI, "/")
	}
	return rootURI + "/" + escapedRel
}

// unixRelativePath mirrors filepath.Rel for slash-separated Unix paths, so the
// Unix flavor remains independent of the OS executing these helpers.
func unixRelativePath(base, target string) (string, bool) {
	base, target = path.Clean(base), path.Clean(target)
	if base == target {
		return ".", true
	}
	if strings.HasPrefix(base, "/") != strings.HasPrefix(target, "/") {
		return "", false
	}
	parts := func(p string) []string {
		if p == "." || p == "/" {
			return nil
		}
		return strings.Split(strings.TrimPrefix(p, "/"), "/")
	}
	b, t := parts(base), parts(target)
	i := 0
	for i < len(b) && i < len(t) && b[i] == t[i] {
		i++
	}
	if i < len(b) && b[i] == ".." {
		return "", false
	}
	rel := make([]string, 0, len(b)-i+len(t)-i)
	for range b[i:] {
		rel = append(rel, "..")
	}
	rel = append(rel, t[i:]...)
	if len(rel) == 0 {
		return ".", true
	}
	return strings.Join(rel, "/"), true
}
