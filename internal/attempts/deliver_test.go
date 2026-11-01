package attempts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
)

const deliverRequest = "Write the final report to `/app/out/report.txt` and build the tool at /app/bin/tool. Port 8080 must serve traffic."

func TestDeliverableRequirementsKeepsOutputKinds(t *testing.T) {
	reqs := DeliverableRequirements(deliverRequest)
	var paths []string
	for _, r := range reqs {
		paths = append(paths, r.Path)
	}
	joined := strings.Join(paths, ",")
	if !strings.Contains(joined, "/app/out/report.txt") {
		t.Fatalf("named output missing from deliverables: %v", paths)
	}
	for _, r := range reqs {
		if r.Kind == "port" || r.Kind == "url" {
			t.Fatalf("ports and urls stay with the engine gate: %+v", r)
		}
	}
}

func TestCheckDeliverablesMapsAbsoluteOutputsOntoCopy(t *testing.T) {
	real := t.TempDir()
	attemptDir := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(filepath.Join(attemptDir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := WorkdirMap{Real: real, Attempt: attemptDir}
	// The extraction also yields a bare "report.txt" that resolves at the
	// workdir root (the completion gate's own behaviour, package D). This
	// test pins the mapping of the absolute output only.
	var reqs []tasks.ArtifactRequirement
	for _, r := range DeliverableRequirements("Write the final report to `" + filepath.Join(real, "out", "report.txt") + "`.") {
		if filepath.IsAbs(r.Path) {
			reqs = append(reqs, r)
		}
	}
	if len(reqs) != 1 {
		t.Fatalf("request must yield one absolute named output, got %+v", reqs)
	}

	// Missing in the copy: pending, phrased with the real path.
	pend := CheckDeliverables(reqs, m, attemptDir)
	if len(pend) == 0 {
		t.Fatal("missing output in the copy must fail the deliverable check")
	}
	if !strings.Contains(strings.Join(pend, "\n"), filepath.Join(real, "out", "report.txt")) {
		t.Fatalf("pending must name the requested path: %v", pend)
	}

	// Written into the copy: passes, and the real directory is never read.
	if err := os.WriteFile(filepath.Join(attemptDir, "out", "report.txt"), []byte("done"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pend := CheckDeliverables(reqs, m, attemptDir); len(pend) != 0 {
		t.Fatalf("output present in the copy must pass: %v", pend)
	}
	if _, err := os.Stat(filepath.Join(real, "out", "report.txt")); !os.IsNotExist(err) {
		t.Fatal("the real directory must not receive the attempt output")
	}
}

func TestCheckDeliverablesEmptyOutputFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "out", "report.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := WorkdirMap{Real: "/nonexistent-real", Attempt: dir}
	reqs := DeliverableRequirements("Write the final report to `out/report.txt`.")
	if len(reqs) == 0 {
		t.Skip("extraction did not yield a relative output for this sentence")
	}
	if pend := CheckDeliverables(reqs, m, dir); len(pend) == 0 {
		t.Fatal("an empty named output must fail (non-empty is part of the check)")
	}
}
