//go:build linux

package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/unix"
)

func TestInspectImageRefusesFIFOWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.ppm")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	safego.Go("image-special-file-test", nil, func() {
		_, _, err := loadInspectImage(context.Background(), path)
		done <- err
	})
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("special file error: %v", err)
		}
	case <-time.After(time.Second):
		// Unblock a regressed implementation so the failure leaves no reader.
		fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("blocked image reader did not exit")
		}
		t.Fatal("image inspection opened a FIFO and blocked")
	}
}
