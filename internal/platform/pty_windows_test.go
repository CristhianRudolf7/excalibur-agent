package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

func TestWindowsPTYHelper(t *testing.T) {
	if os.Getenv("EXCALIBUR_PTY_HELPER") == "" {
		return
	}
	if os.Getenv("EXCALIBUR_PTY_HELPER") == "flood" {
		for i := 0; i < 4096; i++ {
			fmt.Println(strings.Repeat("x", 1024))
		}
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	fmt.Println("DIR=" + cwd)
	fmt.Println("ENV=" + os.Getenv("EXCALIBUR_PTY_VALUE"))
	for i, arg := range os.Args {
		if arg == "--" {
			for _, value := range os.Args[i+1:] {
				fmt.Println("ARG=" + strconv.Quote(value))
			}
			break
		}
	}
	os.Exit(0)
}

func collectPTY(t *testing.T, session PTYSession) (string, error) {
	t.Helper()
	type result struct {
		text string
		err  error
	}
	output := make(chan result, 1)
	done := make(chan error, 1)
	safego.Go("windows-pty-test-read", nil, func() { data, err := io.ReadAll(session); output <- result{string(data), err} })
	safego.Go("windows-pty-test-wait", nil, func() { done <- session.Wait() })
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(15 * time.Second):
		_ = session.Close()
		t.Fatal("PTY process did not exit")
	}
	select {
	case r := <-output:
		if r.err != nil {
			t.Fatalf("PTY read: %v", r.err)
		}
		return r.text, waitErr
	case <-time.After(15 * time.Second):
		_ = session.Close()
		t.Fatal("PTY output did not reach EOF")
	}
	return "", nil
}

func TestWindowsPTYEchoResize(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/d", "/c", "echo hi & set /p wait=")
	session, err := (WindowsPTY{}).Start(cmd, Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		t.Fatal("missing cmd.Process")
	}
	if err := session.Resize(Winsize{Rows: 40, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("continue\r\n")); err != nil {
		t.Fatal(err)
	}
	text, err := collectPTY(t, session)
	if err != nil || !strings.Contains(text, "hi") {
		t.Fatalf("output %q, wait %v", text, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := session.Resize(Winsize{Rows: 24, Cols: 80}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("resize closed: %v", err)
	}
}

func TestWindowsPTYArgumentsDirectoryEnvironment(t *testing.T) {
	dir := t.TempDir()
	args := []string{"", "two words", `a"b`, `C:\trailing space\`, "unicode-🙂"}
	commandArgs := append([]string{"-test.run=^TestWindowsPTYHelper$", "--"}, args...)
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "EXCALIBUR_PTY_HELPER=report", "EXCALIBUR_PTY_VALUE=value with spaces")
	session, err := (WindowsPTY{}).Start(cmd, Winsize{Rows: 24, Cols: 200})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	text, err := collectPTY(t, session)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range append([]string{"DIR=" + filepath.Clean(dir), "ENV=value with spaces"}, func() []string {
		var values []string
		for _, arg := range args {
			values = append(values, "ARG="+strconv.Quote(arg))
		}
		return values
	}()...) {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q missing %q", text, want)
		}
	}
}

func TestWindowsPTYWrite(t *testing.T) {
	session, err := (WindowsPTY{}).Start(exec.Command("cmd.exe", "/d", "/q"), Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Write([]byte("echo input-roundtrip\r\nexit\r\n")); err != nil {
		t.Fatal(err)
	}
	text, err := collectPTY(t, session)
	if err != nil || !strings.Contains(text, "input-roundtrip") {
		t.Fatalf("output %q, %v", text, err)
	}
}

func TestWindowsPTYExitError(t *testing.T) {
	session, err := (WindowsPTY{}).Start(exec.Command("cmd.exe", "/d", "/c", "exit 7"), Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	_, err = collectPTY(t, session)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit = %v", err)
	}
	if err2 := session.Wait(); err2 != err {
		t.Fatalf("repeated Wait = %v", err2)
	}
}

func TestWindowsPTYCloseWithoutReader(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsPTYHelper$")
	cmd.Env = append(os.Environ(), "EXCALIBUR_PTY_HELPER=flood")
	session, err := (WindowsPTY{}).Start(cmd, Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	done := make(chan error, 1)
	safego.Go("windows-pty-close-test", nil, func() { done <- session.Close() })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked with unread output")
	}
	_ = session.Wait()
}

func TestWindowsPTYInvalidStart(t *testing.T) {
	if _, err := (WindowsPTY{}).Start(nil, Winsize{Rows: 24, Cols: 80}); err == nil {
		t.Fatal("nil command accepted")
	}
	for _, ws := range []Winsize{{}, {Rows: 1, Cols: 65535}} {
		if _, err := (WindowsPTY{}).Start(exec.Command("cmd.exe"), ws); err == nil {
			t.Fatal("invalid dimensions accepted")
		}
	}
	if _, err := (WindowsPTY{}).Start(exec.Command(filepath.Join(t.TempDir(), "missing.exe")), Winsize{Rows: 24, Cols: 80}); err == nil {
		t.Fatal("missing executable accepted")
	}
}

func TestWindowsPTYReadBeforeWait(t *testing.T) {
	session, err := (WindowsPTY{}).Start(exec.Command("cmd.exe", "/d", "/c", "echo read-first"), Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	done := make(chan error, 1)
	safego.Go("windows-pty-read-first-test", nil, func() {
		data, err := io.ReadAll(session)
		if err == nil && !strings.Contains(string(data), "read-first") {
			err = fmt.Errorf("unexpected output %q", data)
		}
		if err == nil {
			err = session.Wait()
		}
		done <- err
	})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("read-first PTY flow stuck")
	}
}
