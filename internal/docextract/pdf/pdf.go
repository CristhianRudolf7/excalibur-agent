package pdf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// Options configures an extractor. Zero values select production defaults.
type Options struct {
	InProcess       bool                     // Tests only: execute WebAssembly without a child process.
	Timeout         time.Duration            // Per document; default EXCALIBUR_PDF_TIMEOUT or 10m.
	PageTimeout     time.Duration            // Per page (including worker startup); default 30s.
	MemoryLimit     uint64                   // Worker RSS limit; default 1 GiB (2 GiB in Linux race builds). Wasm linear memory is capped at 512 MiB.
	WorkerPath      string                   // Defaults to the current executable.
	ProcessGroup    platform.ProcessGroup    // Optional platform implementation.
	ResourceSampler platform.ResourceSampler // Optional platform implementation.
}

// Extractor implements both the shared extractor and renderer contracts.
// It is safe to reuse concurrently; each call owns its worker and input file.
type Extractor struct{ options Options }

func New(options Options) *Extractor { return &Extractor{options: options} }
func (*Extractor) Formats() []string { return []string{"pdf"} }

var _ docextract.Extractor = (*Extractor)(nil)
var _ docextract.Renderer = (*Extractor)(nil)

func enabled() bool {
	v, err := strconv.ParseBool(os.Getenv("EXCALIBUR_PDF_ENABLED"))
	return err != nil || v
}
func (e *Extractor) timeout() time.Duration {
	if e.options.Timeout > 0 {
		return e.options.Timeout
	}
	if d, err := time.ParseDuration(os.Getenv("EXCALIBUR_PDF_TIMEOUT")); err == nil && d > 0 {
		return d
	}
	return 10 * time.Minute
}
func (e *Extractor) pageTimeout() time.Duration {
	if e.options.PageTimeout > 0 {
		return e.options.PageTimeout
	}
	return workerOperationTimeout
}
func (e *Extractor) session(ctx context.Context) (session, error) {
	if e.options.InProcess {
		return &localSession{root: ctx}, nil
	}
	if !workerSupported() {
		return nil, ErrUnsupported
	}
	return newWorker(e.options)
}
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts docextract.Options) (docextract.Document, error) {
	var d docextract.Document
	if err := ctx.Err(); err != nil {
		return d, err
	}
	if !enabled() {
		return d, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	path, err := stage(ctx, r, size)
	if err != nil {
		return d, err
	}
	defer os.Remove(path)
	s, err := e.session(ctx)
	if err != nil {
		return d, err
	}
	defer s.close()
	maxPages := opts.MaxPages
	if maxPages <= 0 {
		maxPages = 2000
	}
	result, err := s.call(ctx, request{Path: path, Operation: "metadata", MaxPages: maxPages})
	if err != nil {
		return d, err
	}
	d = result.Document
	pages := opts.Pages
	if len(pages) == 0 {
		pages = make([]int, d.PageCount)
		for i := range pages {
			pages[i] = i + 1
		}
	}
	seen := map[int]bool{}
	for _, n := range pages {
		if n < 1 || n > d.PageCount || seen[n] {
			return d, fmt.Errorf("pdf: invalid or duplicate page %d", n)
		}
		seen[n] = true
	}
	for i, n := range pages {
		if err := ctx.Err(); err != nil {
			return d, err
		}
		result, err := e.call(ctx, s, request{Path: path, Operation: "page", Page: n})
		if ctx.Err() != nil {
			return d, ctx.Err()
		}
		p := result.Page
		if err != nil {
			p = docextract.Page{Number: n, Method: docextract.MethodNone, Blocks: []docextract.Block{}, Warnings: []string{"page_failed: " + err.Error()}}
		}
		d.Pages = append(d.Pages, p)
		if opts.Progress != nil {
			opts.Progress(i+1, len(pages))
		}
	}
	cleanup(&d)
	return d, nil
}
func (e *Extractor) call(ctx context.Context, s session, q request) (response, error) {
	pageCtx, cancel := context.WithTimeout(ctx, e.pageTimeout())
	defer cancel()
	return s.call(pageCtx, q)
}
func (e *Extractor) RenderPNG(ctx context.Context, r io.ReaderAt, size int64, page, dpi int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !enabled() {
		return nil, ErrUnsupported
	}
	if page < 1 {
		return nil, errors.New("pdf: page must be 1-based")
	}
	if dpi == 0 {
		dpi = 150
	}
	if dpi < 72 || dpi > 300 {
		return nil, errors.New("pdf: DPI must be between 72 and 300")
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	path, err := stage(ctx, r, size)
	if err != nil {
		return nil, err
	}
	defer os.Remove(path)
	s, err := e.session(ctx)
	if err != nil {
		return nil, err
	}
	defer s.close()
	result, err := e.call(ctx, s, request{Path: path, Operation: "render", Page: page, DPI: dpi})
	return result.PNG, err
}
func stage(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	if size <= 0 {
		return "", errors.New("pdf: empty input")
	}
	if size > maxInput {
		return "", ErrTooLarge
	}
	f, err := os.CreateTemp("", "excalibur-pdf-*.pdf")
	if err != nil {
		return "", err
	}
	name := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	reader := io.NewSectionReader(r, 0, size)
	buf := make([]byte, 64<<10)
	remaining := size
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := reader.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", werr
			}
			remaining -= int64(n)
		}
		if err != nil {
			if err == io.EOF && remaining == 0 {
				break
			}
			return "", err
		}
		if n == 0 {
			return "", io.ErrNoProgress
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
