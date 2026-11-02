//go:build !linux

package tools

func (m *SessionManager) trySurvivablePTY(s *Session, command, dir string, env []string) bool {
	return false
}
