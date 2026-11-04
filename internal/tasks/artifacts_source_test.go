package tasks

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArtifactSourceRemainsBoundedForEveryExtractionForm(t *testing.T) {
	for _, instruction := range []string{
		"Read /app/input.dat",
		"Write /app/result.csv",
		"Install /usr/local/bin/tool",
		"Write ./result.csv",
		"Write `result.csv`",
		"Write result.csv",
		"Listen on port 18081",
		"Serve http://localhost:18082/status",
	} {
		t.Run(instruction, func(t *testing.T) {
			request := strings.Repeat("context ", 17) + instruction
			reqs := ExtractArtifactRequirements(request, 8)
			if len(reqs) == 0 {
				t.Fatal("no requirements extracted")
			}
			for _, req := range reqs {
				if want := strings.Repeat("context ", 14) + "context…"; req.Source != want {
					t.Fatalf("source = %q, want pre-change source %q", req.Source, want)
				}
				if req.IntentSource() != request {
					t.Fatalf("intent source lost trailing instruction: %q", req.IntentSource())
				}
			}
		})
	}
}

func TestArtifactRequirementSerializationUnchanged(t *testing.T) {
	request := strings.Repeat("context ", 17) + "Write /app/result.csv"
	reqs := ExtractArtifactRequirements(request, 8)
	if len(reqs) == 0 || reqs[0].Path != "/app/result.csv" {
		t.Fatalf("missing absolute requirement: %+v", reqs)
	}
	req := reqs[0]
	// These are the exact records emitted before full sentences were retained.
	source := strings.Repeat("context ", 14) + "context…"
	wantJSON := `{"ID":"A1","Kind":"output","Path":"/app/result.csv","Port":0,"URL":"","Expect":null,"Detail":"output /app/result.csv exists as stated","Source":"` + source + `"}`
	gotJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != wantJSON {
		t.Fatalf("JSON changed:\ngot  %s\nwant %s", gotJSON, wantJSON)
	}
	wantYAML := "id: A1\nkind: output\npath: /app/result.csv\nport: 0\nurl: \"\"\nexpect: []\ndetail: output /app/result.csv exists as stated\nsource: " + source + "\n"
	gotYAML, err := yaml.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotYAML) != wantYAML {
		t.Fatalf("YAML changed:\ngot  %s\nwant %s", gotYAML, wantYAML)
	}
	for _, record := range []struct {
		data   []byte
		decode func([]byte, any) error
	}{
		{gotJSON, json.Unmarshal},
		{gotYAML, yaml.Unmarshal},
	} {
		var decoded ArtifactRequirement
		if err := record.decode(record.data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.IntentSource() != source {
			t.Fatalf("decoded requirement must fall back to bounded source: %q", decoded.IntentSource())
		}
	}
}

func TestArtifactIntentSourceFallback(t *testing.T) {
	req := ArtifactRequirement{Source: "Create /app/result.csv"}
	if req.IntentSource() != req.Source {
		t.Fatal("directly constructed requirement lost source")
	}
}
