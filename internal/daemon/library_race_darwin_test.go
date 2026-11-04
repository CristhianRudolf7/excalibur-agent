//go:build darwin && race

package daemon

import "time"

func init() {
	// Race builds interpret PDFium to stay within the worker RSS limit.
	// Scanned PDFs also start a rendering worker for OCR; the combined work
	// can exceed 45s on a busy Intel runner even after extraction progresses.
	libraryE2EWaitTimeout = 3 * time.Minute
}
