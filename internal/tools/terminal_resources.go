package tools

import (
	"fmt"
	"strconv"

	"github.com/CristhianRudolf7/excalibur-agent/internal/envsnap"
)

func (m *SessionManager) resourceReader() envsnap.ResourceReader {
	if m.deps.Resources != nil {
		return *m.deps.Resources
	}
	return envsnap.ResourceReader{}
}

func (s *Session) oomDiagnostic(code int) string {
	if !envsnap.EffectiveLimitsEnabled() {
		return ""
	}
	s.mu.Lock()
	o := s.oom
	s.mu.Unlock()
	if o.Killed {
		return fmt.Sprintf("killed by the out-of-memory killer (memory limit %s); reduce parallelism or memory use; retry parallelism: %d",
			envsnap.MemoryLabel(o.MemoryBytes), o.RetryJobs)
	}
	if code == 137 && (!o.CounterAvailable || o.Pressure) {
		return "SIGKILL; possibly out of memory"
	}
	return ""
}

func (s *Session) hasOOMDiagnostic(code int) bool { return s.oomDiagnostic(code) != "" }

// exitLabel is shared by the initial exec result and later background polls.
// A driver may return an ordinary exit code when a descendant was OOM-killed;
// the counter change still belongs in its header without changing that code.
func (s *Session) exitLabel(code int, signal string) string {
	label := strconv.Itoa(code)
	if diagnosis := s.oomDiagnostic(code); diagnosis != "" {
		return label + " (" + diagnosis + ")"
	}
	if signal != "" {
		label += " (" + signal + ")"
	}
	return label
}
