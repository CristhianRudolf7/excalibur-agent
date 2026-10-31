package master

import (
	"sort"
	"strings"
	"sync"
)

// Lexicon is the in-RAM alias trie of Section 6.1: project names,
// active agent names (visible name and alias) and open task codes and
// titles. Matching is case-insensitive substring search over a small
// set (dozens of entries); rebuilt when projects, agents or tasks
// change. An ambiguous surface form returns every candidate.
type Lexicon struct {
	mu       sync.RWMutex
	projects map[string]string   // lower alias -> project id
	agents   map[string][]string // lower name -> agent ids
	tasks    map[string]string   // lower code/title -> task id
}

// NewLexicon returns an empty lexicon.
func NewLexicon() *Lexicon {
	return &Lexicon{
		projects: map[string]string{},
		agents:   map[string][]string{},
		tasks:    map[string]string{},
	}
}

// AddProject registers a project name and alias.
func (l *Lexicon) AddProject(name, alias, id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range []string{name, alias} {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			l.projects[k] = id
		}
	}
}

// AddAgent registers an agent visible name.
func (l *Lexicon) AddAgent(name, id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	k := strings.ToLower(strings.TrimSpace(name))
	if k == "" {
		return
	}
	for _, have := range l.agents[k] {
		if have == id {
			return
		}
	}
	l.agents[k] = append(l.agents[k], id)
}

// AddTask registers an open task code and title.
func (l *Lexicon) AddTask(code, title, id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range []string{code, title} {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			l.tasks[k] = id
		}
	}
}

// MentionKind names what a mention resolved to.
type MentionKind string

// Mention kinds.
const (
	MentionProject MentionKind = "project"
	MentionAgent   MentionKind = "agent"
	MentionTask    MentionKind = "task"
)

// Mention is one resolved alias: every candidate id, so ambiguous
// forms ("two agents called Lena") surface all of them (Section 6.1).
type Mention struct {
	Text       string
	Kind       MentionKind
	Candidates []string
}

// Ambiguous reports whether the mention needs a clarifying question.
func (m Mention) Ambiguous() bool { return len(m.Candidates) > 1 }

// Mentions scans text for every registered alias. Matches are
// longest-first so "gastos app" wins over "gastos" at the same spot.
func (l *Lexicon) Mentions(text string) []Mention {
	l.mu.RLock()
	defer l.mu.RUnlock()
	lower := strings.ToLower(text)
	type hit struct {
		at   int
		men  Mention
		long int
	}
	var hits []hit
	add := func(kind MentionKind, table map[string][]string, single map[string]string) {
		keys := make([]string, 0, len(table)+len(single))
		for k := range table {
			keys = append(keys, k)
		}
		for k := range single {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		for _, k := range keys {
			idx := strings.Index(lower, k)
			if idx < 0 {
				continue
			}
			var cands []string
			if table != nil {
				if c, ok := table[k]; ok {
					cands = append([]string(nil), c...)
				}
			}
			if id, ok := single[k]; ok {
				cands = append(cands, id)
			}
			if len(cands) == 0 {
				continue
			}
			hits = append(hits, hit{at: idx, men: Mention{Text: k, Kind: kind, Candidates: cands}, long: len(k)})
		}
	}
	agentTable := l.agents
	projSingle := map[string]string{}
	for k, v := range l.projects {
		projSingle[k] = v
	}
	taskSingle := map[string]string{}
	for k, v := range l.tasks {
		taskSingle[k] = v
	}
	add(MentionAgent, agentTable, nil)
	add(MentionProject, nil, projSingle)
	add(MentionTask, nil, taskSingle)
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].at != hits[j].at {
			return hits[i].at < hits[j].at
		}
		return hits[i].long > hits[j].long
	})
	out := make([]Mention, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.men)
	}
	return out
}
