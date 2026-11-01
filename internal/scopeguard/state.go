package scopeguard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type state struct {
	Guard                         *Guard
	Only, Required, ProtectedDirs []string
	PreserveExistence             []string
	Counts                        []countRule
	FailedChecks, InlineChecks    map[string]string
	Violations                    []string
	InstructionHash               string
}

// StartRun reuses the initial snapshot after interruption; a resumed run must
// never silently bless changes as a new baseline. State is private and atomic.
func StartRun(root, instruction, id string) (*Guard, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(abs+"\x00"+id)))
	dir := filepath.Join("/tmp/excalibur-originals", "state-"+key)
	path := filepath.Join(dir, "guard.json")
	ih := fmt.Sprintf("%x", sha256.Sum256([]byte(instruction)))
	if b, e := os.ReadFile(path); e == nil {
		var s state
		if e = json.Unmarshal(b, &s); e != nil {
			return nil, e
		}
		if s.Guard == nil || s.InstructionHash != ih {
			return nil, fmt.Errorf("scope snapshot does not match resumed instruction")
		}
		g := s.Guard
		g.only = s.Only
		g.required = s.Required
		g.protectedDirs = s.ProtectedDirs
		g.preserveExistence = s.PreserveExistence
		g.counts = s.Counts
		g.failureChecks = s.FailedChecks
		g.inlineChecks = s.InlineChecks
		g.violations = s.Violations
		g.StatePath = path
		g.instructionHash = ih
		return g, nil
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	g, err := Start(root, instruction)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	g.StatePath = path
	g.instructionHash = ih
	if err = g.save(); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *Guard) save() error {
	if g.StatePath == "" {
		return nil
	}
	s := state{Guard: g, Only: g.only, Required: g.required, ProtectedDirs: g.protectedDirs, PreserveExistence: g.preserveExistence, Counts: g.counts, FailedChecks: g.failureChecks, InlineChecks: g.inlineChecks, Violations: g.violations, InstructionHash: g.instructionHash}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(g.StatePath), ".guard-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), g.StatePath)
}

// Report is safe to emit after the run has stopped.
func (g *Guard) Report() map[string]any {
	reason := g.Check()
	return map[string]any{"enabled": true, "snapshot": g.StatePath, "root": g.Root, "file_count": len(g.Initial), "protected": g.Protected, "input_copies": g.Backups, "constraints": g.Constraints, "git_diff_baseline": g.GitDiffBaseline, "git_diff_final": g.GitDiffCurrent, "violation": reason}
}
