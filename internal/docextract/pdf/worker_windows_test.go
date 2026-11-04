package pdf

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"golang.org/x/sys/windows"
)

func TestWindowsWorkerLifecycle(t *testing.T) {
	t.Setenv("PDF_TEST_WORKER", "hang")
	w, err := newWorker(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	q := request{Path: filepath.Join(t.TempDir(), "Unicode document \u03b2.pdf"), Operation: "metadata"}
	r, err := w.call(ctx, q)
	if err != nil || r.Document.PageCount != 3 {
		t.Fatalf("metadata over pipes: %+v, %v", r, err)
	}
	sample, err := w.sampler.Sample(w.cmd.Process.Pid)
	if err != nil || sample.RSSBytes == 0 || sample.CPUSeconds < 0 || sample.Processes < 1 {
		t.Fatalf("worker sample: %+v, %v", sample, err)
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(w.cmd.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	pageCtx, pageCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer pageCancel()
	_, err = w.call(pageCtx, request{Path: q.Path, Operation: "page", Page: 2})
	if !errors.Is(err, context.DeadlineExceeded) || w.cmd != nil {
		t.Fatalf("cancelled worker: %v, cmd=%v", err, w.cmd)
	}
	if state, err := windows.WaitForSingleObject(handle, 2000); err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("worker survived cancellation: %d, %v", state, err)
	}
	if _, err := w.call(ctx, q); err != nil {
		t.Fatalf("restart: %v", err)
	}
	w.options.MemoryLimit = 1
	_, err = w.call(ctx, request{Path: q.Path, Operation: "page", Page: 2})
	if err == nil || !strings.Contains(err.Error(), "memory limit") || w.cmd != nil {
		t.Fatalf("memory limit: %v, cmd=%v", err, w.cmd)
	}
}

type rejectedWorkerGroup struct{ platform.WindowsProcessGroup }

func (rejectedWorkerGroup) AttachJob(int) error { return errors.New("test attachment failure") }

func TestWindowsWorkerAttachmentFailure(t *testing.T) {
	t.Setenv("PDF_TEST_WORKER", "hang")
	w, err := newWorker(Options{ProcessGroup: rejectedWorkerGroup{}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	if err := w.start(); err == nil || !strings.Contains(err.Error(), "test attachment failure") || w.cmd != nil {
		t.Fatalf("attachment failure: %v, cmd=%v", err, w.cmd)
	}
}
