package scopeguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var clauses = regexp.MustCompile(`[\n;]|\.\s+`)
var onlyEdits = regexp.MustCompile(`(?i)\bonly\s+(?:modify|edit|change|write|create)\b|\b(?:modify|edit|change|write|create)\s+only\b`)
var requiredLocation = regexp.MustCompile(`(?i)\b(?:write|create|save|output|produce|store|place)\b`)
var cardinality = regexp.MustCompile(`(?i)\b(exactly|at least|at most|no more than|more than|less than)\s+(\d+)\s+(lines|rows|records|entries|bytes)\b`)

type countRule struct {
	Path, Relation, Unit string
	Count                int
}

func (g *Guard) extract(instruction string) {
	for _, sentence := range clauses.Split(instruction, -1) {
		ps := constraintPaths(sentence)
		if onlyEdits.MatchString(sentence) {
			for _, p := range ps {
				g.only = append(g.only, g.resolve(p))
			}
		}
		if requiredLocation.MatchString(sentence) && !prohibition.MatchString(sentence) {
			for _, p := range ps {
				g.required = append(g.required, g.resolve(p))
			}
		}
		if m := cardinality.FindStringSubmatch(sentence); len(m) > 0 && len(ps) == 1 {
			n, _ := strconv.Atoi(m[2])
			g.counts = append(g.counts, countRule{g.resolve(ps[0]), strings.ToLower(m[1]), strings.ToLower(m[3]), n})
		}
	}
}

func (g *Guard) checkConstraints() []string {
	var bad []string
	for _, p := range g.preserveExistence {
		if _, e := os.Lstat(p); e != nil {
			bad = append(bad, "protected path deleted: "+p)
		}
	}
	for _, dir := range g.protectedDirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			bad = append(bad, "protected directory removed or replaced: "+dir)
			continue
		}
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				if _, ok := g.Initial[p]; !ok {
					bad = append(bad, "protected directory gained a path: "+p)
				}
			}
			return nil
		})
	}
	for _, p := range g.required {
		if _, err := os.Stat(p); err != nil {
			bad = append(bad, "required file location unavailable: "+p+": "+err.Error())
		}
	}
	if len(g.only) > 0 {
		current := map[string]string{}
		_ = filepath.WalkDir(g.Root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() || d.Type()&os.ModeSymlink != 0 {
				if h, e := digest(p); e == nil {
					current[p] = h
				}
			}
			return nil
		})
		union := map[string]bool{}
		for p := range g.Initial {
			if p != g.Root && !strings.HasPrefix(p, g.Root+string(filepath.Separator)) {
				if h, err := digest(p); err == nil {
					current[p] = h
				}
			}
		}
		for p := range current {
			union[p] = true
		}
		for p := range g.Initial {
			union[p] = true
		}
		for p := range union {
			if current[p] == g.Initial[p] {
				continue
			}
			allowed := false
			for _, a := range g.only {
				if p == a || strings.HasPrefix(p, a+string(filepath.Separator)) {
					allowed = true
				}
			}
			if !allowed {
				bad = append(bad, "only constraint: changed path outside allowed locations: "+p)
			}
		}
	}
	for _, r := range g.counts {
		b, e := os.ReadFile(r.Path)
		if e != nil {
			bad = append(bad, "count constraint unreadable: "+r.Path)
			continue
		}
		n := -1
		switch r.Unit {
		case "bytes":
			n = len(b)
		case "lines":
			n = strings.Count(string(b), "\n")
			if len(b) > 0 && b[len(b)-1] != '\n' {
				n++
			}
		case "rows", "records", "entries":
			var a []json.RawMessage
			if json.Unmarshal(b, &a) == nil {
				n = len(a)
			}
		}
		if n < 0 {
			bad = append(bad, "count constraint requires a JSON array: "+r.Path)
			continue
		}
		ok := false
		switch r.Relation {
		case "exactly":
			ok = n == r.Count
		case "at least":
			ok = n >= r.Count
		case "at most", "no more than":
			ok = n <= r.Count
		case "more than":
			ok = n > r.Count
		case "less than":
			ok = n < r.Count
		}
		if !ok {
			bad = append(bad, fmt.Sprintf("count constraint: %s requires %s %d %s; got %d", r.Path, r.Relation, r.Count, r.Unit, n))
		}
	}
	return bad
}

func pathNames(text string) []string {
	ps := paths.FindAllString(text, -1)
	for i, p := range ps {
		ps[i] = strings.TrimRight(p, ".,")
	}
	return ps
}
