package tools

import (
	"regexp"
	"strings"
)

var (
	diagramCSSImport = regexp.MustCompile(`(?is)@import\b[^;]*(?:;|$)`)
	diagramCSSURL    = regexp.MustCompile(`(?is)url\(\s*(?:"[^"]*"|'[^']*'|[^)]*)\s*\)`)
)

// Preserve renderer styles and embedded fonts while removing network/file
// lookups. Escaped identifiers and alternative image functions are rejected
// rather than interpreted differently by a browser and the sanitizer.
func sanitizeDiagramCSS(css string) string {
	lower := strings.ToLower(css)
	for _, unsafe := range []string{"\\", "javascript:", "expression(", "image-set(", "image("} {
		if strings.Contains(lower, unsafe) {
			return ""
		}
	}
	css = diagramCSSImport.ReplaceAllString(css, "")
	css = diagramCSSURL.ReplaceAllStringFunc(css, func(token string) string {
		value := strings.Trim(strings.TrimSpace(token[4:len(token)-1]), "\"'")
		if safeDiagramReference(value) {
			return token
		}
		return "none"
	})
	// A malformed url() must not bypass complete-token processing.
	if strings.Contains(strings.ToLower(diagramCSSURL.ReplaceAllString(css, "")), "url(") {
		return ""
	}
	return css
}

// Embedded raster images and fonts are self-contained. SVG and HTML data URLs
// remain forbidden because they can carry active content or further references.
func safeDiagramReference(value string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "#") {
		return true
	}
	lower := strings.ToLower(value)
	if (strings.HasPrefix(lower, "data:font/") || strings.HasPrefix(lower, "data:application/font-") || strings.HasPrefix(lower, "data:application/x-font-")) && strings.Contains(lower, ";base64,") {
		return true
	}
	for _, mime := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		if strings.HasPrefix(lower, "data:"+mime+";base64,") {
			return true
		}
	}
	return false
}
