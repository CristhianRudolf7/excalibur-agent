package engine

import (
	"strings"
	"testing"
)

func TestChecklistRanksNumericLimits(t *testing.T) {
	request := `This descriptive prose explains how the system works.
Write the results into /work/results.json.
The implementation must remain understandable to maintainers.
/work/results.json
"/work/a path with spaces.json"
Must work.
- Total memory must stay below 4.8e10 bytes.
- Total memory must stay below 4.8e10 bytes.
1. Response time must be <= 200 ms.
| Error rate | must stay under 2% |
At least 8 workers must remain available. The final output must include a summary! It must also include verification evidence.`
	items := checklistItems(request, 8)
	if len(items) != 8 {
		t.Fatalf("%d: %v", len(items), items)
	}
	for i, want := range []string{"4.8e10", "<= 200", "2%", "At least 8"} {
		if !strings.Contains(items[i], want) {
			t.Fatalf("rank %d: %v", i, items)
		}
	}
	for _, item := range items {
		if len(strings.Fields(item)) < 4 || strings.HasPrefix(item, "This descriptive") {
			t.Fatal(item)
		}
	}
	if got := checklistItems(request, 0); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestChecklistCapsAndDeduplicates(t *testing.T) {
	req := ""
	for _, n := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		req += "- Runtime must stay below " + n + " seconds.\n"
	}
	req += "Runtime   must stay below 1 seconds.\n"
	got := checklistItems(req, 12)
	if len(got) != 8 || !strings.Contains(got[0], "1 seconds") || !strings.Contains(got[7], "8 seconds") {
		t.Fatal(got)
	}
	t.Setenv("EXCALIBUR_CHECKLIST_LIMIT_RANKING", "off")
	if len(checklistItems(req, 12)) < 9 {
		t.Fatal("legacy switch ignored")
	}
}
