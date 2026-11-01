package attempts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	outs := []Outcome{
		{Index: 0, State: "completed", Summary: "ok", ToolCalls: 5, Verified: 5, FilesChanged: []string{"a.txt"}, DiffStat: "1 changed"},
		{Index: 1, State: "failed", Summary: "bad", ToolErrors: 2},
	}
	rec := ToRecord(outs, 0)
	if err := WriteRecord(dir, rec); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "attempts.json"))
	if err != nil {
		t.Fatal(err)
	}
	var back Record
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Winner != 0 || len(back.Attempts) != 2 || !back.Attempts[0].Winner {
		t.Fatalf("record = %+v", back)
	}
	if back.Attempts[0].ToolCalls != 5 || back.Attempts[1].ToolErrors != 2 {
		t.Fatalf("counts lost: %+v", back.Attempts)
	}
}
