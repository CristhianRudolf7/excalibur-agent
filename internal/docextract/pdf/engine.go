// Package pdf extracts native PDF text and renders pages with a bounded,
// pure-Go WebAssembly engine. Production calls run in an isolated worker.
package pdf

import (
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	pdferrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

var (
	ErrEncrypted   = errors.New("pdf: encrypted document")
	ErrTooLarge    = errors.New("pdf: document exceeds limits")
	ErrUnsupported = errors.ErrUnsupported
)

const maxInput = 64 << 20

var compilationCache = newCompilationCache()

// Platforms may select a backend that fits the isolated worker's RSS budget.
// The default preserves wazero's native compiler on existing platforms.
var newPDFRuntimeConfig = wazero.NewRuntimeConfig

func newCompilationCache() wazero.CompilationCache {
	if dir, err := os.UserCacheDir(); err == nil {
		if cache, err := wazero.NewCompilationCacheWithDir(filepath.Join(dir, "excalibur", "pdf-wasm")); err == nil {
			return cache
		}
	}
	return wazero.NewCompilationCache()
}

type engine struct {
	pool pdfium.Pool
	api  pdfium.Pdfium
	doc  references.FPDF_DOCUMENT
}

func openEngine(ctx context.Context, r io.ReaderAt, size int64) (*engine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if size <= 0 {
		return nil, errors.New("pdf: empty input")
	}
	if size > maxInput {
		return nil, ErrTooLarge
	}
	pool, err := webassembly.Init(webassembly.Config{Context: ctx, MaxTotal: 1, FSConfig: wazero.NewFSConfig(), RuntimeConfig: newPDFRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).WithMemoryLimitPages(8192).WithCloseOnContextDone(true).WithCompilationCache(compilationCache), Stdout: io.Discard, Stderr: io.Discard})
	if err != nil {
		return nil, err
	}
	e := &engine{pool: pool}
	e.api, err = pool.GetInstanceWithContext(ctx)
	if err != nil {
		e.close()
		return nil, err
	}
	opened, err := e.api.OpenDocument(&requests.OpenDocument{FileReader: io.NewSectionReader(r, 0, size), FileReaderSize: size})
	if err != nil {
		e.close()
		if errors.Is(err, pdferrors.ErrPassword) || errors.Is(err, pdferrors.ErrSecurity) {
			return nil, ErrEncrypted
		}
		return nil, err
	}
	e.doc = opened.Document
	security, err := e.api.FPDF_GetSecurityHandlerRevision(&requests.FPDF_GetSecurityHandlerRevision{Document: e.doc})
	if err != nil {
		e.close()
		return nil, err
	}
	if security.SecurityHandlerRevision >= 0 {
		e.close()
		return nil, ErrEncrypted
	}
	return e, nil
}
func (e *engine) close() {
	if e.api != nil {
		_ = e.api.Close()
	}
	if e.pool != nil {
		_ = e.pool.Close()
	}
}
func (e *engine) pageRef(n int) requests.Page {
	return requests.Page{ByIndex: &requests.PageByIndex{Document: e.doc, Index: n - 1}}
}
func (e *engine) metadata(maxPages int) (docextract.Document, error) {
	d := docextract.Document{Format: "pdf", Paginated: true}
	count, err := e.api.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: e.doc})
	if err != nil {
		return d, err
	}
	d.PageCount = count.PageCount
	if d.PageCount > maxPages {
		return d, ErrTooLarge
	}
	if m, err := e.api.GetMetaData(&requests.GetMetaData{Document: e.doc}); err == nil {
		for _, t := range m.Tags {
			switch t.Tag {
			case "Title":
				d.Title = t.Value
			case "Author":
				d.Author = t.Value
			}
		}
	} else {
		d.Warnings = append(d.Warnings, "metadata_unavailable")
	}
	// Walk bookmarks ourselves so malformed/cyclic trees cannot cause unbounded recursion.
	seen := map[references.FPDF_BOOKMARK]bool{}
	var walk func(*references.FPDF_BOOKMARK, int) error
	walk = func(parent *references.FPDF_BOOKMARK, level int) error {
		if level > 32 {
			return errors.New("bookmark depth exceeded")
		}
		child, err := e.api.FPDFBookmark_GetFirstChild(&requests.FPDFBookmark_GetFirstChild{Document: e.doc, Bookmark: parent})
		if err != nil {
			return err
		}
		for ref := child.Bookmark; ref != nil; {
			if seen[*ref] || len(seen) >= 10000 {
				return errors.New("bookmark limit exceeded")
			}
			seen[*ref] = true
			title, err := e.api.FPDFBookmark_GetTitle(&requests.FPDFBookmark_GetTitle{Bookmark: *ref})
			if err != nil {
				return err
			}
			dest, err := e.api.FPDFBookmark_GetDest(&requests.FPDFBookmark_GetDest{Document: e.doc, Bookmark: *ref})
			if err != nil {
				return err
			}
			target := dest.Dest
			if target == nil {
				if a, err := e.api.FPDFBookmark_GetAction(&requests.FPDFBookmark_GetAction{Bookmark: *ref}); err == nil && a.Action != nil {
					if ad, err := e.api.FPDFAction_GetDest(&requests.FPDFAction_GetDest{Document: e.doc, Action: *a.Action}); err == nil {
						target = ad.Dest
					}
				}
			}
			if target != nil {
				if idx, err := e.api.FPDFDest_GetDestPageIndex(&requests.FPDFDest_GetDestPageIndex{Document: e.doc, Dest: *target}); err == nil && idx.Index >= 0 && idx.Index < d.PageCount {
					d.Outline = append(d.Outline, docextract.OutlineEntry{Title: title.Title, Level: level, Page: idx.Index + 1})
				}
			}
			if err := walk(ref, level+1); err != nil {
				return err
			}
			next, err := e.api.FPDFBookmark_GetNextSibling(&requests.FPDFBookmark_GetNextSibling{Document: e.doc, Bookmark: *ref})
			if err != nil {
				return err
			}
			ref = next.Bookmark
		}
		return nil
	}
	if err := walk(nil, 1); err != nil {
		d.Warnings = append(d.Warnings, "outline_incomplete")
	}
	return d, nil
}
func (e *engine) page(n int) (docextract.Page, error) {
	p := docextract.Page{Number: n, Method: docextract.MethodNone, Blocks: []docextract.Block{}}
	ref := e.pageRef(n)
	dim, err := e.api.GetPageSize(&requests.GetPageSize{Page: ref})
	if err != nil {
		return p, err
	}
	p.Width, p.Height = dim.Width, dim.Height
	if p.Width <= 0 || p.Height <= 0 || math.IsNaN(p.Width*p.Height) || math.IsInf(p.Width*p.Height, 0) {
		return p, errors.New("pdf: invalid page dimensions")
	}
	if l, err := e.api.FPDF_GetPageLabel(&requests.FPDF_GetPageLabel{Document: e.doc, Page: n - 1}); err == nil {
		p.Label = l.Label
	}
	text, err := e.api.GetPageTextStructured(&requests.GetPageTextStructured{Page: ref, Mode: requests.GetPageTextStructuredModeChars})
	if err != nil {
		return p, err
	}
	if len(text.Chars) > 1000000 {
		return p, ErrTooLarge
	}
	// PDFium reports its discretionary end-of-line hyphen marker as U+0002.
	// Check IsHyphen so actual control characters still lower page quality.
	var textPage references.FPDF_TEXTPAGE
	loaded := false
	for i, c := range text.Chars {
		if c.Text != "\x02" {
			continue
		}
		if !loaded {
			tp, err := e.api.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: ref})
			if err != nil {
				break
			}
			textPage = tp.TextPage
			loaded = true
		}
		if hyphen, err := e.api.FPDFText_IsHyphen(&requests.FPDFText_IsHyphen{TextPage: textPage, Index: i}); err == nil && hyphen.IsHyphen {
			c.Text = "-"
		}
	}
	if loaded {
		_, _ = e.api.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: textPage})
	}
	lines := charLines(text.Chars, n, p.Height)
	coverage := 0.0
	if objects, err := e.api.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: ref}); err == nil {
		if objects.Count > 100000 {
			return p, ErrTooLarge
		}
		for i := 0; i < objects.Count; i++ {
			obj, err := e.api.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: ref, Index: i})
			if err != nil {
				continue
			}
			typ, err := e.api.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
			if err != nil || typ.Type != enums.FPDF_PAGEOBJ_IMAGE {
				continue
			}
			if b, err := e.api.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: obj.PageObject}); err == nil {
				coverage += math.Max(0, math.Min(p.Width, float64(b.Right))-math.Max(0, float64(b.Left))) * math.Max(0, math.Min(p.Height, float64(b.Top))-math.Max(0, float64(b.Bottom))) / (p.Width * p.Height)
			}
		}
	}
	p.Blocks = lines
	if err := e.visualBoxes(ref, &p); err != nil {
		return p, err
	}
	var nativeText strings.Builder
	for _, c := range text.Chars {
		nativeText.WriteString(c.Text)
	}
	p.Quality = quality([]docextract.Block{{Text: nativeText.String()}}, p.Width*p.Height, math.Min(1, coverage))
	if p.Quality >= qualityMin() {
		p.Method = docextract.MethodNative
	}
	return p, nil
}
func (e *engine) render(n, dpi int) ([]byte, error) {
	ref := e.pageRef(n)
	dim, err := e.api.GetPageSize(&requests.GetPageSize{Page: ref})
	if err != nil {
		return nil, err
	}
	if dim.Width <= 0 || dim.Height <= 0 || math.IsNaN(dim.Width*dim.Height) || math.IsInf(dim.Width*dim.Height, 0) || dim.Width*float64(dpi)/72 > 12000000 || dim.Height*float64(dpi)/72 > 12000000 {
		return nil, ErrTooLarge
	}
	dimensions, err := e.api.GetPageSizeInPixels(&requests.GetPageSizeInPixels{Page: ref, DPI: dpi})
	if err != nil {
		return nil, err
	}
	if dimensions.Width <= 0 || dimensions.Height <= 0 || int64(dimensions.Width)*int64(dimensions.Height) > 12000000 {
		return nil, ErrTooLarge
	}
	out, err := e.api.RenderToFile(&requests.RenderToFile{RenderPageInDPI: &requests.RenderPageInDPI{Page: ref, DPI: dpi}, OutputFormat: requests.RenderToFileOutputFormatPNG, OutputTarget: requests.RenderToFileOutputTargetBytes})
	if err != nil {
		return nil, err
	}
	if out.ImageBytes == nil {
		return nil, errors.New("pdf: renderer returned no image")
	}
	return *out.ImageBytes, nil
}

// visualBoxes derives the page-to-render affine transform from PDFium. This
// handles non-zero crop origins and intrinsic page rotation without rounding
// the resulting text boxes to display pixels.
func (e *engine) visualBoxes(ref requests.Page, p *docextract.Page) error {
	if p.Width > 1000000 || p.Height > 1000000 {
		return ErrTooLarge
	}
	type point struct{ x, y float64 }
	var basis [3]point
	for i, input := range []point{{0, 0}, {1, 0}, {0, 1}} {
		out, err := e.api.FPDF_PageToDevice(&requests.FPDF_PageToDevice{Page: ref, SizeX: int(math.Round(p.Width * 1000)), SizeY: int(math.Round(p.Height * 1000)), PageX: input.x, PageY: input.y})
		if err != nil {
			return err
		}
		basis[i] = point{float64(out.DeviceX) / 1000, float64(out.DeviceY) / 1000}
	}
	transform := func(x, y float64) point {
		return point{basis[0].x + x*(basis[1].x-basis[0].x) + y*(basis[2].x-basis[0].x), basis[0].y + x*(basis[1].y-basis[0].y) + y*(basis[2].y-basis[0].y)}
	}
	for i := range p.Blocks {
		for j, b := range p.Blocks[i].Boxes {
			corners := []point{transform(b.X0, p.Height-b.Y0), transform(b.X1, p.Height-b.Y0), transform(b.X0, p.Height-b.Y1), transform(b.X1, p.Height-b.Y1)}
			b.X0, b.Y0, b.X1, b.Y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, c := range corners {
				b.X0 = math.Min(b.X0, c.x)
				b.X1 = math.Max(b.X1, c.x)
				b.Y0 = math.Min(b.Y0, c.y)
				b.Y1 = math.Max(b.Y1, c.y)
			}
			p.Blocks[i].Boxes[j] = b
		}
	}
	return nil
}
