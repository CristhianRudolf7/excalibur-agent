package engine

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type recheckGuard struct {
	seen        map[string]bool
	consecutive int
}

func recheckGuardEnabled() bool {
	v, set := envSwitch("EXCALIBUR_RECHECK_GUARD")
	return !set || v
}

// workspaceCheckHash is deliberately conservative: incomplete snapshots do
// not prove absence of changes. Bounds keep large trees from stalling a run.
// Symlinks are recorded without following them outside the working directory.
func workspaceCheckHash(root string) string {
	if root == "" {
		return ""
	}
	h := sha256.New()
	var bytes int64
	files := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		files++
		if files > 20000 {
			return fmt.Errorf("snapshot file limit")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%q %v %d %d\n", rel, info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			bytes += info.Size()
			if bytes > 50<<20 {
				return fmt.Errorf("snapshot byte limit")
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(h, io.LimitReader(f, info.Size()+1))
			_ = f.Close()
			if copyErr != nil {
				return copyErr
			}
			if n != info.Size() {
				return fmt.Errorf("file changed during snapshot")
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%q\n", target)
		} else if !info.IsDir() {
			return fmt.Errorf("unsupported file type")
		}
		return nil
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func (r *Run) recheckSnapshot() string {
	if !recheckGuardEnabled() || r.lastRefusal() == "" {
		return ""
	}
	return workspaceCheckHash(r.Workdir)
}

func (r *Run) observeRecheck(res tools.Result, unchanged bool) string {
	if !recheckGuardEnabled() {
		return ""
	}
	// Normalize only the exec header, preserving meaningful values (including
	// timestamps and process IDs) in the process output itself.
	output := res.Output
	if strings.HasPrefix(output, "exit:") {
		if head, tail, ok := strings.Cut(output, "\n"); ok {
			output = tasks.ResultFingerprint(head) + "\n" + tail
		}
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(output)))
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rechecks.seen == nil {
		r.rechecks.seen = map[string]bool{}
	}
	repeated := r.rechecks.seen[fingerprint]
	r.rechecks.seen[fingerprint] = true
	if r.LastRefusal == "" || !unchanged || !repeated || tasks.LiveSessionOutput(res.Output) {
		r.rechecks.consecutive = 0
		return ""
	}
	r.rechecks.consecutive++
	if r.rechecks.consecutive%8 != 0 {
		return ""
	}
	first := r.LastRefusal
	// Actionable refusals retain their original ordering in the audit records.
	if r.refusals != nil && len(r.refusals.records) > 0 {
		records := r.refusals.records
		latest := records[len(records)-1].Attempt
		for _, rec := range records {
			if rec.Attempt == latest && rec.Review == "" && r.refusals.dropped[rec.Item.ID] == "" {
				first = rec.Item.Missing
				break
			}
		}
	}
	first = strings.TrimSpace(first)
	return fmt.Sprintf("the last %d calls only re-checked unchanged results; the open finish refusal asks for: %s; do that instead", r.rechecks.consecutive, first)
}
