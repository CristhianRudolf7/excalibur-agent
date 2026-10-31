package vault

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// InjectEnv resolves secret:// references in an environment map inside
// the tool process and returns a scrubbed cleanup. Values are []byte
// while in use and zeroed by Cleanup. The executor calls Cleanup after
// the child exits (Section 20.1: env destination).
func (v *Vault) InjectEnv(ctx context.Context, env map[string]string, tool, runID, project string) (resolved map[string]string, cleanup func(), err error) {
	out := make(map[string]string, len(env))
	var live [][]byte
	cleanup = func() {
		for _, b := range live {
			zero(b)
		}
	}
	for k, val := range env {
		if !isSecretRef(val) {
			out[k] = val
			continue
		}
		res, rerr := v.Resolve(ctx, ResolveRequest{Ref: val, Tool: tool, RunID: runID, Project: project, Dest: DestEnv})
		if rerr != nil {
			cleanup()
			return nil, nil, rerr
		}
		live = append(live, res.Value)
		out[k] = string(res.Value)
	}
	return out, cleanup, nil
}

// InjectHeader resolves one secret:// reference for an HTTP header
// value (Section 20.1: header destination). The value is returned as a
// string for the header setter; callers must not log it.
func (v *Vault) InjectHeader(ctx context.Context, ref, tool, runID, project string) (string, func(), error) {
	res, err := v.Resolve(ctx, ResolveRequest{Ref: ref, Tool: tool, RunID: runID, Project: project, Dest: DestHeader})
	if err != nil {
		return "", nil, err
	}
	return string(res.Value), func() { ZeroBytes(res.Value) }, nil
}

// CheckFormPolicy checks scopes, allowed tools and the entry's domains
// against the page domain before the browser fills a credential field
// (Section 20.1: form destination). It resolves the value only when the
// policy passes; the fill operation consumes it without logging.
func (v *Vault) CheckFormPolicy(ctx context.Context, ref, tool, runID, project, pageDomain string) (ResolveResult, error) {
	return v.Resolve(ctx, ResolveRequest{Ref: ref, Tool: tool, RunID: runID, Project: project, Domain: pageDomain, Dest: DestForm})
}

// InjectFile writes a secret (SSH keys, certificates) to a temp file
// in runtimeDir (memory, 0600) and returns its path. RemoveTempFile
// deletes it when the tool finishes (Section 20.1: file destination).
func (v *Vault) InjectFile(ctx context.Context, ref, tool, runID, project, runtimeDir string) (path string, cleanup func(), err error) {
	res, err := v.Resolve(ctx, ResolveRequest{Ref: ref, Tool: tool, RunID: runID, Project: project, Dest: DestFile})
	if err != nil {
		return "", nil, err
	}
	defer ZeroBytes(res.Value)
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return "", nil, err
	}
	// 0600 temp file, created exclusively.
	f, err := os.OpenFile(filepath.Join(runtimeDir, "excalibur-secret-*"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		// Fall back to a unique name under the runtime dir.
		f, err = os.CreateTemp(runtimeDir, "excalibur-secret-*")
		if err != nil {
			return "", nil, err
		}
		_ = f.Chmod(0o600)
	}
	name := f.Name()
	if _, err := f.Write(res.Value); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", nil, err
	}
	cleanup = func() { _ = os.Remove(name) }
	return name, cleanup, nil
}

// SetAuthHeader is a helper for HTTP capability adapters: it resolves
// ref and sets the header on req without logging the value.
func (v *Vault) SetAuthHeader(ctx context.Context, req *http.Request, header, ref, tool, runID, project string) (func(), error) {
	val, cleanup, err := v.InjectHeader(ctx, ref, tool, runID, project)
	if err != nil {
		return nil, err
	}
	req.Header.Set(header, val)
	// The header value now lives in the request; the resolved copy is
	// cleared. Callers must not log req with this header.
	ZeroBytes([]byte(val))
	return cleanup, nil
}

// isSecretRef reports whether s is a secret:// reference.
func isSecretRef(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "secret://") || (strings.HasPrefix(t, "secret:") && len(t) > len("secret:"))
}

// ScrubArgs replaces secret:// references in tool arguments with their
// markers for logging: the journal and telemetry record markers, the
// executor resolves the real values separately.
func ScrubArgs(args map[string]string) map[string]string {
	out := make(map[string]string, len(args))
	for k, val := range args {
		if isSecretRef(val) {
			name, err := ParseRef(val)
			if err != nil {
				out[k] = RedactPatternMarker
				continue
			}
			out[k] = RedactSecretPrefix + name + "›"
			continue
		}
		out[k] = val
	}
	return out
}

// ResolveError assists the executor: locked vaults surface an
// unlock-vault question (waiting-on-user), policy denials surface a
// clear denial the agent may relay.
func ResolveError(err error, name string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("vault: cannot inject %q: %w", name, err)
}
