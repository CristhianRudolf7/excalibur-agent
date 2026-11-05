//go:build linux

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

func TestSIGTERMContext(t *testing.T) {
	t.Setenv("EXCALIBUR_DEADLINE", "on")
	if os.Getenv("EXCALIBUR_TEST_SIGTERM") == "1" {
		ctx := noSignalContext()
		fmt.Println("signal-handler-ready")
		<-ctx.Done()
		if !errors.Is(context.Cause(ctx), cli.ErrTermination) {
			os.Exit(2)
		}
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSIGTERMContext$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_TEST_SIGTERM=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	scan := bufio.NewScanner(out)
	if !scan.Scan() || scan.Text() != "signal-handler-ready" {
		t.Fatal("signal handler did not start")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
