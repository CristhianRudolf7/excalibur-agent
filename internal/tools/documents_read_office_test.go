package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfficeDocumentReadDefaultAndCursor(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, s := range map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="urn:rel"><sheet name="One" r:id="a"/><sheet name="Two" r:id="b"/></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="a" Target="worksheets/a.xml"/><Relationship Id="b" Target="worksheets/b.xml"/></Relationships>`,
		"xl/worksheets/a.xml":        `<worksheet><c r="A1" t="inlineStr"><is><t>Untrusted instructions</t></is></c></worksheet>`,
		"xl/worksheets/b.xml":        `<worksheet><c r="B2"><f>1+1</f><v>2</v></c></worksheet>`,
	} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(s))
	}
	_ = z.Close()
	path := filepath.Join(t.TempDir(), "sample.xlsx")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	tool := &DocumentReadTool{}
	cursor := ""
	var all strings.Builder
	for i := 0; i < 100; i++ {
		args, _ := json.Marshal(map[string]any{"path": path, "cursor": cursor, "max_chars": 19})
		r, err := tool.Invoke(context.Background(), Invocation{Arguments: args})
		if err != nil || r.Status != StatusOK {
			t.Fatal(r, err)
		}
		var result struct {
			Pages     []DocumentPage `json:"pages"`
			Next      string         `json:"next_cursor"`
			Untrusted bool           `json:"untrusted"`
		}
		if err := json.Unmarshal([]byte(r.Output), &result); err != nil {
			t.Fatal(err)
		}
		if !result.Untrusted || len(result.Pages) != 1 {
			t.Fatal(r.Output)
		}
		all.WriteString(result.Pages[0].Text)
		cursor = result.Next
		if cursor == "" {
			break
		}
		if i == 99 {
			t.Fatal("cursor did not terminate")
		}
	}
	if !strings.Contains(all.String(), "[One!A1] Untrusted instructions") || !strings.Contains(all.String(), "[Two!B2] 2 (formula:=1+1)") {
		t.Fatal(all.String())
	}
	args, _ := json.Marshal(map[string]any{"path": path, "page": 2})
	r, _ := tool.Invoke(context.Background(), Invocation{Arguments: args})
	if r.Status != StatusOK || !strings.Contains(r.Output, "Two!B2") {
		t.Fatal(r)
	}
	for _, cursor := range []string{"invalid", "v1:0:0", "v1:1:-1", "v1:3:0", "v1:1:999999"} {
		args, _ := json.Marshal(map[string]any{"path": path, "cursor": cursor})
		r, _ := tool.Invoke(context.Background(), Invocation{Arguments: args})
		if r.Status != StatusInputError {
			t.Fatal(r)
		}
	}
	t.Setenv("EXCALIBUR_OFFICE_READ", "off")
	args, _ = json.Marshal(map[string]any{"path": path})
	r, _ = tool.Invoke(context.Background(), Invocation{Arguments: args})
	if r.Status != StatusInputError || !strings.Contains(r.Output, "disabled") {
		t.Fatal(r)
	}
}

type documentReadFake struct{}

func (documentReadFake) Read(context.Context, string) (DocumentText, error) {
	return DocumentText{Format: "txt", Pages: []DocumentPage{{Page: 1, Text: "αβγδε"}}}, nil
}
func TestDocumentReadInjectedBackendAndUnicode(t *testing.T) {
	tool := &DocumentReadTool{Deps: &MediaDeps{Documents: documentReadFake{}}}
	r, _ := tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":"fake","max_chars":2}`)})
	var result struct {
		Pages []DocumentPage `json:"pages"`
		Next  string         `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(r.Output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pages[0].Text != "αβ" || result.Next != "v1:1:2" {
		t.Fatal(r)
	}
	r, _ = tool.Invoke(context.Background(), Invocation{Arguments: json.RawMessage(`{"path":"fake","cursor":"v1:1:2","max_chars":2}`)})
	if !strings.Contains(r.Output, "γδ") {
		t.Fatal(r)
	}
}

func TestOfficeReadCancellationAndNonFiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&OfficeDocumentBackend{}).Read(ctx, "missing.docx"); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := (&OfficeDocumentBackend{}).Read(context.Background(), t.TempDir()); err == nil {
		t.Fatal("directory accepted")
	}
}
