package main

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestConsoleEchoRestored(t *testing.T) {
	h := windows.Handle(os.Stdin.Fd())
	var original uint32
	if err := windows.GetConsoleMode(h, &original); err != nil {
		t.Skip("no console input in this runner")
	}
	restore, err := disableEcho()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	var hidden uint32
	if err := windows.GetConsoleMode(h, &hidden); err != nil {
		t.Fatal(err)
	}
	if hidden&windows.ENABLE_ECHO_INPUT != 0 {
		t.Fatal("echo still enabled")
	}
	restore()
	var after uint32
	if err := windows.GetConsoleMode(h, &after); err != nil {
		t.Fatal(err)
	}
	if after != original {
		t.Fatalf("console mode = %x; want %x", after, original)
	}
}
