// Package legacy bridges legacy Office files to the native OOXML readers using
// an installed LibreOffice executable. Originals remain in the library blob store.
package legacy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	officereader "github.com/CristhianRudolf7/excalibur-agent/internal/office/read"
)

// Extractor requires LibreOffice only when opening a legacy file. The binary
// path is injectable so tests never depend on host Office installations.
type Extractor struct {
	Format string
	Binary string
}

func New(format string) *Extractor     { return &Extractor{Format: format} }
func (e *Extractor) Formats() []string { return []string{e.Format} }
func (e *Extractor) Extract(ctx context.Context, r io.ReaderAt, size int64, opts docextract.Options) (docextract.Document, error) {
	if runtime.GOOS != "linux" {
		return docextract.Document{}, errors.ErrUnsupported
	}
	target := map[string]string{"xls": "xlsx", "doc": "docx", "ppt": "pptx"}[e.Format]
	if target == "" {
		return docextract.Document{}, fmt.Errorf("unsupported legacy format %q", e.Format)
	}
	if size < 0 || size > 64<<20 {
		return docextract.Document{}, fmt.Errorf("legacy Office input exceeds 64 MiB")
	}
	binary := e.Binary
	if binary == "" {
		var err error
		binary, err = exec.LookPath("libreoffice")
		if err != nil {
			binary, err = exec.LookPath("soffice")
		}
		if err != nil {
			return docextract.Document{}, fmt.Errorf("legacy Office extraction requires LibreOffice: %w", errors.ErrUnsupported)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp("", "excalibur-office-*")
	if err != nil {
		return docextract.Document{}, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input."+e.Format)
	data, err := io.ReadAll(io.NewSectionReader(r, 0, size))
	if err != nil {
		return docextract.Document{}, err
	}
	if err = os.WriteFile(input, data, 0600); err != nil {
		return docextract.Document{}, err
	}
	profile := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "profile")}).String()
	cmd := exec.CommandContext(ctx, binary, "-env:UserInstallation="+profile, "--headless", "--convert-to", target, "--outdir", dir, input)
	// Do not include converter diagnostics: documents may contain private text.
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return docextract.Document{}, ctx.Err()
		}
		return docextract.Document{}, fmt.Errorf("legacy Office conversion failed")
	}
	output, err := os.Open(filepath.Join(dir, "input."+target))
	if err != nil {
		return docextract.Document{}, fmt.Errorf("legacy Office conversion produced no document")
	}
	defer output.Close()
	stat, err := output.Stat()
	if err != nil {
		return docextract.Document{}, err
	}
	if stat.Size() > 64<<20 {
		return docextract.Document{}, fmt.Errorf("converted Office file exceeds 64 MiB")
	}
	converted, err := io.ReadAll(io.LimitReader(output, 64<<20))
	if err != nil {
		return docextract.Document{}, err
	}
	doc, err := officereader.New(target).Extract(ctx, bytes.NewReader(converted), int64(len(converted)), opts)
	doc.Format = e.Format
	return doc, err
}
