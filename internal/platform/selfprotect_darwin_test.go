//go:build darwin

package platform

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestReexecRemovesInstruction(t *testing.T) {
	const marker = "w17-private-instruction-9842"
	if phase := os.Getenv("W17_REEXEC_TEST"); phase != "" {
		if phase == "first" {
			argv := append([]string(nil), os.Args...)
			argv[len(argv)-1] = "[request transferred]"
			if err := os.Setenv("W17_REEXEC_TEST", "second"); err != nil {
				os.Exit(3)
			}
			env := append(os.Environ(), "W17_ORIGINAL_PID="+strconv.Itoa(os.Getpid()))
			if err := Reexec(argv, env); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		raw, err := unix.SysctlRaw("kern.procargs2", os.Getpid())
		_, args, parseErr := parseDarwinProcArgs(raw)
		raw = []byte(strings.Join(args, " "))
		if err != nil || parseErr != nil || strings.Contains(string(raw), marker) || !strings.Contains(string(raw), "[request transferred]") || os.Getenv("W17_ORIGINAL_PID") != strconv.Itoa(os.Getpid()) {
			os.Exit(2)
		}
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestReexecRemovesInstruction$", "--", marker)
	cmd.Env = append(os.Environ(), "W17_REEXEC_TEST=first")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reexec: %v %s", err, out)
	}
}

func TestProtectedSignalTargets(t *testing.T) {
	targets, err := ProtectedSignalTargets()
	if err != nil {
		t.Fatal(err)
	}
	found := map[int]bool{}
	for _, p := range targets {
		found[p.PID] = true
		if p.Group <= 0 || p.Name == "" {
			t.Fatalf("bad target: %+v", p)
		}
	}
	if !found[os.Getpid()] || !found[os.Getppid()] {
		t.Fatalf("missing self or parent: %v", found)
	}
}

func darwinProcArgsFixture(executable string, args []string) []byte {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(args)))
	buf.WriteString(executable)
	buf.Write([]byte{0, 0, 0})
	for _, arg := range args {
		buf.WriteString(arg)
		buf.WriteByte(0)
	}
	buf.WriteString("PRIVATE_ENV=must-not-be-part-of-command\x00")
	return buf.Bytes()
}

func TestDarwinProcArgs(t *testing.T) {
	for _, args := range [][]string{{"excalibur", "daemon", "start"}, {"name with spaces", "", "a=b", "last"}, nil} {
		raw := darwinProcArgsFixture("/Applications/Test App/excalibur", args)
		executable, got, err := parseDarwinProcArgs(raw)
		if err != nil || executable != "/Applications/Test App/excalibur" || !reflect.DeepEqual(got, args) {
			t.Fatal("procargs2 parse failed")
		}
	}
	for _, raw := range [][]byte{nil, {1, 0, 0}, {255, 255, 255, 255, 0}, {1, 0, 0, 0, 'p'}, {2, 0, 0, 0, 'p', 0, 'a', 0, 'b'}} {
		if _, _, err := parseDarwinProcArgs(raw); err == nil {
			t.Fatal("malformed procargs2 accepted")
		}
	}
}

func TestDarwinSignalTargetsFallback(t *testing.T) {
	makeProc := func(pid, group int, name string) unix.KinfoProc {
		var p unix.KinfoProc
		p.Proc.P_pid = int32(pid)
		p.Eproc.Pgid = int32(group)
		copy(p.Proc.P_comm[:], name)
		return p
	}
	procs := []unix.KinfoProc{makeProc(10, 20, "self"), makeProc(9, 21, "parent\x00stale"), makeProc(11, 20, "sibling"), makeProc(12, 21, "peer"), makeProc(13, 22, "unrelated")}
	targets := darwinSignalTargets(procs, 10, 9, map[int]bool{20: true, 21: true}, func(pid int) ([]byte, error) {
		switch pid {
		case 9:
			return nil, unix.EPERM
		case 11:
			return nil, unix.EINVAL
		case 12:
			return []byte{1}, nil
		}
		return darwinProcArgsFixture("/bin/self", []string{"self", "daemon"}), nil
	})
	if len(targets) != 4 {
		t.Fatalf("protected count = %d", len(targets))
	}
	for _, target := range targets {
		if target.PID == 9 && target.Name != "parent" {
			t.Fatal("comm did not stop at first NUL")
		}
		if target.PID == 10 {
			if target.Command != "self daemon" {
				t.Fatal("missing command")
			}
		} else if target.Command != target.Name {
			t.Fatal("missing comm fallback")
		}
		if strings.Contains(target.Command, "PRIVATE_ENV") {
			t.Fatal("environment exposed")
		}
	}
}
