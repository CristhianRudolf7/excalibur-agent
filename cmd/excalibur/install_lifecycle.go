package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func installedListen() string {
	if listen := strings.TrimSpace(os.Getenv("EXCALIBUR_LISTEN")); listen != "" {
		return listen
	}
	if dir, err := installDirs().ConfigDir(); err == nil {
		if raw, err := os.ReadFile(filepath.Join(dir, "listen.addr")); err == nil && strings.TrimSpace(string(raw)) != "" {
			return strings.TrimSpace(string(raw))
		}
	}
	return "127.0.0.1:7787"
}

func saveInstalledListen(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("install: invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("install: listen address must be loopback")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("install: listen port must be between 1 and 65535")
	}
	dir, err := installDirs().ConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "listen.addr"), []byte(listen+"\n"), 0600)
}

func installedURL() string { return "http://" + installedListen() }

func runOpen(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("open takes no arguments")
	}
	fmt.Println(installedURL())
	return nil
}

func runUninstall(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	purge := fs.Bool("purge", false, "also remove user data, configuration and cache")
	if err := fs.Parse(args); err != nil {
		return err
	}
	service, ok := installService().(interface{ Uninstall() error })
	if !ok {
		return fmt.Errorf("uninstall: platform service removal unavailable")
	}
	if err := service.Uninstall(); err != nil {
		return err
	}
	rt, err := installDirs().RuntimeDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(daemon.RuntimeAddrDir(rt), daemonPidFile)); err == nil {
		if err := runDaemonStop(nil); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if *purge {
		dirs := installDirs()
		for _, resolve := range []func() (string, error){dirs.DataDir, dirs.ConfigDir, dirs.CacheDir} {
			dir, err := resolve()
			if err != nil {
				return err
			}
			if err := purgeInstallDir(dir); err != nil {
				return err
			}
		}
	}
	fmt.Println("Excalibur service removed; binary remains available for reinstall.")
	return nil
}

// startInstalledDaemon also handles minimal Linux sessions without a user bus
// and the Windows Run-key backend, which only starts at the next logon.
func startInstalledDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return startInstalledDaemonFrom(exe)
}

func startInstalledDaemonFrom(exe string) error {
	client := &http.Client{Timeout: time.Second}
	healthy := func() bool {
		resp, err := client.Get(installedURL() + "/healthz")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}
	for n := 0; n < 10; n++ {
		if healthy() {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	data, err := installDirs().DataDir()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(data, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(exe, "daemon", "start")
	cmd.Stdout, cmd.Stderr = log, log
	detachInstalledProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for {
		if healthy() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("daemon did not become healthy; see %s", log.Name())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Windows stores the binary under its data directory. Preserve that subtree
// while purging user state, since an executable cannot remove itself there.
func purgeInstallDir(dir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(dir, exe)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return os.RemoveAll(dir)
	}
	keep := strings.Split(relative, string(os.PathSeparator))[0]
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != keep {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
