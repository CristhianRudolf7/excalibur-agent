// Notebook tests (Section 8.9): the general board lists
// general-scope tasks, and notes version with history.
package tasks

import (
	"testing"
)

func TestGeneralBoard(t *testing.T) {
	store := NewMemoryStore()
	seedTask(t, store, seedScope(), "T2")
	seedTask(t, store, seedScope(), "T1")
	board := GeneralBoard(store)
	if len(board) != 2 || board[0].Code != "T1" || board[1].Code != "T2" {
		t.Fatalf("board must list general tasks in code order: %+v", board)
	}
}

func TestNotebookVersions(t *testing.T) {
	nb := NewNotebook()
	note := nb.Add("first")
	next, err := nb.Update(note.ID, "second", note.Version)
	if err != nil {
		t.Fatal(err)
	}
	if next.Version != 2 || next.Text != "second" {
		t.Fatalf("update must append a version: %+v", next)
	}
	if _, err := nb.Update(note.ID, "stale", note.Version); err == nil {
		t.Fatal("stale version must be rejected with the current note")
	}
	if len(nb.History(note.ID)) != 2 {
		t.Fatal("history must keep both versions")
	}
	got, err := nb.Get(note.ID)
	if err != nil || got.Text != "second" {
		t.Fatalf("get must return the latest: %+v", got)
	}
}
