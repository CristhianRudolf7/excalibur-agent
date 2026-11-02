package opc

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture() *Package {
	p := New()
	p.Parts["doc.xml"] = []byte(`<root/>`)
	p.Parts["opaque.bin"] = []byte{0, 1, 2, 255}
	p.Types(map[string]string{"opaque.bin": "application/octet-stream"})
	p.SetRelationships("_rels/.rels", []Relationship{{ID: "r1", Type: OfficeRel + "officeDocument", Target: "doc.xml"}})
	return p
}
func TestRoundTripAtomic(t *testing.T) {
	p := fixture()
	f := filepath.Join(t.TempDir(), "a.docx")
	h, issues, e := WriteAtomic(f, p, "", false)
	if e != nil || len(issues) != 0 {
		t.Fatal(e, issues)
	}
	b, _ := os.ReadFile(f)
	q, e := Open(b)
	if e != nil || !bytes.Equal(q.Parts["opaque.bin"], p.Parts["opaque.bin"]) {
		t.Fatal(e)
	}
	if _, _, e = WriteAtomic(f, p, "wrong", true); e == nil || !strings.Contains(e.Error(), h) {
		t.Fatal(e)
	}
	q.Parts["doc.xml"] = []byte(`<broken>`)
	if _, _, e = WriteAtomic(f, q, h, true); e == nil {
		t.Fatal("invalid accepted")
	}
	after, _ := os.ReadFile(f)
	if !bytes.Equal(b, after) {
		t.Fatal("failed write changed file")
	}
	if _, _, e = WriteAtomic(f, p, "", false); e == nil {
		t.Fatal("clobbered")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(f), ".office-*"))
	if len(files) > 0 {
		t.Fatal(files)
	}
}
func TestUnsafeZipAndMissingTarget(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("../bad")
	_, _ = w.Write([]byte("x"))
	_ = z.Close()
	if _, e := Open(b.Bytes()); e == nil {
		t.Fatal("unsafe path")
	}
	p := fixture()
	delete(p.Parts, "doc.xml")
	if len(p.Validate()) == 0 {
		t.Fatal("missing target")
	}
}

func TestValidationRejectsMalformedXMLAndDecodesTargetURIs(t *testing.T) {
	for _, xml := range []string{`text<root/>`, `<!DOCTYPE root><root/>`, `<root/><root/>`, strings.Repeat(`<a>`, 257) + strings.Repeat(`</a>`, 257)} {
		p := fixture()
		p.Parts["doc.xml"] = []byte(xml)
		if len(p.Validate()) == 0 {
			t.Fatalf("accepted %s", xml[:min(len(xml), 40)])
		}
	}
	p := fixture()
	p.Parts["with space.xml"] = []byte(`<root/>`)
	p.SetRelationships("_rels/.rels", []Relationship{{ID: "r1", Type: OfficeRel + "officeDocument", Target: "with%20space.xml"}})
	if issues := p.Validate(); len(issues) > 0 {
		t.Fatal(issues)
	}
	p.Parts["_rels/.rels"] = []byte(`<wrong xmlns="` + RelNS + `"/>`)
	if len(p.Validate()) == 0 {
		t.Fatal("accepted bad relationship root")
	}
}
