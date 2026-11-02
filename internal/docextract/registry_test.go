package docextract

import (
	"archive/zip"
	"bytes"
	"testing"
)

type registryExtractor struct {
	Extractor
	formats []string
}

func (e *registryExtractor) Formats() []string { return e.formats }

func TestRegistryRegistrationIsAtomic(t *testing.T) {
	// The zero value supports registration, as does NewRegistry.
	var reg Registry
	pdf := &registryExtractor{formats: []string{"pdf"}}
	if err := reg.Register(pdf); err != nil {
		t.Fatal(err)
	}
	if got, err := reg.Get("pdf"); err != nil || got != pdf {
		t.Fatalf("registered extractor: %v %v", got, err)
	}
	for _, invalid := range []Extractor{
		nil,
		&registryExtractor{},
		&registryExtractor{formats: []string{"txt", ""}},
		&registryExtractor{formats: []string{"txt", "pdf"}},
	} {
		if err := reg.Register(invalid); err == nil {
			t.Fatalf("invalid registration succeeded: %v", invalid)
		}
		if _, err := reg.Get("txt"); err == nil {
			t.Fatal("failed registration left a partial mapping")
		}
		if got, err := reg.Get("pdf"); err != nil || got != pdf {
			t.Fatalf("failed registration replaced an existing extractor: %v %v", got, err)
		}
	}
	text := &registryExtractor{formats: []string{"txt", "md"}}
	if err := reg.Register(text); err != nil {
		t.Fatal(err)
	}
	for _, format := range text.formats {
		if got, err := reg.Get(format); err != nil || got != text {
			t.Fatalf("format %s: %v %v", format, got, err)
		}
	}
}

func TestSniff(t *testing.T) {
	for _, tc := range []struct{ data, name, mime, want string }{{"%PDF-1.7", "a.txt", "text/plain", "pdf"}, {"\x89PNG\r\n\x1a\n", "a.pdf", "", "png"}, {"\xff\xd8\xff", "a.txt", "", "jpeg"}, {"", "a.MD", "application/pdf", "md"}, {"", "", "text/plain; charset=utf-8", "txt"}, {"", "file.unknown", "", ""}} {
		if got := Sniff([]byte(tc.data), tc.name, tc.mime); got != tc.want {
			t.Fatalf("%+v: %s", tc, got)
		}
	}
	for _, tc := range []struct{ part, content, want string }{{"word/document.xml", "", "docx"}, {"xl/workbook.xml", "", "xlsx"}, {"ppt/presentation.xml", "", "pptx"}, {"mimetype", "application/vnd.oasis.opendocument.text", "odt"}, {"mimetype", "application/epub+zip", "epub"}} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, _ := z.Create("[Content_Types].xml")
		_, _ = f.Write([]byte("types"))
		f, _ = z.Create(tc.part)
		_, _ = f.Write([]byte(tc.content))
		_ = z.Close()
		if got := Sniff(b.Bytes(), "wrong.pdf", ""); got != tc.want {
			t.Fatalf("%s: %s", tc.part, got)
		}
	}
}
