package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/eval"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

// stoppedByTimeout is an exec record state, not a new durable engine state.
const stoppedByTimeout types.RunState = "stopped_by_timeout"

func writeFinalRecord(dir, state, reason string, code int) error {
	raw, err := json.MarshalIndent(eval.FinalState{At: time.Now().UTC(), State: state, Reason: reason, ExitCode: code}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".final-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "final.json"))
}

// Only synchronized recorders are used here: the worker may still be returning
// from a canceled provider or tool. Never read its mutable event/history slices.
func writeStoppedRecords(run *ExecRun, metricsPath string, cause error) error {
	reason := "timeout"
	if cause != nil {
		reason = cause.Error()
	}
	state := string(stoppedByTimeout)
	// Write the final marker first so a second signal cannot leave state=running.
	if err := writeFinalRecord(run.Dir, state, reason, ExitPaused); err != nil {
		return err
	}
	if run.Recorder != nil {
		_ = run.Recorder.WriteFinal(state, reason, ExitPaused)
	}
	run.Metrics.Finish(state)
	snap := run.Metrics.Snapshot()
	if err := telemetry.WriteMetricsFile(filepath.Dir(metricsPath), snap); err != nil {
		return err
	}
	if filepath.Dir(metricsPath) != run.Dir {
		_ = telemetry.WriteMetricsFile(run.Dir, snap)
	}
	_ = run.ATIF.Append(telemetry.ATIFStep{At: time.Now().UTC(), Role: "system", Content: state + ": " + reason})
	if run.Harbor != nil {
		_ = run.Harbor.AppendStep("system", state+": "+reason, snap.Model)
		_ = run.Harbor.SetFinal(snap.Usage.Input, snap.Usage.Output+snap.Usage.Reasoning, snap.Usage.CacheRead+snap.Usage.CacheWrite, snap.CostUSD)
	}
	return nil
}
