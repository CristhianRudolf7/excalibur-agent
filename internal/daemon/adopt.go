// Adoption of daemon-less tasks (Sections 3.6 and 17.3).
//
// Without a daemon, every delegated task runs as a detached
// `excalibur exec` process with its database in
// <project>/.excalibur/. When the daemon starts, it adopts the tasks
// of the registered projects with the boot algorithm of 17.3 instead
// of re-running them: the register lists the candidate files, the
// importer hands each one to the boot reconciler, and the report tells
// which tasks were adopted, skipped or need review.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TaskFile is one daemon-less task database found under a registered
// project (Section 3.6: <project>/.excalibur/).
type TaskFile struct {
	// ProjectDir is the project root the task belongs to.
	ProjectDir string
	// DBPath is the task database file.
	DBPath string
	// UpdatedAt is the last write, used to order adoption.
	UpdatedAt time.Time
}

// ScanTaskFiles lists <project>/.excalibur/*.db under each registered
// project dir. Missing directories and empty task dirs scan clean:
// a project without delegated tasks contributes nothing.
func ScanTaskFiles(projectDirs []string) ([]TaskFile, error) {
	var out []TaskFile
	for _, dir := range projectDirs {
		taskDir := filepath.Join(dir, ".excalibur")
		entries, err := os.ReadDir(taskDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("daemon: scan %q: %w", taskDir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
				continue
			}
			full := filepath.Join(taskDir, e.Name())
			st, serr := os.Stat(full)
			if serr != nil {
				return nil, fmt.Errorf("daemon: stat %q: %w", full, serr)
			}
			out = append(out, TaskFile{ProjectDir: dir, DBPath: full, UpdatedAt: st.ModTime().UTC()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, nil
}

// TaskImporter hands one daemon-less task to the daemon: it reads the
// task database and registers its unfinished runs for boot
// reconciliation. Implementations own the file format; the daemon only
// orders the calls.
type TaskImporter interface {
	Import(task TaskFile) (runs int, err error)
}

// AdoptReport tells what the daemon did with every task file.
type AdoptReport struct {
	Adopted []string
	Skipped []string
	Failed  map[string]string
}

// AdoptAll imports every task file in order. A failing file does not
// stop the rest: it lands in Failed with its error and adoption
// continues, because one corrupt task database must never wedge boot.
func AdoptAll(tasks []TaskFile, imp TaskImporter) AdoptReport {
	rep := AdoptReport{Failed: map[string]string{}}
	for _, t := range tasks {
		if imp == nil {
			rep.Skipped = append(rep.Skipped, t.DBPath)
			continue
		}
		n, err := imp.Import(t)
		if err != nil {
			rep.Failed[t.DBPath] = err.Error()
			continue
		}
		if n == 0 {
			rep.Skipped = append(rep.Skipped, t.DBPath)
			continue
		}
		rep.Adopted = append(rep.Adopted, t.DBPath)
	}
	return rep
}
