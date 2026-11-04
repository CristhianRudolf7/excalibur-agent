//go:build darwin

package vault

import (
	"bytes"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinMemoryLockBestEffort(t *testing.T) {
	lockMemory(nil)
	unlockMemory(nil)
	lockMemory([]byte{})
	unlockMemory([]byte{})
	// Use a separate mapped page so unlocking cannot affect another key's
	// Go heap page. mlock permission/limit errors are intentionally benign.
	page, err := unix.Mmap(-1, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Munmap(page) }()
	copy(page, []byte{0, 255, 1, 128})
	before := append([]byte(nil), page...)
	lockMemory(page)
	unlockMemory(page)
	if !bytes.Equal(page, before) {
		t.Fatal("locking modified key material")
	}
}
