package tools

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// TaskBoundarySwitch gates the task-boundary refusals (item 93). Default ON.
// The refusals apply only inside an exec scope.
const TaskBoundarySwitch = "EXCALIBUR_TASK_BOUNDARY"

// BoundaryRefusal is the message for a command or read that reaches outside
// the task. The agent is pointed at the files the task provides.
const BoundaryRefusal = "refused: outside the task boundary; work with the files the task provides"

// errBoundary is returned by the boundary checks.
var errBoundary = errors.New(BoundaryRefusal)

// ExecScope describes the autonomous run this process performs. The
// `excalibur exec` entry point sets it once; library and daemon runs keep the
// zero value, so the scope-only behaviour never applies to them.
type ExecScope struct {
	// Active is set inside `excalibur exec` (eval and exec profiles).
	Active bool
	// HarnessPaths are the harness's own log and state locations. The agent
	// must not read them: the trial records live there.
	HarnessPaths []string
}

var (
	execScopeMu sync.RWMutex
	execScope   ExecScope
)

// SetExecScope installs s as the process exec scope and returns a function
// that restores the previous scope.
func SetExecScope(s ExecScope) (restore func()) {
	execScopeMu.Lock()
	prev := execScope
	execScope = s
	execScopeMu.Unlock()
	return func() {
		execScopeMu.Lock()
		execScope = prev
		execScopeMu.Unlock()
	}
}

// CurrentExecScope returns the active exec scope.
func CurrentExecScope() ExecScope {
	execScopeMu.RLock()
	defer execScopeMu.RUnlock()
	s := execScope
	s.HarnessPaths = append([]string(nil), s.HarnessPaths...)
	return s
}

// hostRootPrefixes are container-runtime and host storage locations that a
// task never needs. Reading them exposes other workloads or the host image.
var hostRootPrefixes = []string{"/run/containerd", "/var/lib/containerd", "/var/lib/docker"}

// boundaryActive reports whether the boundary refusals apply right now.
func boundaryActive() bool {
	return switchDefaultOn(TaskBoundarySwitch) && CurrentExecScope().Active
}

// CheckCommandBoundary refuses a shell command that reaches outside the task
// boundary: paths under another process's root (/proc/<pid>/root), container
// runtime storage, the harness's own logs and state, nsenter, chroot into a
// host root and bind mounts of host paths. The check is lexical on the words
// of the command, which covers quoted paths, redirections and chained
// commands; a path assembled at run time cannot be seen and is not refused.
func CheckCommandBoundary(command string) error {
	if !boundaryActive() {
		return nil
	}
	words := boundaryWords(command)
	harness := CurrentExecScope().HarnessPaths
	for i, w := range words {
		if pathOutsideBoundary(w, harness) {
			return errBoundary
		}
		switch path.Base(w) {
		case "nsenter":
			return errBoundary
		case "chroot":
			if i+1 < len(words) && chrootTargetOutside(words[i+1:], harness) {
				return errBoundary
			}
		case "mount":
			if mountBindsHostPath(words[i+1:], harness) {
				return errBoundary
			}
		}
	}
	return nil
}

// CheckPathBoundary refuses a file read that reaches outside the task
// boundary. Symbolic links are resolved when the target exists, so a link
// inside the task cannot lead out of it.
func CheckPathBoundary(p string) error {
	if !boundaryActive() {
		return nil
	}
	harness := CurrentExecScope().HarnessPaths
	if pathOutsideBoundary(filepath.Clean(p), harness) {
		return errBoundary
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil && pathOutsideBoundary(resolved, harness) {
		return errBoundary
	}
	return nil
}

// boundaryWords splits a command into the words that may name a path or a
// command. Quotes, separators, redirections and assignments all split.
func boundaryWords(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		switch r {
		case ' ', '\t', '\r', '\n', ';', '&', '|', '(', ')', '<', '>', '`', '"', '\'', '=', '$', '{', '}':
			return true
		}
		return false
	})
}

// pathOutsideBoundary reports whether an absolute path names something
// outside the task. Relative paths are not judged here: they resolve against
// the working directory, which the boundary does not restrict by itself.
func pathOutsideBoundary(p string, harness []string) bool {
	if p == "" {
		return false
	}
	clean := path.Clean(p)
	if !strings.HasPrefix(clean, "/") {
		return false
	}
	if procRootEscape(clean) {
		return true
	}
	// The harness's own root is the task's root. Judge what follows it.
	if rest, ok := selfRootRemainder(clean); ok {
		return pathOutsideBoundary(rest, harness)
	}
	for _, prefix := range hostRootPrefixes {
		if within(clean, prefix) {
			return true
		}
	}
	for _, h := range harness {
		if h == "" {
			continue
		}
		if base := path.Clean(h); base != "/" && within(clean, base) {
			return true
		}
	}
	return false
}

// procRootEscape reports whether clean is /proc/<pid>/root for a process
// other than this one, including glob forms such as /proc/*/root.
func procRootEscape(clean string) bool {
	parts := strings.Split(clean, "/")
	if len(parts) < 4 || parts[1] != "proc" || parts[3] != "root" {
		return false
	}
	return !isSelfProcEntry(parts[2])
}

// selfRootRemainder returns the path inside this process's own root when
// clean is /proc/self/root/... or /proc/<own pid>/root/..., so that the
// harness's files stay protected through the self-root alias.
func selfRootRemainder(clean string) (string, bool) {
	parts := strings.Split(clean, "/")
	if len(parts) < 4 || parts[1] != "proc" || parts[3] != "root" || !isSelfProcEntry(parts[2]) {
		return "", false
	}
	return "/" + strings.Join(parts[4:], "/"), true
}

func isSelfProcEntry(name string) bool {
	return name == "self" || name == "thread-self" || name == strconv.Itoa(os.Getpid())
}

// within reports whether p equals base or lies below it.
func within(p, base string) bool {
	return p == base || strings.HasPrefix(p, strings.TrimSuffix(base, "/")+"/")
}

// chrootTargetOutside reports whether the new root of a chroot call (the
// first operand after its options) is a host root.
func chrootTargetOutside(args, harness []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if a == "/" {
			return true
		}
		return pathOutsideBoundary(a, harness)
	}
	return false
}

// mountBindsHostPath reports whether a mount call bind-mounts a host path:
// the source of a --bind, --rbind, -B or "-o bind" form is the root, a
// container-runtime path or a harness path.
func mountBindsHostPath(args, harness []string) bool {
	bind := false
	for _, a := range args {
		switch {
		case a == "--bind" || a == "--rbind" || a == "-B" || a == "-R":
			bind = true
		case strings.HasPrefix(a, "-o") && strings.Contains(a, "bind"):
			bind = true
		}
	}
	if !bind {
		return false
	}
	for _, a := range args {
		if strings.HasPrefix(a, "/") && (a == "/" || pathOutsideBoundary(a, harness)) {
			return true
		}
	}
	return false
}
