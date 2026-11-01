package scopeguard

import (
	"regexp"
	"strings"
)

var bareLocation = regexp.MustCompile("(?i)(?:\\b(?:modify|delete|change|edit|touch|write|create)(?:\\s+only)?|\\bleave)\\s+(?:the\\s+(?:file|directory)\\s+)?[`\"']?([A-Za-z0-9_./*-]+)")
var deleteOnly = regexp.MustCompile(`(?i)(?:do not|don't|must not|never)\s+delete\b`)

func constraintPaths(sentence string) []string {
	ps := pathNames(sentence)
	if prohibition.MatchString(sentence) || onlyEdits.MatchString(sentence) {
		for _, m := range bareLocation.FindAllStringSubmatch(sentence, -1) {
			p := strings.TrimRight(m[1], ".,")
			switch strings.ToLower(p) {
			case "all", "any", "anything", "existing", "files", "the", "or", "only":
				continue
			}
			found := false
			for _, old := range ps {
				if old == p {
					found = true
				}
			}
			if !found {
				ps = append(ps, p)
			}
		}
	}
	return ps
}
