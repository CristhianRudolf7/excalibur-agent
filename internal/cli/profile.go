package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProfileName is one execution profile of Section 3.6: the same binary
// behaves differently depending on which capabilities are active.
type ProfileName string

// Execution profiles.
const (
	// ProfileDefault is the full assistant with daemon: complete
	// memory, master and conversation, persistent state.
	ProfileDefault ProfileName = "default"
	// ProfileDelegate is for tasks delegated by another agent or the
	// CLI: project-scoped memory only (personal memory off, and
	// --no-memory disables memory entirely), no master or
	// conversation; the task is a level-1 root run like exec (6.5),
	// persistent in <project>/.excalibur/ or in the daemon when it
	// is active.
	ProfileDelegate ProfileName = "delegate"
	// ProfileEval is the frozen benchmark profile of Section 23.2:
	// no memory, no master, ephemeral state with hashes.
	ProfileEval ProfileName = "eval"
)

// ParseProfile resolves a --profile flag value. Empty means the full
// assistant default.
func ParseProfile(s string) (ProfileName, error) {
	switch ProfileName(strings.ToLower(strings.TrimSpace(s))) {
	case "", ProfileDefault:
		return ProfileDefault, nil
	case ProfileDelegate:
		return ProfileDelegate, nil
	case ProfileEval:
		return ProfileEval, nil
	default:
		return "", fmt.Errorf("cli: unknown profile %q (want default|delegate|eval)", s)
	}
}

// DefaultProfileForTask is the profile of Section 3.6 task
// delegation when --profile is omitted: delegate.
func DefaultProfileForTask() ProfileName { return ProfileDelegate }

// ValidateEphemeral enforces the Section 3.6 rule: --ephemeral uses a
// temporary database that is removed on exit, in any profile except
// default.
func ValidateEphemeral(profile ProfileName, ephemeral bool) error {
	if ephemeral && profile == ProfileDefault {
		return fmt.Errorf("cli: --ephemeral is not allowed with the default profile")
	}
	return nil
}

// DataDir resolves the persistent state directory for a profile:
//
//   - eval: <logsDir>/excalibur (the trial database; Harbor mounts
//     /logs/agent from the host trial directory, Section 23.2). The
//     directory must be empty at start unless a profile override is
//     declared; callers enforce that with eval.CheckDataDir.
//   - delegate: <project>/.excalibur when a project directory is
//     given, else the user state directory. When the daemon is active
//     it adopts the same directory (Section 3.6), so tasks stay
//     visible to the master and other clients.
//   - default: the user state directory (persistent assistant state).
//
// With ephemeral set, the caller must use a fresh temporary directory
// instead; EphemeralDir creates one.
func DataDir(profile ProfileName, projectDir, logsDir string) (string, error) {
	switch profile {
	case ProfileEval:
		if logsDir == "" {
			return "", fmt.Errorf("cli: eval profile needs a logs directory")
		}
		return filepath.Join(logsDir, "excalibur"), nil
	case ProfileDelegate:
		if projectDir != "" {
			return filepath.Join(projectDir, ".excalibur"), nil
		}
		return userStateDir()
	case ProfileDefault:
		return userStateDir()
	default:
		return "", fmt.Errorf("cli: unknown profile %q", profile)
	}
}

// EphemeralDir creates a fresh temporary state directory for
// --ephemeral runs. The caller removes it on exit.
func EphemeralDir() (string, error) {
	dir, err := os.MkdirTemp("", "excalibur-ephemeral-")
	if err != nil {
		return "", fmt.Errorf("cli: ephemeral dir: %w", err)
	}
	return dir, nil
}

// userStateDir is the persistent assistant state directory.
func userStateDir() (string, error) {
	if home := os.Getenv("EXCALIBUR_HOME"); home != "" {
		return filepath.Join(home, "state"), nil
	}
	return defaultUserStateDir()
}

// ResolveLogsDir picks the trial log directory for exec/eval output
// (ATIF trajectory, events, metrics). Precedence: explicit flag, the
// EXCALIBUR_LOGS environment, /logs/agent when it exists (the Harbor
// mount inside a trial container, Section 23.2), otherwise a fresh
// temporary directory.
func ResolveLogsDir(flag string) (string, error) {
	if flag != "" {
		if err := os.MkdirAll(flag, 0o755); err != nil {
			return "", fmt.Errorf("cli: logs dir: %w", err)
		}
		return flag, nil
	}
	if env := os.Getenv("EXCALIBUR_LOGS"); env != "" {
		if err := os.MkdirAll(env, 0o755); err != nil {
			return "", fmt.Errorf("cli: logs dir: %w", err)
		}
		return env, nil
	}
	if st, err := os.Stat("/logs/agent"); err == nil && st.IsDir() {
		return "/logs/agent", nil
	}
	dir, err := os.MkdirTemp("", "excalibur-logs-")
	if err != nil {
		return "", fmt.Errorf("cli: logs dir: %w", err)
	}
	return dir, nil
}

// EvalDataReady verifies the Section 23.2 deterministic profile
// control for an eval start: the data directory must be empty unless
// an explicit --profile-override is declared (recorded in metadata
// and ATIF; a deviating trial never compares against a clean one).
func EvalDataReady(dataDir, override string) error {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cli: read eval data dir: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}
	if override == "" {
		return fmt.Errorf("cli: eval data dir %s is not empty; refusing start without --profile-override", dataDir)
	}
	return nil
}
