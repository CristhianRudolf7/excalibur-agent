//go:build darwin

package main

import (
	"flag"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
)

// PrepareInstructionArgv inserts this flag before --, so the test
// binary accepts the same handoff flag as the production exec command.
var darwinInstructionFileFlag = flag.String("instruction-file", "", "private instruction handoff test")

func TestDarwinExecInstructionHandoff(t *testing.T) {
	const request = "darwin-private-handoff-test-marker"
	if !argvReexecSupported() {
		t.Fatal("Darwin reexec gate disabled")
	}
	if os.Getenv("EXCALIBUR_DARWIN_HANDOFF_HELPER") == "1" {
		if path := os.Getenv(cli.InstructionFileEnv); path != "" {
			if *darwinInstructionFileFlag != path {
				t.Fatal("handoff flag and environment differ")
			}
			text, err := cli.ConsumeInstructionFile("")
			if err != nil || text != request {
				t.Fatal("instruction file was not consumed correctly")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("instruction file remains")
			}
			if strings.Contains(strings.Join(os.Args, " "), request) {
				t.Fatal("instruction remains in argv")
			}
			if os.Getenv("EXCALIBUR_DARWIN_HANDOFF_PID") != strconv.Itoa(os.Getpid()) {
				t.Fatal("reexec changed PID")
			}
			if os.Getenv(cli.InstructionFileEnv) != "" {
				t.Fatal("handoff environment remains")
			}
			return
		}
		argv, env, path, err := cli.PrepareInstructionArgv(os.Args, os.Environ(), request)
		if err != nil || path == "" {
			t.Fatal("could not prepare handoff")
		}
		defer func() { _ = os.Remove(path) }()
		env = append(env, "EXCALIBUR_DARWIN_HANDOFF_PID="+strconv.Itoa(os.Getpid()))
		if err := platform.Reexec(argv, env); err != nil {
			t.Fatal(err)
		}
		t.Fatal("reexec returned")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDarwinExecInstructionHandoff$", "--", request)
	cmd.Env = append(os.Environ(), "EXCALIBUR_DARWIN_HANDOFF_HELPER=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("handoff helper: %v: %s", err, out)
	}
}
