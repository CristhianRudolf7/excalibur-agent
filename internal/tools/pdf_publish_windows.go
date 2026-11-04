package tools

import "os"

func closePDFInputForPublish(f *os.File) error { return f.Close() }
