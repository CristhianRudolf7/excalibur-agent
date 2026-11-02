package tools

import (
	"bytes"
	"context"
	"fmt"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePDF writes a small PDF with independently identifiable pages, optional
// AcroForm fields, one attachment and one outline. No external fixtures are used.
func makePDF(t *testing.T, dir, name string, forms bool) string {
	t.Helper()
	objects := []string{
		`<< /Type /Catalog /Pages 2 0 R /Outlines 11 0 R /Names << /EmbeddedFiles << /Names [(note.txt) 14 0 R] >> >>`,
		`<< /Type /Pages /Kids [3 0 R 5 0 R 7 0 R] /Count 3 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 9 0 R >> >> /Contents 4 0 R >>`,
		"",
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 9 0 R >> >> /Contents 6 0 R >>`,
		"",
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 9 0 R >> >> /Contents 8 0 R >>`,
		"",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		`<< /Title (PDF operations fixture) /Author (Excalibur tests) >>`,
		`<< /Type /Outlines /First 12 0 R /Last 12 0 R /Count 1 >>`,
		`<< /Title (First chapter) /Parent 11 0 R /Dest [3 0 R /Fit] >>`,
		"<< /Type /EmbeddedFile /Length 5 >>\nstream\nhello\nendstream",
		`<< /Type /Filespec /F (note.txt) /EF << /F 13 0 R >> >>`,
	}
	objects[0] += " >>"
	for i := 0; i < 3; i++ {
		s := fmt.Sprintf("BT /F1 12 Tf 50 700 Td (Page marker %d) Tj ET", i+1)
		objects[3+i*2] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s)
	}
	if forms {
		objects[0] = strings.TrimSuffix(objects[0], " >>") + " /AcroForm 15 0 R >>"
		objects[2] = strings.TrimSuffix(objects[2], " >>") + " /Annots [16 0 R 17 0 R 18 0 R] >>"
		objects = append(objects,
			`<< /Fields [16 0 R 17 0 R 18 0 R] /DR << /Font << /Helv 9 0 R >> >> /DA (/Helv 12 Tf 0 g) /NeedAppearances true >>`,
			`<< /Type /Annot /Subtype /Widget /FT /Tx /T (full_name) /Rect [50 600 250 625] /P 3 0 R /V (Before) /DA (/Helv 12 Tf 0 g) >>`,
			`<< /Type /Annot /Subtype /Widget /FT /Btn /T (accepted) /Rect [50 550 70 570] /P 3 0 R /V /Off /AS /Off /AP << /N << /Off 19 0 R /Yes 20 0 R >> >> >>`,
			`<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 131072 /T (country) /Rect [50 500 250 525] /P 3 0 R /Opt [(UK) (US)] /V (UK) /DA (/Helv 12 Tf 0 g) >>`,
			"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 0 >>\nstream\n\nendstream",
			"<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length 13 >>\nstream\n0 0 20 20 re f\nendstream",
		)
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, o := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 10 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCPUPDFOperations(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "source.pdf", false)
	b := &CPUPDFBackend{}
	for _, tc := range []struct {
		action       string
		pages        []int
		ranges       []string
		angle, count int
		text         string
	}{
		{action: "merge", count: 6}, {action: "extract_pages", ranges: []string{"2-3"}, count: 2},
		{action: "delete_pages", pages: []int{2}, count: 2}, {action: "reorder", pages: []int{3, 1, 2}, count: 3},
		{action: "rotate", pages: []int{2}, angle: 90, count: 3}, {action: "stamp", text: "Reviewed", count: 3},
		{action: "watermark", text: "Draft", count: 3}, {action: "page_numbers", count: 3},
		{action: "annotate", text: "Note", count: 3}, {action: "optimize", count: 3},
	} {
		t.Run(tc.action, func(t *testing.T) {
			req := PDFRequest{Action: tc.action, Inputs: []string{input}, Output: filepath.Join(dir, tc.action+".pdf"), Pages: tc.pages, Ranges: tc.ranges, Angle: tc.angle, Text: tc.text}
			if tc.action == "merge" {
				req.Inputs = append(req.Inputs, input)
			}
			got, err := b.ExecutePDF(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			out := got.(PDFOutput)
			if out.PageCount != tc.count || len(out.SHA256) != 64 {
				t.Fatalf("bad output: %+v", out)
			}
			if _, err = b.ExecutePDF(context.Background(), PDFRequest{Action: "validate", Inputs: []string{out.Path}}); err != nil {
				t.Fatal(err)
			}
			markers := map[string][]int{"merge": {1, 2, 3, 1, 2, 3}, "extract_pages": {2, 3}, "delete_pages": {1, 3}, "reorder": {3, 1, 2}}
			for p, marker := range markers[tc.action] {
				content, _ := pdfPageForTest(t, out.Path, p+1)
				if !strings.Contains(string(content), fmt.Sprintf("Page marker %d", marker)) {
					t.Fatalf("wrong page %d content: %s", p+1, content)
				}
			}
			if tc.action == "rotate" {
				_, one := pdfPageForTest(t, out.Path, 1)
				_, two := pdfPageForTest(t, out.Path, 2)
				if one != 0 || two != 90 {
					t.Fatalf("page rotations: %d %d", one, two)
				}
			}

		})
	}
	for _, action := range []string{"info", "attachments", "bookmarks", "form_fields"} {
		t.Run(action, func(t *testing.T) {
			got, err := b.ExecutePDF(context.Background(), PDFRequest{Action: action, Inputs: []string{input}})
			if err != nil {
				t.Fatal(err)
			}
			if got == nil {
				t.Fatal("nil read result")
			}
		})
	}
	info, err := readPDFInfo(input, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.PageCount != 3 || info.Encrypted || info.Metadata["title"] != "PDF operations fixture" {
		t.Fatalf("bad info: %+v", info)
	}
	for i, ranges := range [][]string{nil, {"1-2", "3"}} {
		got, err := b.ExecutePDF(context.Background(), PDFRequest{Action: "split", Inputs: []string{input}, OutputDir: filepath.Join(dir, fmt.Sprint(i)), Ranges: ranges})
		if err != nil {
			t.Fatal(err)
		}
		outs := got.(map[string]any)["outputs"].([]PDFOutput)
		want := 3
		if i == 1 {
			want = 2
		}
		if len(outs) != want {
			t.Fatal(outs)
		}
	}
}

func TestCPUPDFEncryptionAndForms(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "form.pdf", true)
	b := &CPUPDFBackend{}
	filled := filepath.Join(dir, "filled.pdf")

	for _, fields := range []map[string]any{{"missing": "value"}, {"accepted": "true"}, {"full_name": true}, {"country": "not-an-option"}} {
		output := filepath.Join(dir, "rejected.pdf")
		_, err := b.ExecutePDF(context.Background(), PDFRequest{Action: "form_fill", Inputs: []string{input}, Output: output, Fields: fields})
		if err == nil {
			t.Fatalf("accepted invalid form fields: %v", fields)
		}
		if _, err = os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("failed form fill published output")
		}
	}
	_, err := b.ExecutePDF(context.Background(), PDFRequest{Action: "form_fill", Inputs: []string{input}, Output: filled, Fields: map[string]any{"full_name": "Ada Lovelace", "accepted": true, "country": "US"}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := readPDFInfo(filled, "")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, f := range info.FormFields {
		values[f.Name] = f.V
	}
	if values["full_name"] != "Ada Lovelace" || values["country"] != "US" || values["accepted"] != "Yes" {
		t.Fatalf("bad filled fields: %+v", info.FormFields)
	}
	locked := filepath.Join(dir, "locked.pdf")
	_, err = b.ExecutePDF(context.Background(), PDFRequest{Action: "form_lock", Inputs: []string{filled}, Output: locked})
	if err != nil {
		t.Fatal(err)
	}
	info, err = readPDFInfo(locked, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range info.FormFields {
		if !f.Locked {
			t.Fatalf("unlocked: %+v", f)
		}
	}
	encrypted := filepath.Join(dir, "encrypted.pdf")
	_, err = b.ExecutePDF(context.Background(), PDFRequest{Action: "encrypt", Inputs: []string{filled}, Output: encrypted, Password: "test-private-password"})
	if err != nil {
		t.Fatal(err)
	}
	info, err = readPDFInfo(encrypted, "test-private-password")
	if err != nil || !info.Encrypted {
		t.Fatalf("encryption: %+v %v", info, err)
	}
	if _, err = readPDFInfo(encrypted, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	decrypted := filepath.Join(dir, "decrypted.pdf")
	_, err = b.ExecutePDF(context.Background(), PDFRequest{Action: "decrypt", Inputs: []string{encrypted}, Output: decrypted, Password: "test-private-password"})
	if err != nil {
		t.Fatal(err)
	}
	info, err = readPDFInfo(decrypted, "")
	if err != nil || info.Encrypted {
		t.Fatalf("decrypt: %+v %v", info, err)
	}
}

func TestCPUPDFFailuresPreserveFiles(t *testing.T) {
	dir := t.TempDir()
	input := makePDF(t, dir, "source.pdf", false)
	before, _ := os.ReadFile(input)
	b := &CPUPDFBackend{}
	for _, req := range []PDFRequest{
		{Action: "rotate", Output: input, Angle: 90}, {Action: "extract_pages", Pages: []int{4}},
		{Action: "extract_pages", Ranges: []string{"3-2"}}, {Action: "rotate", Angle: 45},
		{Action: "redact"}, {Action: "form_flatten"}, {Action: "encrypt"}, {Action: "unknown"},
	} {
		req.Inputs = []string{input}
		if req.Output == "" {
			req.Output = filepath.Join(dir, "out.pdf")
		}
		if _, err := b.ExecutePDF(context.Background(), req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
	after, _ := os.ReadFile(input)
	if !bytes.Equal(before, after) {
		t.Fatal("input changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.ExecutePDF(ctx, PDFRequest{Action: "optimize", Inputs: []string{input}, Output: input, Overwrite: true}); err == nil {
		t.Fatal("cancellation ignored")
	}
	_, err := b.ExecutePDF(context.Background(), PDFRequest{Action: "rotate", Inputs: []string{input}, Output: input, Angle: 90, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = readPDFInfo(input, ""); err != nil {
		t.Fatal(err)
	}
}

func pdfPageForTest(t *testing.T, path string, page int) ([]byte, int) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, err := api.ReadAndValidate(f, pdfConfig(""))
	if err != nil {
		t.Fatal(err)
	}
	d, _, attrs, err := ctx.PageDict(page, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := ctx.PageContent(d, page)
	if err != nil {
		t.Fatal(err)
	}
	return data, attrs.Rotate
}

func TestPDFPublicationChecksCancellationAndClobber(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.pdf")
	ctx, cancel := context.WithCancel(context.Background())
	err := pdfPublish(ctx, out, false, func(w *os.File) error { _, e := w.Write([]byte("complete temporary content")); cancel(); return e })
	if err == nil {
		t.Fatal("published a canceled operation")
	}
	if _, err = os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("canceled output exists")
	}
	err = pdfPublish(context.Background(), out, false, func(w *os.File) error {
		if _, e := w.Write([]byte("operation content")); e != nil {
			return e
		}
		return os.WriteFile(out, []byte("concurrent writer"), 0600)
	})
	if err == nil {
		t.Fatal("clobbered a concurrent writer")
	}
	data, err := os.ReadFile(out)
	if err != nil || string(data) != "concurrent writer" {
		t.Fatal("concurrent content lost")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("temporary files leaked")
	}
}
