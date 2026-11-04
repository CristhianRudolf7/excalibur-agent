//go:build !windows

package tools

import "os"

// POSIX permits replacement while the source remains open until ExecutePDF returns.
func closePDFInputForPublish(*os.File) error { return nil }
