package tools

import (
	"path"
	"strings"
)

// commandBaseForOS is lexical so Windows command paths can be tested on Unix.
func commandBaseForOS(word string, windows bool) string {
	if !windows {
		return path.Base(word)
	}
	name := strings.ToLower(path.Base(strings.ReplaceAll(strings.Trim(word, `"'`), `\`, "/")))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return name
}

// canonicalWindowsBoundaryPath accepts drive, UNC and extended-length paths.
// Device namespaces other than their drive/UNC aliases are rejected outright.
func canonicalWindowsBoundaryPath(p string) (clean string, absolute, device bool) {
	p = strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
	switch {
	case strings.HasPrefix(p, "//?/unc/"):
		p = "//" + p[len("//?/unc/"):]
	case strings.HasPrefix(p, "//?/"):
		p = p[4:]
		if len(p) < 3 || p[1] != ':' || p[2] != '/' {
			return "", true, true
		}
	case strings.HasPrefix(p, "//./"), strings.HasPrefix(p, "/??/"):
		return "", true, true
	}
	// Git for Windows bash exposes drives as /c/... (and Cygwin uses
	// /cygdrive/c/...). These name the same protected native locations.
	if strings.HasPrefix(p, "/cygdrive/") {
		p = p[len("/cygdrive"):]
	}
	if len(p) >= 3 && p[0] == '/' && p[1] >= 'a' && p[1] <= 'z' && p[2] == '/' {
		p = p[1:2] + ":" + p[2:]
	}
	unc := strings.HasPrefix(p, "//")
	drive := len(p) >= 3 && p[0] >= 'a' && p[0] <= 'z' && p[1] == ':' && p[2] == '/'
	if !unc && !drive && !strings.HasPrefix(p, "/") {
		return "", false, false
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		trimmed := strings.TrimRight(part, " ")
		if trimmed == "." || trimmed == ".." {
			parts[i] = trimmed
			continue
		}
		// Win32 ignores trailing spaces/dots and supports alternate data streams.
		if !(i == 0 && drive) {
			if at := strings.IndexByte(part, ':'); at >= 0 {
				part = part[:at]
			}
		}
		parts[i] = strings.TrimRight(part, " .")
	}
	p = strings.Join(parts, "/")
	if drive {
		p = p[:2] + path.Clean(p[2:])
	} else if unc {
		// A UNC share is a root; .. cannot escape its server/share prefix.
		pieces := strings.Split(strings.TrimLeft(p, "/"), "/")
		if len(pieces) < 2 {
			return "", true, true
		}
		p = "//" + pieces[0] + "/" + pieces[1] + strings.TrimSuffix(path.Clean("/"+strings.Join(pieces[2:], "/")), "/")
	} else {
		p = path.Clean(p)
	}
	return p, true, false
}

func windowsPathOutsideBoundary(p string, harness, systemRoots []string) bool {
	clean, absolute, device := canonicalWindowsBoundaryPath(p)
	if device {
		return true
	}
	if !absolute {
		return false
	}
	for _, roots := range [][]string{systemRoots, harness} {
		for _, root := range roots {
			base, abs, dev := canonicalWindowsBoundaryPath(root)
			if abs && !dev && base != "/" && (within(clean, base) ||
				(strings.HasPrefix(clean, "/") && !strings.HasPrefix(clean, "//") && len(base) > 2 && base[1] == ':' && within(clean, base[2:]))) {
				return true
			}
		}
	}
	return false
}

// Quotes preserve spaces and shell metacharacters within path operands.
func windowsBoundaryWords(command string) []string {
	return windowsLexicalWords(command, " \t\r\n;&|()<>`=${}")
}

func windowsLexicalWords(command, separators string) []string {
	var words []string
	var word strings.Builder
	var quote rune
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if strings.ContainsRune(separators, r) {
			flush()
		} else {
			word.WriteRune(r)
		}
	}
	flush()
	return words
}
