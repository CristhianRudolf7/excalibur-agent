package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"golang.org/x/sys/windows"
)

// WindowsPTY starts interactive terminal sessions using ConPTY (Section 13.5).
// Windows 10 version 1809 or newer is required.
type WindowsPTY struct{}

var _ PTY = WindowsPTY{}
var conptyKernel = windows.NewLazySystemDLL("kernel32.dll")

// Start launches cmd with a pseudoconsole startup attribute. This backend owns
// waiting: callers use the session's Wait, not cmd.Wait. Process is populated
// for callers that need its PID. Session Close cancels the entire process tree.
// A caller requesting CREATE_SUSPENDED owns job assignment and resumption;
// otherwise Start assigns its own job and resumes before returning.
func (WindowsPTY) Start(cmd *exec.Cmd, ws Winsize) (PTYSession, error) {
	if cmd == nil {
		return nil, errors.New("platform: nil command")
	}
	if cmd.Process != nil {
		return nil, errors.New("platform: command already started")
	}
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	size, err := conptySize(ws)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"CreatePseudoConsole", "ResizePseudoConsole", "ClosePseudoConsole"} {
		if err := conptyKernel.NewProc(name).Find(); err != nil {
			return nil, fmt.Errorf("platform: ConPTY unavailable (requires Windows 10 1809 or newer): %w", err)
		}
	}
	if cmd.SysProcAttr != nil && (cmd.SysProcAttr.Token != 0 || cmd.SysProcAttr.ParentProcess != 0 || len(cmd.SysProcAttr.AdditionalInheritedHandles) != 0) {
		return nil, errors.New("platform: ConPTY does not support token, parent-process or inherited-handle overrides")
	}
	path := cmd.Path
	if path == "" {
		return nil, errors.New("platform: empty command path")
	}
	if !filepath.IsAbs(path) {
		path, err = filepath.Abs(filepath.Join(cmd.Dir, path))
		if err != nil {
			return nil, err
		}
	}
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	args := cmd.Args
	if len(args) == 0 {
		args = []string{cmd.Path}
	}
	line := windows.ComposeCommandLine(args)
	callerSuspended := cmd.SysProcAttr != nil && cmd.SysProcAttr.CreationFlags&windows.CREATE_SUSPENDED != 0
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if cmd.SysProcAttr != nil {
		flags |= cmd.SysProcAttr.CreationFlags
		if cmd.SysProcAttr.CmdLine != "" {
			line = cmd.SysProcAttr.CmdLine
		}
	}
	commandLine, err := windows.UTF16PtrFromString(line)
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		dir, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return nil, err
		}
	}
	env, err := windowsEnvironmentBlock(cmd.Environ())
	if err != nil {
		return nil, err
	}
	inputRead, inputWrite, err := conptyPipe()
	if err != nil {
		return nil, err
	}
	defer inputRead.Close()
	outputRead, outputWrite, err := conptyPipe()
	if err != nil {
		inputWrite.Close()
		return nil, err
	}
	defer outputWrite.Close()
	var console windows.Handle
	if err := windows.CreatePseudoConsole(size, windows.Handle(inputRead.Fd()), windows.Handle(outputWrite.Fd()), 0, &console); err != nil {
		inputWrite.Close()
		outputRead.Close()
		return nil, fmt.Errorf("platform: create ConPTY: %w", err)
	}
	reader, writer := io.Pipe()
	s := &windowsPTYSession{cmd: cmd, input: inputWrite, output: outputRead, reader: reader, console: console, waitDone: make(chan struct{}), callerSuspended: callerSuspended}
	// Always drain native console output, even when Close interrupts a reader.
	// A blocking ClosePseudoConsole otherwise deadlocks on a full output pipe.
	safego.Go("windows-conpty-output", nil, func() {
		_, err := io.Copy(writer, outputRead)
		if errors.Is(err, io.ErrClosedPipe) {
			_, _ = io.Copy(io.Discard, outputRead)
		}
		_ = writer.CloseWithError(err)
	})
	started := false
	defer func() {
		if !started {
			_ = inputRead.Close()
			_ = outputWrite.Close()
			_ = s.Close()
		}
	}()
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	// PSEUDOCONSOLE takes the HPCON value, unlike attributes taking a pointer
	// to data. Pass it directly rather than manufacturing an unsafe Go pointer.
	ok, _, err := conptyKernel.NewProc("UpdateProcThreadAttribute").Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(console), unsafe.Sizeof(console), 0, 0)
	if ok == 0 {
		return nil, fmt.Errorf("platform: ConPTY startup attribute: %w", err)
	}
	startup := windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// Do not reuse the parent console handles. ConPTY supplies the client's
	// standard handles when these are explicitly left unset.
	startup.Flags |= windows.STARTF_USESTDHANDLES
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.HideWindow {
		startup.Flags |= windows.STARTF_USESHOWWINDOW
		startup.ShowWindow = windows.SW_HIDE
	}
	var info windows.ProcessInformation
	if err := windows.CreateProcess(app, commandLine, nil, nil, false, flags, &env[0], dir, &startup.StartupInfo, &info); err != nil {
		return nil, err
	}
	// Release the launcher's console-side copies after creating the client.
	// The console owns its copies; EOF now follows console shutdown.
	_ = inputRead.Close()
	_ = outputWrite.Close()
	defer windows.CloseHandle(info.Process)
	defer windows.CloseHandle(info.Thread)
	process, err := os.FindProcess(int(info.ProcessId))
	if err != nil {
		_ = windows.TerminateProcess(info.Process, 1)
		return nil, err
	}
	cmd.Process = process
	if !callerSuspended {
		// Assign while suspended, so even the first descendant inherits the job.
		if err := AttachJob(process.Pid); err != nil {
			_ = windows.TerminateProcess(info.Process, 1)
			_, _ = process.Wait()
			return nil, err
		}
		if _, err := windows.ResumeThread(info.Thread); err != nil {
			_ = (WindowsProcessGroup{}).Kill(process.Pid)
			_, _ = process.Wait()
			return nil, err
		}
	}
	started = true
	// Reap independently of Wait: terminal callers read through EOF before
	// calling Wait, and ConPTY produces EOF only once its console closes.
	safego.Go("windows-conpty-wait", nil, func() {
		state, err := process.Wait()
		cmd.ProcessState = state
		if err != nil {
			s.waitErr = err
		} else if !state.Success() {
			s.waitErr = &exec.ExitError{ProcessState: state}
		}
		_, _ = terminateRegisteredJob(process.Pid)
		close(s.waitDone)
		s.closeConsole()
	})
	return s, nil
}

// ConPTY requires synchronous pipe handles. os.Pipe may create overlapped
// handles in newer Go versions, so use CreatePipe explicitly.
func conptyPipe() (*os.File, *os.File, error) {
	var read, write windows.Handle
	if err := windows.CreatePipe(&read, &write, nil, 0); err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(read), "conpty-read"), os.NewFile(uintptr(write), "conpty-write"), nil
}

func conptySize(ws Winsize) (windows.Coord, error) {
	if ws.Rows == 0 || ws.Cols == 0 || ws.Rows > 32767 || ws.Cols > 32767 {
		return windows.Coord{}, errors.New("platform: ConPTY dimensions must be between 1 and 32767")
	}
	return windows.Coord{X: int16(ws.Cols), Y: int16(ws.Rows)}, nil
}

type windowsPTYSession struct {
	cmd                    *exec.Cmd
	input, output          *os.File
	reader                 *io.PipeReader
	mu                     sync.Mutex
	console                windows.Handle
	closed                 bool
	callerSuspended        bool
	closeOnce, consoleOnce sync.Once
	waitDone               chan struct{}
	closeErr, waitErr      error
}

var _ PTYSession = (*windowsPTYSession)(nil)

func (s *windowsPTYSession) Read(p []byte) (int, error)  { return s.reader.Read(p) }
func (s *windowsPTYSession) Write(p []byte) (int, error) { return s.input.Write(p) }

func (s *windowsPTYSession) Resize(ws Winsize) error {
	size, err := conptySize(ws)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(s.console, size)
}

func (s *windowsPTYSession) closeConsole() {
	s.consoleOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		windows.ClosePseudoConsole(s.console)
	})
}

// Close interrupts the reader first, letting the output pump drain while the
// console closes. It terminates descendants and releases all session handles.
func (s *windowsPTYSession) Close() error {
	s.closeOnce.Do(func() {
		_ = s.reader.Close()
		_ = s.input.Close()
		if s.cmd.Process != nil {
			// Every successfully launched session owns a job. Never look up the
			// PID again after reaping; it may already name another process.
			if s.callerSuspended {
				// The caller releases its job. Kill also handles attachment failure,
				// when the initial thread is still suspended and owns no job yet.
				s.closeErr = s.cmd.Process.Kill()
				if errors.Is(s.closeErr, os.ErrProcessDone) {
					s.closeErr = nil
				}
			} else {
				_, s.closeErr = terminateRegisteredJob(s.cmd.Process.Pid)
			}
		}
		s.closeConsole()
		_ = s.output.Close()
	})
	return s.closeErr
}

// Wait reports the independently reaped process, exposing exec.ExitError on
// nonzero exit. Closing the console concurrently lets readers reach EOF.
func (s *windowsPTYSession) Wait() error {
	<-s.waitDone
	return s.waitErr
}
