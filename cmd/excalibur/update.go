// Command excalibur update: Section 25.3 online update over the
// daemon core. Thin layer only: it verifies a channel manifest and
// payload (files staged by the operator or by the agenda update-check
// job), then stages, swaps, migrates and health-checks through
// daemon.Updater. The drain/migrate/health hooks run inside the
// daemon; the CLI path supplies the image store and reports the
// outcome without losing client messages.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
)

func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	channel := fs.String("channel", "stable", "release channel: stable or beta")
	manifestPath := fs.String("manifest", "", "signed channel manifest file")
	payloadPath := fs.String("payload", "", "new binary payload file")
	pubkeyHex := fs.String("pubkey", daemon.ReleasePublicKey, "ed25519 channel public key (hex)")
	checkOnly := fs.Bool("check", false, "verify manifest and payload only")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *manifestPath == "" && *payloadPath == "" {
		return runReleaseUpdate(*asJSON, *checkOnly, *channel, *pubkeyHex)
	}
	if *manifestPath == "" || *payloadPath == "" {
		return fmt.Errorf("update needs --manifest and --payload files")
	}
	if *pubkeyHex == "" {
		return fmt.Errorf("update needs --pubkey (channel key, hex)")
	}
	pubRaw, err := hex.DecodeString(strings.TrimSpace(*pubkeyHex))
	if err != nil || len(pubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("update: bad --pubkey: need %d hex bytes", ed25519.PublicKeySize)
	}
	manifestRaw, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("update: read manifest: %w", err)
	}
	var m daemon.Manifest
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		return fmt.Errorf("update: parse manifest: %w", err)
	}
	if ch := daemon.UpdateChannel(strings.TrimSpace(*channel)); m.Channel != ch {
		return fmt.Errorf("update: manifest channel %q is not %q", m.Channel, ch)
	}
	payload, err := os.ReadFile(*payloadPath)
	if err != nil {
		return fmt.Errorf("update: read payload: %w", err)
	}
	if err := daemon.VerifyManifest(m, ed25519.PublicKey(pubRaw), payload); err != nil {
		return err
	}
	if *checkOnly {
		fmt.Printf("manifest ok: %s %s\n", m.Channel, m.Version)
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("update: current binary: %w", err)
	}
	binDir := filepath.Dir(exe)
	up := &daemon.Updater{
		Policy: daemon.UpdatePolicy{Channel: m.Channel},
		PubKey: ed25519.PublicKey(pubRaw),
		Images: &daemon.FileImageStore{Dir: binDir},
	}
	rep, err := up.Apply(context.Background(), m, payload)
	if *asJSON {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(raw))
	} else {
		fmt.Printf("update %s -> %s (rollback: %s)\n", rep.From, rep.To, rep.RolledBack)
	}
	return err
}
