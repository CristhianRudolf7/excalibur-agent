package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// HideInstructionArgvSwitch gates the argv hand-off of the exec request
// (item 22). When on (the default), `excalibur exec` moves the request text
// out of its argument vector into a private temporary file before it execs
// itself, so `ps` and `pgrep -f` inside a task never see the task text.
const HideInstructionArgvSwitch = "EXCALIBUR_HIDE_INSTRUCTION_ARGV"

// InstructionFileEnv names the environment variable that carries the path of
// the request file into the re-executed process. The re-executed process
// removes the variable after reading the file, so tool subprocesses do not
// inherit it.
const InstructionFileEnv = "EXCALIBUR_INSTRUCTION_FILE"

// InstructionFileFlag is the exec flag that names the request file.
const InstructionFileFlag = "--instruction-file"

// HideInstructionArgvEnabled reports whether the argv hand-off applies.
func HideInstructionArgvEnabled() bool {
	v, ok := os.LookupEnv(HideInstructionArgvSwitch)
	if !ok || len(strings.TrimSpace(v)) == 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "off", "false", "no":
		return false
	default:
		return true
	}
}

// PrepareInstructionArgv plans the hand-off of request out of argv. It writes
// the request to a new private file (mode 0600), and returns the argument
// vector with the last occurrence of request after the program name replaced
// by InstructionFileFlag and the file path, and the environment with
// InstructionFileEnv set to that path. The inputs are not modified.
//
// The function is a no-op, returning path "", when request is empty, when
// request is not in argv, or when the environment already names a request
// file: that last condition is the loop guard, so a process started by the
// hand-off never hands off again.
func PrepareInstructionArgv(argv, env []string, request string) (newArgv, newEnv []string, path string, err error) {
	newArgv, newEnv = argv, env
	if request == "" {
		return newArgv, newEnv, "", nil
	}
	if envHasKey(env, InstructionFileEnv) {
		return newArgv, newEnv, "", nil
	}
	idx := -1
	for i := 1; i < len(argv); i++ {
		if argv[i] == request {
			idx = i
		}
	}
	if idx < 0 {
		return newArgv, newEnv, "", nil
	}
	// Minimal container images may not contain the temporary directory yet.
	// Create it privately without changing permissions on an existing one.
	if err := os.MkdirAll(os.TempDir(), 0o700); err != nil {
		return argv, env, "", fmt.Errorf("create instruction directory: %w", err)
	}
	f, err := os.CreateTemp("", "excalibur-instruction-*")
	if err != nil {
		return argv, env, "", fmt.Errorf("create instruction file: %w", err)
	}
	path = f.Name()
	if err := protectInstructionFile(path); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return argv, env, "", fmt.Errorf("protect instruction file: %w", err)
	}
	if _, err := f.WriteString(request); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return argv, env, "", fmt.Errorf("write instruction file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return argv, env, "", fmt.Errorf("close instruction file: %w", err)
	}
	newArgv = make([]string, 0, len(argv)+1)
	// Put the hand-off flag before any positional-only separator.
	insert := idx
	for i := 1; i < idx; i++ {
		if argv[i] == "--" {
			insert = i
			break
		}
	}
	newArgv = append(newArgv, argv[:insert]...)
	newArgv = append(newArgv, InstructionFileFlag, path)
	newArgv = append(newArgv, argv[insert:idx]...)
	newArgv = append(newArgv, argv[idx+1:]...)
	newEnv = make([]string, 0, len(env)+1)
	newEnv = append(newEnv, env...)
	newEnv = append(newEnv, InstructionFileEnv+"="+path)
	return newArgv, newEnv, path, nil
}

// ErrNoInstructionFile reports that neither the flag nor the environment
// names a request file.
var ErrNoInstructionFile = errors.New("no instruction file named")

// ConsumeInstructionFile returns the request text from the file named by
// flagPath, or by InstructionFileEnv when flagPath is empty. The file must be
// a regular file. It is removed after reading, and the environment variable
// is unset, so the request exists in one place only.
func ConsumeInstructionFile(flagPath string) (string, error) {
	path := flagPath
	if path == "" {
		path = os.Getenv(InstructionFileEnv)
	}
	_ = os.Unsetenv(InstructionFileEnv)
	if path == "" {
		return "", ErrNoInstructionFile
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("instruction file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("instruction file %s is not a regular file", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("instruction file: %w", err)
	}
	_ = os.Remove(path)
	return string(raw), nil
}

// envHasKey reports whether the environment list defines key.
func envHasKey(env []string, key string) bool {
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && k == key {
			return true
		}
	}
	return false
}
