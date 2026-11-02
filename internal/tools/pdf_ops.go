package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
)

// PDFOpsEnabled controls the real PDF tool. Turning it off fails explicitly;
// the test-only FilePDFBackend is never selected as a production fallback.
func PDFOpsEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_PDF_OPS"))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

func (t *PDFOpsTool) pdfPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	root := ""
	if t.Deps != nil {
		root = t.Deps.Workdir
	}
	if root != "" && !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	// Resolve existing ancestors as well as final files so an output below a
	// symlink cannot escape the configured project directory.
	tail := []string{}
	ancestor := path
	for {
		resolved, e := filepath.EvalSymlinks(ancestor)
		if e == nil {
			path = resolved
			for i := len(tail) - 1; i >= 0; i-- {
				path = filepath.Join(path, tail[i])
			}
			break
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", e
		}
		tail = append(tail, filepath.Base(ancestor))
		ancestor = parent
	}
	if root != "" {
		root, err = filepath.Abs(root)
		if err != nil {
			return "", err
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		rel, e := filepath.Rel(root, path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", errors.New("PDF path is outside the configured workdir")
		}
	}
	return path, nil
}

func (t *PDFOpsTool) resolvePDFPaths(a *PDFRequest) error {
	var err error
	for i, p := range a.Inputs {
		if strings.TrimSpace(p) == "" {
			return errors.New("input path must not be empty")
		}
		a.Inputs[i], err = t.pdfPath(p)
		if err != nil {
			return err
		}
	}
	a.Output, err = t.pdfPath(a.Output)
	if err != nil {
		return err
	}
	a.OutputDir, err = t.pdfPath(a.OutputDir)
	return err
}

func pdfResult(value any, err error, password string) (Result, error) {
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return Result{Status: StatusCanceled, Output: "PDF operation canceled"}, nil
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return Result{Status: StatusTimeout, Output: "PDF operation timed out"}, nil
		}
		// Do not expose library diagnostics for password operations; they may
		// contain a decrypted dictionary or credentials. The secret never leaves
		// the invocation boundary through a result or error.
		message := err.Error()
		if password != "" {
			message = "PDF operation failed; check the password, input and requested operation"
		}
		return Result{Status: StatusToolError, Output: message}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return Result{Status: StatusToolError, Output: "cannot encode PDF result"}, nil
	}
	return Result{Status: StatusOK, Output: string(raw)}, nil
}

func (t *PDFOpsTool) Invoke(ctx context.Context, inv Invocation) (Result, error) {
	if !PDFOpsEnabled() {
		return Result{Status: StatusToolError, Output: "pdf_ops is disabled (EXCALIBUR_PDF_OPS)"}, nil
	}
	if err := ctx.Err(); err != nil {
		return pdfResult(nil, err, "")
	}
	var a PDFRequest
	if err := json.Unmarshal(inv.Arguments, &a); err != nil {
		return Result{Status: StatusInputError, Output: "invalid pdf_ops arguments"}, nil
	}
	if len(a.Inputs) == 0 || len(a.Inputs) > 100 {
		return Result{Status: StatusInputError, Output: "inputs must contain 1..100 files"}, nil
	}
	if a.Action == "rotate" && a.Angle%90 != 0 {
		return Result{Status: StatusInputError, Output: "angle must be a multiple of 90"}, nil
	}
	if err := t.resolvePDFPaths(&a); err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	if strings.HasPrefix(a.Password, "secret://") {
		if t.Deps == nil || t.Deps.PDFSecret == nil {
			return Result{Status: StatusToolError, Output: "PDF password secret resolver is not configured"}, nil
		}
		password, err := t.Deps.PDFSecret(ctx, a.Password)
		if err != nil || password == "" {
			return Result{Status: StatusToolError, Output: "cannot resolve PDF password reference"}, nil
		}
		a.Password = password
	}
	if a.Action == "text" || a.Action == "render" {
		return t.pdfRead(ctx, a)
	}
	backend := t.backend()
	if advanced, ok := backend.(PDFOperationsBackend); ok {
		value, err := advanced.ExecutePDF(ctx, a)
		return pdfResult(value, err, a.Password)
	}
	// Preserve injection of old backends without changing the shared interface.
	if a.Output != "" && !a.Overwrite {
		if _, err := os.Lstat(a.Output); !os.IsNotExist(err) {
			return Result{Status: StatusInputError, Output: "output exists; use overwrite:true to replace"}, nil
		}
	}
	inv.Arguments, _ = json.Marshal(a)
	return t.invokeLegacy(ctx, inv)
}

func (t *PDFOpsTool) pdfRead(ctx context.Context, a PDFRequest) (Result, error) {
	if t.Deps == nil || (a.Action == "text" && t.Deps.PDFText == nil) || (a.Action == "render" && t.Deps.PDFRender == nil) {
		return Result{Status: StatusToolError, Output: "PDF " + a.Action + " backend is not configured"}, nil
	}
	// Password hooks intentionally use the shared extractor interface, which has
	// no credential parameter. Decrypt to a private new file before reading.
	if a.Password != "" {
		return Result{Status: StatusInputError, Output: "decrypt to a new file before text or render"}, nil
	}
	f, err := os.Open(a.Inputs[0])
	if err != nil {
		return pdfResult(nil, err, "")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return pdfResult(nil, err, "")
	}
	if st.Size() > 128<<20 {
		return Result{Status: StatusInputError, Output: "PDF exceeds 128 MiB limit"}, nil
	}
	info, err := readPDFInfo(a.Inputs[0], "")
	if err != nil {
		return pdfResult(nil, err, "")
	}
	selected, err := pdfSelection(a, info.PageCount)
	if err != nil {
		return Result{Status: StatusInputError, Output: err.Error()}, nil
	}
	pages := a.Pages
	if len(a.Ranges) > 0 {
		pages = nil
		for p := 1; p <= info.PageCount; p++ {
			for _, s := range selected {
				var from, to int
				if strings.Contains(s, "-") {
					_, _ = fmt.Sscanf(s, "%d-%d", &from, &to)
				} else {
					_, _ = fmt.Sscanf(s, "%d", &from)
					to = from
				}
				if p >= from && p <= to {
					pages = append(pages, p)
					break
				}
			}
		}
	}
	if a.Action == "text" {
		doc, e := t.Deps.PDFText.Extract(ctx, f, st.Size(), docextract.Options{Pages: pages, MaxPages: 2000})
		return pdfResult(doc, e, "")
	}
	if a.Page < 1 || a.Page > info.PageCount {
		return Result{Status: StatusInputError, Output: "render requires a page within the document"}, nil
	}
	if a.Output == "" {
		return Result{Status: StatusInputError, Output: "render requires an output PNG path"}, nil
	}
	dpi := a.DPI
	if dpi == 0 {
		dpi = 144
	}
	if dpi < 36 || dpi > 300 {
		return Result{Status: StatusInputError, Output: "dpi must be between 36 and 300"}, nil
	}
	png, err := t.Deps.PDFRender.RenderPNG(ctx, f, st.Size(), a.Page, dpi)
	if err != nil {
		return pdfResult(nil, err, "")
	}
	err = pdfPublish(ctx, a.Output, a.Overwrite, func(w *os.File) error { _, e := w.Write(png); return e })
	if err != nil {
		return pdfResult(nil, err, "")
	}
	return pdfResult(map[string]any{"path": a.Output, "sha256": pdfDigest(png), "page_count": 1, "page": a.Page}, nil, "")
}

func (t *PDFOpsTool) ResourceKeys(args json.RawMessage) ([]string, error) {
	var a PDFRequest
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if err := t.resolvePDFPaths(&a); err != nil {
		return nil, err
	}
	var keys []string
	for _, p := range a.Inputs {
		keys = append(keys, "file:"+p)
	}
	if a.Output != "" {
		keys = append(keys, "file:"+a.Output)
	}
	if a.OutputDir != "" {
		keys = append(keys, "file:"+a.OutputDir)
	}
	return keys, nil
}

func pdfDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
