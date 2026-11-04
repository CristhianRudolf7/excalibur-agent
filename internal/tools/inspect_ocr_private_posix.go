//go:build !windows

package tools

// os.CreateTemp already restricts the file to its owner on POSIX systems.
func restrictOCRFile(string) error { return nil }
