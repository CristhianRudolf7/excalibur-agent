package store

import "testing"

func TestPriorityString(t *testing.T) {
	if PriorityInteractive.String() != "interactive" ||
		PriorityJournal.String() != "journal" ||
		PriorityNormal.String() != "normal" ||
		PriorityBackground.String() != "background" {
		t.Fatal("priority names")
	}
	if PriorityInteractive > PriorityJournal || PriorityJournal > PriorityNormal ||
		PriorityNormal > PriorityBackground {
		t.Fatal("priorities must order interactive first")
	}
}

func TestSentinelErrorsDistinct(t *testing.T) {
	errs := []error{ErrBusy, ErrDiskFull, ErrCorrupt, ErrNetworkFS, ErrBlobNotFound, ErrMigrationFailed, ErrWriteCanceled}
	seen := map[string]bool{}
	for _, err := range errs {
		if err == nil || seen[err.Error()] {
			t.Fatalf("duplicate or nil sentinel: %v", err)
		}
		seen[err.Error()] = true
	}
}
