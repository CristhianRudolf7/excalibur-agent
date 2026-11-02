// Package testpdf builds small, deterministic PDF fixtures without external
// tools, system fonts, or checked-in binary files.
package testpdf

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
)

type Line struct {
	Text      string
	X, Y      float64
	Invisible bool
}
type Page struct {
	Rotation int
	CropBox  *[4]float64

	Lines         []Line
	Image         bool
	Garbled       bool
	Damaged       bool
	Width, Height float64
}
type Options struct {
	Title, Author string
	Labels        bool
	Outline       bool
	Password      string
	Encrypted     bool
}

type writer struct {
	objects []string
	key     []byte
}

func (w *writer) add(s string) int { w.objects = append(w.objects, s); return len(w.objects) }
func (w *writer) stream(s string) int {
	n := len(w.objects) + 1
	if w.key != nil {
		input := append(append([]byte{}, w.key...), byte(n), byte(n>>8), byte(n>>16), 0, 0)
		sum := md5.Sum(input)
		cipher, _ := rc4.NewCipher(sum[:10])
		b := []byte(s)
		cipher.XORKeyStream(b, b)
		s = string(b)
	}
	return w.add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
}
func literal(s string) string {
	return "(" + strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)").Replace(s) + ")"
}

var padding = []byte{0x28, 0xbf, 0x4e, 0x5e, 0x4e, 0x75, 0x8a, 0x41, 0x64, 0x00, 0x4e, 0x56, 0xff, 0xfa, 0x01, 0x08, 0x2e, 0x2e, 0x00, 0xb6, 0xd0, 0x68, 0x3e, 0x80, 0x2f, 0x0c, 0xa9, 0xfe, 0x64, 0x53, 0x69, 0x7a}

func padded(s string) []byte {
	b := append([]byte{}, []byte(s)...)
	b = append(b, padding...)
	return b[:32]
}

// Build returns a complete PDF. Coordinates in Line use PDF's bottom-left origin.
func Build(pages []Page, opts Options) []byte {
	w := &writer{}
	root := w.add("")
	tree := w.add("")
	font := w.add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	cmap := w.stream("/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def /CMapName /PrivateUse def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfrange <20> <7E> <E020> endbfrange endcmap CMapName currentdict /CMap defineresource pop end end")
	garbledFont := w.add(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /ToUnicode %d 0 R >>", cmap))
	image := w.add("<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 3 >>\nstream\n\x80\x80\x80\nendstream")
	info := w.add("<< /Title " + literal(opts.Title) + " /Author " + literal(opts.Author) + " >>")
	encrypt := 0
	id := []byte("fixture-document")
	if opts.Encrypted {
		hash := md5.Sum(padded("owner"))
		cipher, _ := rc4.NewCipher(hash[:5])
		owner := padded(opts.Password)
		cipher.XORKeyStream(owner, owner)
		material := append(padded(opts.Password), owner...)
		permissions := make([]byte, 4)
		binary.LittleEndian.PutUint32(permissions, 0xfffffffc)
		material = append(material, permissions...)
		material = append(material, id...)
		key := md5.Sum(material)
		w.key = append([]byte{}, key[:5]...)
		user := append([]byte{}, padding...)
		cipher, _ = rc4.NewCipher(w.key)
		cipher.XORKeyStream(user, user)
		encrypt = w.add(fmt.Sprintf("<< /Filter /Standard /V 1 /R 2 /Length 40 /O <%x> /U <%x> /P -4 >>", owner, user))
	}
	var refs []int
	for _, p := range pages {
		width, height := p.Width, p.Height
		if width == 0 {
			width = 612
		}
		if height == 0 {
			height = 792
		}
		var content strings.Builder
		if p.Image {
			fmt.Fprintf(&content, "q %g 0 0 %g 0 0 cm /Im Do Q\n", width, height)
		}
		f := font
		if p.Garbled {
			f = garbledFont
		}
		for _, l := range p.Lines {
			mode := 0
			if l.Invisible {
				mode = 3
			}
			fmt.Fprintf(&content, "BT /F 12 Tf %d Tr 1 0 0 1 %g %g Tm %s Tj ET\n", mode, l.X, l.Y, literal(l.Text))
		}
		stream := w.stream(content.String())
		if p.Damaged {
			stream = 999999
		}
		extra := fmt.Sprintf(" /Rotate %d", p.Rotation)
		if p.CropBox != nil {
			c := p.CropBox
			extra += fmt.Sprintf(" /CropBox [%g %g %g %g]", c[0], c[1], c[2], c[3])
		}
		refs = append(refs, w.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %g %g] /Resources << /Font << /F %d 0 R >> /XObject << /Im %d 0 R >> >> /Contents %d 0 R%s >>", tree, width, height, f, image, stream, extra)))
	}
	var kids strings.Builder
	for _, n := range refs {
		fmt.Fprintf(&kids, "%d 0 R ", n)
	}
	w.objects[tree-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(refs))
	extra := ""
	if opts.Labels {
		extra += " /PageLabels << /Nums [0 << /S /r >> 2 << /S /D /St 1 /P (A-) >>] >>"
	}
	if opts.Outline && len(refs) > 0 {
		outline := w.add("")
		bookmark := w.add(fmt.Sprintf("<< /Title (First chapter) /Parent %d 0 R /Dest [%d 0 R /Fit] >>", outline, refs[0]))
		w.objects[outline-1] = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count 1 >>", bookmark, bookmark)
		extra += fmt.Sprintf(" /Outlines %d 0 R", outline)
	}
	w.objects[root-1] = "<< /Type /Catalog /Pages " + fmt.Sprint(tree) + " 0 R" + extra + " >>"
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, s := range w.objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, s)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, n := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", n)
	}
	enc := ""
	if encrypt > 0 {
		enc = fmt.Sprintf(" /Encrypt %d 0 R /ID [<%s> <%s>]", encrypt, hex.EncodeToString(id), hex.EncodeToString(id))
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), root, info, enc, xref)
	return out.Bytes()
}
