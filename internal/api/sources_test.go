package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

func TestMessageSourcesJSONAndContract(t *testing.T) {
	input := MessageEvent{Role: "assistant", Text: "Claim [1].", Sources: []SourceRef{{N: 1, Kind: "document", DocumentID: "doc", ChunkID: "chunk", Title: "Guide", Page: 12, PageLabel: "10", Paragraph: 3, SectionPath: "Chapter 2", Excerpt: "Claim", Boxes: []docextract.Box{{Page: 12, X0: 1, Y0: 2, X1: 3, Y1: 4}}, MIME: "application/pdf", BlobDigest: "digest", Verbatim: "Guide, p. 10, ¶ 3"}, {N: 2, Kind: "web", Title: "Page", URL: "https://example.test"}}}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip MessageEvent
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, roundtrip) {
		t.Fatalf("roundtrip %s", raw)
	}
	contract, err := os.ReadFile(filepath.Join(contractsDir(t), "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schemas struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(contract, &schemas); err != nil {
		t.Fatal(err)
	}
	if schemas.Defs["MessageEvent"].Properties["sources"] == nil {
		t.Fatal("missing sources array contract")
	}
	typ := reflect.TypeOf(SourceRef{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Tag.Get("json")
		for j, c := range name {
			if c == ',' {
				name = name[:j]
				break
			}
		}
		if schemas.Defs["SourceRef"].Properties[name] == nil {
			t.Errorf("contract lacks %s", name)
		}
	}
}
