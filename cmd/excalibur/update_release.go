package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
)

func runReleaseUpdate(asJSON, checkOnly bool, channel, key string) error {
	if channel != "stable" {
		return fmt.Errorf("public releases use --channel stable; beta needs a local signed manifest")
	}
	pub, err := hex.DecodeString(strings.TrimSpace(key))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("update: release public key is not configured; the release owner must set daemon.ReleasePublicKey")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	release, err := daemon.FetchRelease(ctx, os.Getenv("EXCALIBUR_BASE_URL"), runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	manifest, _, err := release.Verify(ed25519.PublicKey(pub))
	if err != nil {
		return err
	}
	if checkOnly {
		fmt.Println("release verified:", manifest.Version)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	drain := &releaseUpdateDrain{}
	updater := daemon.Updater{PubKey: ed25519.PublicKey(pub), Images: &daemon.FileImageStore{Dir: filepath.Dir(exe), Version: version}, Drain: drain, Health: &releaseUpdateHealth{exe: exe, version: manifest.Version, drain: drain}}
	report, err := updater.ApplyRelease(ctx, release)
	if err != nil && drain.stopped {
		// The state machine has restored the old image on health failure. Start it
		// again as well: restoring a file alone does not restore service availability.
		if restartErr := restartReleaseService(exe); restartErr != nil {
			err = fmt.Errorf("%w; service restart failed: %v", err, restartErr)
		}
	}
	if asJSON {
		raw, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Printf("update %s -> %s (rollback: %s)\n", report.From, report.To, report.RolledBack)
	}
	return err
}

type releaseUpdateDrain struct{ stopped bool }

func (d *releaseUpdateDrain) StopIntake() error {
	rt, err := installDirs().RuntimeDir()
	if err != nil {
		return err
	}
	addr := daemon.ReadAddrFile(daemon.RuntimeAddrDir(rt))
	if addr == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := daemon.CallDaemon(ctx, addr, "updater", api.MethodRunList, map[string]any{})
	if err != nil {
		return fmt.Errorf("update: cannot confirm daemon is idle: %w", err)
	}
	var result struct {
		Runs []api.RunStatusResult `json:"runs"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return err
	}
	for _, run := range result.Runs {
		switch run.State {
		case types.RunCompleted, types.RunCompletedPending, types.RunFailed, types.RunCanceled:
		default:
			return fmt.Errorf("update: run %s is %s; finish or cancel it before updating", run.RunID, run.State)
		}
	}
	// Stop cleanly through the PID rendezvous; successful exits do not trigger
	// the native supervisors' restart-on-failure policies.
	if err := runDaemonStop(nil); err != nil {
		return err
	}
	d.stopped = true
	return nil
}
func (*releaseUpdateDrain) ParkActive() (int, error)      { return 0, nil }
func (*releaseUpdateDrain) WaitTools(time.Duration) error { return nil }
func (*releaseUpdateDrain) ConfirmJournal() error         { return nil }

type releaseUpdateHealth struct {
	exe, version string
	drain        *releaseUpdateDrain
}

func (h *releaseUpdateHealth) Check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, h.exe, "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "excalibur "+h.version {
		return fmt.Errorf("new binary failed version health check")
	}
	if h.drain.stopped {
		return restartReleaseService(h.exe)
	}
	return nil
}
func restartReleaseService(exe string) error {
	old, present := os.LookupEnv("EXCALIBUR_EXEC_PATH")
	if err := os.Setenv("EXCALIBUR_EXEC_PATH", exe); err != nil {
		return err
	}
	defer func() {
		if present {
			_ = os.Setenv("EXCALIBUR_EXEC_PATH", old)
		} else {
			_ = os.Unsetenv("EXCALIBUR_EXEC_PATH")
		}
	}()

	if err := installServiceForRelease().Install(); err != nil {
		return err
	}
	return startInstalledDaemonFrom(exe)
}
