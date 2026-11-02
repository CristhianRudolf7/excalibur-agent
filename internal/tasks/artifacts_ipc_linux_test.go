//go:build linux

package tasks

import (
	"net"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLiveSocketAndFIFOArtifactTypes(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "endpoint.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	fifo := filepath.Join(dir, "events.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{socket, fifo} {
		req := ArtifactRequirement{Kind: ArtifactOutput, Path: path, Source: "Create " + path, Expect: []string{"never read"}}
		if pending := CheckArtifactFiles([]ArtifactRequirement{req}, nil, dir, nil, nil); len(pending) != 0 {
			t.Fatal(pending)
		}
	}
}
