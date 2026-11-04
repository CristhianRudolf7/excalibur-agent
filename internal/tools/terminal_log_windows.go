package tools

import (
	"io"
	"os"
)

// MSYS shells reject append-only Windows handles as standard output. Give the
// child a writable handle positioned after existing data. The session and its
// descendants share this file cursor; the parent only reads through another handle.
func openSessionLog(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
