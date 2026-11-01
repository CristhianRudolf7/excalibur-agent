package engine

import (
	"strings"
	"testing"
)

func TestBroadReverificationChecklist(t *testing.T) {
	e, _ := testEngine(&fakeEndpoint{}, scriptedRunner(), passingGate())
	for _, tc := range []struct {
		request string
		want    bool
	}{
		{"Remove all obsolete references.", true},
		{"Replace every occurrence of the old identifier.", true},
		{"Delete any leftover copies.", true},
		{"Sanitize ALL files.", true},
		{"Redact every private value.", true},
		{"Finish replacing all obsolete identifiers.", true},
		{"After removing every old entry, save the file.", true},
		{"Replace the first occurrence.", false},
		{"List all files.", false},
		{"Remove the small widget.", false},
		{"Allow replacements in the editor.", false},
	} {
		t.Run(tc.request, func(t *testing.T) {
			run := explicitRun()
			run.Request = tc.request
			if got := e.confirmationText(run); strings.Contains(got, broadVerificationItem) != tc.want {
				t.Fatal(got)
			}
		})
	}
	run := explicitRun()
	run.Request = "Remove all obsolete references."
	t.Setenv("EXCALIBUR_BROAD_REVERIFICATION", "off")
	if got := e.confirmationText(run); strings.Contains(got, broadVerificationItem) {
		t.Fatal(got)
	}
}
