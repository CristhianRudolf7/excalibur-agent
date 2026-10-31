// General conversations and the general notepad (Section 8.9).
//
// General conversations stay in SQLite like everything else: the
// tasks of the general scope (titles, states, details) and the
// general notepad. Short tasks may use a temporary micro plan
// without creating a project or memory. The notepad itself (identity
// documents and notes) belongs to the memory milestone (H11,
// Section 10.2): this file holds the general-scope board the master
// reads and the note rows with their version history.
package tasks

import (
	"fmt"
	"sort"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// GeneralTask is the board row for one general-scope task: title,
// state and details.
type GeneralTask struct {
	ID      types.TaskID
	Code    types.TaskRef
	Title   string
	State   types.TaskState
	Details string
}

// GeneralBoard lists the general-scope tasks in code order.
func GeneralBoard(store PlanningStore) []GeneralTask {
	var out []GeneralTask
	for _, t := range store.ListTasks(types.GeneralScope()) {
		out = append(out, GeneralTask{
			ID:      t.ID,
			Code:    t.Code,
			Title:   t.Request.Interpretation,
			State:   t.State,
			Details: t.Request.Original,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Note is one general-notepad row with a version history.
type Note struct {
	ID        string
	Text      string
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Notebook keeps the general notepad in memory for tests and the
// embedded profile; the durable binding lands with the memory
// milestone (H11).
type Notebook struct {
	notes   map[string][]Note
	counter int
}

// NewNotebook returns an empty notebook.
func NewNotebook() *Notebook {
	return &Notebook{notes: map[string][]Note{}}
}

// Add appends a note and returns it at version 1.
func (n *Notebook) Add(text string) Note {
	n.counter++
	id := fmt.Sprintf("note_%d", n.counter)
	note := Note{ID: id, Text: text, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	n.notes[id] = []Note{note}
	return note
}

// Update appends a new version, keeping the history.
func (n *Notebook) Update(id, text string, wantVersion int) (Note, error) {
	vers, ok := n.notes[id]
	if !ok || len(vers) == 0 {
		return Note{}, fmt.Errorf("tasks: unknown note %q", id)
	}
	cur := vers[len(vers)-1]
	if cur.Version != wantVersion {
		return Note{}, &VersionConflict{Entity: "note", ID: id, Have: cur.Version, Want: wantVersion, Current: cur}
	}
	next := Note{ID: id, Text: text, Version: cur.Version + 1, CreatedAt: cur.CreatedAt, UpdatedAt: time.Now().UTC()}
	n.notes[id] = append(vers, next)
	return next, nil
}

// Get returns the latest version of a note.
func (n *Notebook) Get(id string) (Note, error) {
	vers, ok := n.notes[id]
	if !ok || len(vers) == 0 {
		return Note{}, fmt.Errorf("tasks: unknown note %q", id)
	}
	return vers[len(vers)-1], nil
}

// History returns every version of a note.
func (n *Notebook) History(id string) []Note {
	return append([]Note(nil), n.notes[id]...)
}

// List returns the latest version of every note in id order.
func (n *Notebook) List() []Note {
	var out []Note
	for _, vers := range n.notes {
		if len(vers) > 0 {
			out = append(out, vers[len(vers)-1])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
