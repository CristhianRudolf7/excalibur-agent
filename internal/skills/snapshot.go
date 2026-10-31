package skills

import "sort"

// ChangeKind classifies one drift between a frozen session catalog
// and the live index.
type ChangeKind string

// Change kinds.
const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
	ChangeUpdated ChangeKind = "updated"
)

// Change is one skill that appeared, disappeared or changed hash
// since a session froze its catalog.
type Change struct {
	Kind ChangeKind
	Name string
}

// Snapshot is a session's frozen catalog (Section 12.2): the
// content hashes of the skills the session may use. The session
// keeps rendering from the snapshot so a reindex never breaks its
// input-cache prefix; Delta reports what changed since.
type Snapshot struct {
	Hashes map[string]string
}

// Freeze captures the current hashes of names. An empty names
// slice freezes the whole catalog.
func (ix *Index) Freeze(names []string) *Snapshot {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	snap := &Snapshot{Hashes: map[string]string{}}
	if len(names) == 0 {
		for n, e := range ix.entries {
			snap.Hashes[n] = e.Hash
		}
		return snap
	}
	for _, n := range names {
		if e, ok := ix.entries[n]; ok {
			snap.Hashes[n] = e.Hash
		}
	}
	return snap
}

// Delta compares the snapshot with the live index: skills the
// session uses that changed hash (updated), vanished (removed) or
// newly appeared under a frozen name set (added). A whole-catalog
// snapshot never reports added entries, only updates and removals,
// so sessions are not surprised by new skills.
func (s *Snapshot) Delta(ix *Index) []Change {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	var out []Change
	for n, h := range s.Hashes {
		e, ok := ix.entries[n]
		if !ok {
			out = append(out, Change{Kind: ChangeRemoved, Name: n})
			continue
		}
		if e.Hash != h {
			out = append(out, Change{Kind: ChangeUpdated, Name: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}
