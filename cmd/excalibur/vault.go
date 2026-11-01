// Vault subcommands (Section 20.1). Values enter only through the
// terminal without echo or through --stdin, never as arguments, and
// `get` is the only command that prints a value, on explicit request.
// Metadata listings never carry values.
//
// Entry operations run against the local vault.db under the data
// directory. Lock and unlock go through the running daemon when one is
// reachable, so its in-memory state changes. Rotation refuses to run
// beside a daemon: the daemon holds the current data key.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
	"github.com/CristhianRudolf7/excalibur-agent/internal/daemon"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// vaultFlags registers the flags every vault subcommand shares.
func vaultFlags(fs *flag.FlagSet) (dataDir, passFile *string) {
	d := fs.String("data-dir", "", "state directory (default: platform data dir)")
	p := fs.String("passphrase-file", "", "file holding the vault passphrase (mode 0600) for unattended unlock")
	return d, p
}

// resolveDataDir returns the state directory of the vault.
func resolveDataDir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	d, err := installDirs().DataDir()
	if err != nil {
		return "", fmt.Errorf("vault: data dir: %w", err)
	}
	return d, nil
}

// openVaultLocal opens the vault for one command. Without a passphrase
// file the platform keyring is the key source when reachable.
func openVaultLocal(ctx context.Context, dataDir, passFile string) (*vault.Vault, error) {
	var kr = daemonKeyring()
	if passFile != "" {
		kr = nil
	}
	return daemon.OpenVault(ctx, daemon.VaultOpenOptions{
		Path:           filepath.Join(dataDir, "vault.db"),
		Keyring:        kr,
		PassphraseFile: passFile,
	})
}

// unlockForUse unlocks a locked vault for an operation that touches
// entries, prompting for the passphrase when no keyring or passphrase
// file supplied the key.
func unlockForUse(ctx context.Context, v *vault.Vault) error {
	if !v.IsLocked() {
		return nil
	}
	pp, err := readSecretLine("Vault passphrase: ")
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(pp)
	return v.Unlock(ctx, pp)
}

// readSecretLine reads one line from the terminal without echo. It
// refuses non-terminal input: scripts pass --passphrase-file or --stdin.
func readSecretLine(prompt string) ([]byte, error) {
	if !isTerminal(os.Stdin) {
		return nil, &cli.CodedError{Code: cli.ExitError, Err: errors.New(
			"no terminal for the passphrase: pass --passphrase-file")}
	}
	fmt.Fprint(os.Stderr, prompt)
	if err := setEcho(false); err != nil {
		return nil, fmt.Errorf("vault: cannot hide input: %w", err)
	}
	line, err := bufio.NewReader(os.Stdin).ReadBytes('\n')
	_ = setEcho(true)
	fmt.Fprintln(os.Stderr)
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("vault: read passphrase: %w", err)
	}
	out := []byte(strings.TrimRight(string(line), "\r\n"))
	if len(out) == 0 {
		return nil, errors.New("vault: empty passphrase")
	}
	return out, nil
}

// isTerminal reports whether f is a character device (a terminal).
func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// setEcho toggles terminal echo through stty; other platforms without
// stty report an error and callers fall back to --stdin.
func setEcho(on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// readValueStdin reads the whole standard input as one secret value.
func readValueStdin() ([]byte, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("vault: read stdin: %w", err)
	}
	return vault.ReadValueStdin(data)
}

// splitList splits a comma-separated flag into its non-empty items.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// printEntry renders one entry's metadata, never its value.
func printEntry(md vault.Metadata) {
	if globalJSON {
		_ = printJSON(md)
		return
	}
	fmt.Fprintf(os.Stdout, "name:     %s\ntype:     %s\nscopes:   %s\ntools:    %s\ndomains:  %s\nupdated:  %s\n",
		md.Name, md.Type, joinOrDash(md.Scopes), joinOrDash(md.AllowedTools), joinOrDash(md.Domains),
		md.UpdatedAt.Format(time.RFC3339))
}

func joinOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ",")
}

// runVaultSet creates or updates one entry; the value comes from the
// terminal or from --stdin.
func runVaultSet(args []string) error {
	fs := flag.NewFlagSet("vault set", flag.ContinueOnError)
	typ := fs.String("type", vault.TypeToken, "secret type: api_key, provider_credential, website_password, host, ssh_key, token, note")
	url := fs.String("url", "", "URL or host the secret belongs to")
	user := fs.String("user", "", "user name the secret belongs to")
	scopes := fs.String("scopes", "", "comma-separated scopes: general or project ids")
	tools := fs.String("tools", "", "comma-separated tools allowed to use the entry")
	domains := fs.String("domains", "", "comma-separated domains for website passwords")
	stdin := fs.Bool("stdin", false, "read the value from standard input")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault set <name> [--type T] [--stdin]")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	var value []byte
	if *stdin {
		value, err = readValueStdin()
	} else {
		value, err = readSecretLine("Secret value: ")
	}
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(value)
	meta := map[string]string{}
	if *url != "" {
		meta["url"] = *url
	}
	if *user != "" {
		meta["user"] = *user
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	md, err := v.Set(ctx, vault.SetParams{
		Name: rest[0], Type: *typ, Value: value, Meta: meta,
		Scopes: splitList(*scopes), AllowedTools: splitList(*tools), Domains: splitList(*domains),
	})
	if err != nil {
		return err
	}
	printEntry(md)
	return nil
}

// runVaultGet reveals one value on explicit request. It is the only
// vault command that prints a value.
func runVaultGet(args []string) error {
	fs := flag.NewFlagSet("vault get", flag.ContinueOnError)
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault get <name>")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	value, err := v.Reveal(ctx, rest[0])
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(value)
	fmt.Fprintln(os.Stdout, string(value))
	return nil
}

// runVaultList prints metadata of every entry, never values.
func runVaultList(args []string) error {
	fs := flag.NewFlagSet("vault list", flag.ContinueOnError)
	dataDir, passFile := vaultFlags(fs)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	metas, err := v.ListMetadata(ctx)
	if err != nil {
		return err
	}
	if globalJSON {
		return printJSON(map[string]any{"entries": metas})
	}
	for _, md := range metas {
		fmt.Fprintf(os.Stdout, "%-24s %-20s scopes=%s tools=%s\n",
			md.Name, md.Type, joinOrDash(md.Scopes), joinOrDash(md.AllowedTools))
	}
	return nil
}

// runVaultShow prints one entry's metadata, never its value.
func runVaultShow(args []string) error {
	fs := flag.NewFlagSet("vault show", flag.ContinueOnError)
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault show <name>")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	md, err := v.DescribeEntry(ctx, rest[0])
	if err != nil {
		return err
	}
	printEntry(md)
	return nil
}

// runVaultEdit changes scopes, allowed tools or domains. Flags not given
// keep their current values.
func runVaultEdit(args []string) error {
	fs := flag.NewFlagSet("vault edit", flag.ContinueOnError)
	scopes := fs.String("scopes", "", "comma-separated scopes")
	tools := fs.String("tools", "", "comma-separated allowed tools")
	domains := fs.String("domains", "", "comma-separated domains")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault edit <name> [--scopes S] [--tools T] [--domains D]")}
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	var p vault.PolicyParams
	if given["scopes"] {
		s := splitList(*scopes)
		p.Scopes = &s
	}
	if given["tools"] {
		t := splitList(*tools)
		p.AllowedTools = &t
	}
	if given["domains"] {
		d := splitList(*domains)
		p.Domains = &d
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	md, err := v.EditPolicy(ctx, rest[0], p)
	if err != nil {
		return err
	}
	printEntry(md)
	return nil
}

// runVaultRM deletes one entry. Deletion is irreversible, so --yes is
// required.
func runVaultRM(args []string) error {
	fs := flag.NewFlagSet("vault rm", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm the irreversible deletion")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault rm <name> --yes")}
	}
	if !*yes {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("deleting %q is irreversible: pass --yes", rest[0])}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := v.DeleteEntry(ctx, rest[0], true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "deleted %s\n", rest[0])
	return nil
}

// runVaultGenerate stores a random value; the value never prints.
func runVaultGenerate(args []string) error {
	fs := flag.NewFlagSet("vault generate", flag.ContinueOnError)
	typ := fs.String("type", vault.TypeToken, "secret type")
	length := fs.Int("length", 32, "value length")
	charset := fs.String("charset", "", "alphabet (default: letters, digits and symbols)")
	scopes := fs.String("scopes", "", "comma-separated scopes")
	tools := fs.String("tools", "", "comma-separated allowed tools")
	domains := fs.String("domains", "", "comma-separated domains")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault generate <name> [--length N]")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	md, err := v.GenerateEntry(ctx, vault.GenerateParams{
		Name: rest[0], Type: *typ, Length: *length, Charset: *charset,
		Scopes: splitList(*scopes), AllowedTools: splitList(*tools), Domains: splitList(*domains),
	})
	if err != nil {
		return err
	}
	printEntry(md)
	return nil
}

// daemonRunning reports whether a daemon answers on the gate.
func daemonRunning() bool { return cli.DaemonReachable() }

// runVaultLock locks the running daemon's vault. Without a daemon the
// vault is already locked: nothing stays unlocked between commands.
func runVaultLock(args []string) error {
	fs := flag.NewFlagSet("vault lock", flag.ContinueOnError)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	if !daemonRunning() {
		fmt.Fprintln(os.Stdout, "vault locked (no daemon running; nothing stays unlocked between commands)")
		return nil
	}
	raw, err := cli.GateJSON(api.MethodVaultLock, map[string]any{})
	if err != nil {
		return err
	}
	return printGateResult(raw)
}

// runVaultUnlock unlocks the running daemon's vault with the passphrase.
// Without a daemon it checks the passphrase and reports that the unlock
// lasts for this command only.
func runVaultUnlock(args []string) error {
	fs := flag.NewFlagSet("vault unlock", flag.ContinueOnError)
	dataDir, passFile := vaultFlags(fs)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	ctx := context.Background()
	if daemonRunning() {
		pp, err := readSecretLine("Vault passphrase: ")
		if err != nil {
			return err
		}
		defer vault.ZeroBytes(pp)
		raw, err := cli.GateJSON(api.MethodVaultUnlock, map[string]any{"passphrase": string(pp)})
		if err != nil {
			return err
		}
		return printGateResult(raw)
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "vault unlocked for this command (no daemon running; start the daemon to keep it unlocked)")
	return nil
}

// runVaultRotate replaces the data key and rewraps every entry in one
// transaction (Section 20.1). It refuses beside a running daemon.
func runVaultRotate(args []string) error {
	fs := flag.NewFlagSet("vault rotate", flag.ContinueOnError)
	dataDir, passFile := vaultFlags(fs)
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	if daemonRunning() {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New(
			"vault rotate changes the data key the daemon holds: stop the daemon first (`excalibur daemon stop`)")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	var pp []byte
	if v.KEKKind() == "passphrase" {
		if *passFile != "" {
			pp, err = vault.LoadPassphraseFile(*passFile)
		} else {
			pp, err = readSecretLine("Vault passphrase (again): ")
		}
		if err != nil {
			return err
		}
	}
	oldVer, newVer, err := v.RotateKeys(ctx, pp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "data key rotated: version %d -> %d\n", oldVer, newVer)
	return nil
}

// backupPassphrase reads the backup passphrase from --backup-passphrase-file
// or, interactively, twice from the terminal.
func backupPassphrase(file string) ([]byte, error) {
	if file != "" {
		return vault.LoadPassphraseFile(file)
	}
	first, err := readSecretLine("Backup passphrase: ")
	if err != nil {
		return nil, err
	}
	second, err := readSecretLine("Repeat backup passphrase: ")
	if err != nil {
		vault.ZeroBytes(first)
		return nil, err
	}
	defer vault.ZeroBytes(second)
	if string(first) != string(second) {
		vault.ZeroBytes(first)
		return nil, errors.New("vault: backup passphrases differ")
	}
	return first, nil
}

// runVaultExport writes an encrypted backup of every entry.
func runVaultExport(args []string) error {
	fs := flag.NewFlagSet("vault export", flag.ContinueOnError)
	backupFile := fs.String("backup-passphrase-file", "", "file holding the backup passphrase (mode 0600)")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault export <file>")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	pp, err := backupPassphrase(*backupFile)
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(pp)
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	if err := v.ExportToFile(ctx, rest[0], pp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "exported encrypted backup to %s\n", rest[0])
	return nil
}

// runVaultImport merges an encrypted backup into the vault.
func runVaultImport(args []string) error {
	fs := flag.NewFlagSet("vault import", flag.ContinueOnError)
	backupFile := fs.String("backup-passphrase-file", "", "file holding the backup passphrase (mode 0600)")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault import <file>")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	pp, err := backupPassphrase(*backupFile)
	if err != nil {
		return err
	}
	defer vault.ZeroBytes(pp)
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := unlockForUse(ctx, v); err != nil {
		return err
	}
	n, err := v.ImportFromFile(ctx, rest[0], pp)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "imported %d entries\n", n)
	return nil
}

// runVaultUsage prints where a secret was used: run, tool, destination
// and time. It never prints values.
func runVaultUsage(args []string) error {
	fs := flag.NewFlagSet("vault usage", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "most recent uses to show")
	dataDir, passFile := vaultFlags(fs)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return &cli.CodedError{Code: cli.ExitError, Err: errors.New("usage: excalibur vault usage <name> [--limit N]")}
	}
	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		return err
	}
	ctx := context.Background()
	v, err := openVaultLocal(ctx, dir, *passFile)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	uses, err := v.UsageOf(ctx, rest[0], *limit)
	if err != nil {
		return err
	}
	if globalJSON {
		return printJSON(map[string]any{"uses": uses})
	}
	for _, u := range uses {
		fmt.Fprintf(os.Stdout, "%s  %-12s dest=%-8s run=%s\n", u.At.Format(time.RFC3339), u.Tool, u.Dest, u.RunID)
	}
	return nil
}
