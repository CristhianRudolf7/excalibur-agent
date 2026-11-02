package docextract

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"sync"
)

// Registry maps sniffed formats to extractors. It is safe to register while reading.
type Registry struct {
	mu         sync.RWMutex
	extractors map[string]Extractor
}

func NewRegistry() *Registry { return &Registry{extractors: make(map[string]Extractor)} }

func (r *Registry) Register(e Extractor) error {
	if e == nil || len(e.Formats()) == 0 {
		return fmt.Errorf("docextract: extractor has no formats")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extractors == nil {
		r.extractors = make(map[string]Extractor)
	}
	for _, f := range e.Formats() {
		if f == "" {
			return fmt.Errorf("docextract: empty format")
		}
		if _, ok := r.extractors[f]; ok {
			return fmt.Errorf("docextract: duplicate format %s", f)
		}
	}
	for _, f := range e.Formats() {
		r.extractors[f] = e
	}
	return nil
}

func (r *Registry) Get(format string) (Extractor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e := r.extractors[format]
	if e == nil {
		return nil, fmt.Errorf("docextract: unsupported format %q", format)
	}
	return e, nil
}

// Sniff prefers byte signatures to a filename and MIME type. ZIP detection needs
// the complete archive in head; a truncated prefix falls back to name and MIME.
func Sniff(head []byte, name, mediaType string) string {
	switch {
	case bytes.HasPrefix(head, []byte("%PDF")):
		return "pdf"
	case bytes.HasPrefix(head, []byte{137, 80, 78, 71, 13, 10, 26, 10}):
		return "png"
	case bytes.HasPrefix(head, []byte{255, 216, 255}):
		return "jpeg"
	case bytes.HasPrefix(head, []byte("PK")):
		if z, err := zip.NewReader(bytes.NewReader(head), int64(len(head))); err == nil {
			parts := map[string]bool{}
			typ := ""
			for _, f := range z.File {
				parts[f.Name] = true
				if f.Name == "mimetype" && f.UncompressedSize64 <= 256 {
					if rc, err := f.Open(); err == nil {
						b, _ := io.ReadAll(io.LimitReader(rc, 257))
						_ = rc.Close()
						typ = strings.TrimSpace(string(b))
					}
				}
			}
			if f := mimeFormat(typ); f != "" {
				return f
			}
			if parts["[Content_Types].xml"] {
				for _, p := range []struct{ prefix, format string }{{"word/", "docx"}, {"xl/", "xlsx"}, {"ppt/", "pptx"}} {
					for n := range parts {
						if strings.HasPrefix(n, p.prefix) {
							return p.format
						}
					}
				}
			}
		}
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	switch ext {
	case "pdf", "doc", "xls", "ppt", "docx", "xlsx", "pptx", "odt", "ods", "odp", "epub", "txt", "md", "html", "csv", "tsv", "json", "png":
		return ext
	case "jpg", "jpeg":
		return "jpeg"
	case "htm":
		return "html"
	case "markdown":
		return "md"
	}
	mt, _, _ := mime.ParseMediaType(mediaType)
	return mimeFormat(mt)
}

func mimeFormat(mt string) string {
	return map[string]string{"application/pdf": "pdf", "image/png": "png", "image/jpeg": "jpeg", "text/plain": "txt", "text/markdown": "md", "text/html": "html", "text/csv": "csv", "text/tab-separated-values": "tsv", "application/json": "json", "application/epub+zip": "epub", "application/vnd.oasis.opendocument.text": "odt", "application/vnd.oasis.opendocument.spreadsheet": "ods", "application/vnd.oasis.opendocument.presentation": "odp", "application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": "xlsx", "application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx"}[mt]
}
