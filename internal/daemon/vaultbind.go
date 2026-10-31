// Vault bindings for providers and tools (Section 20.1, H10 hook).
//
// The model only ever sees references (secret://name) and metadata.
// This file is the single place where the daemon connects the vault
// to the two consumers:
//
//   - providers (19.2): the gateway resolves provider_credential at
//     call time through ProviderHook. The vault wins over environment
//     and explicit config; the eval profile never opens the vault
//     (23.2); delegate tasks only resolve entries scoped to their
//     project (3.6).
//   - tools (13.4 step 5): the executor resolves references just
//     before running inside the tool process (env, header, form,
//     file) and passes every output through ToolsRedactor before it
//     reaches the durable journal, context, telemetry, ATIF or blobs.
//
// The Backend interface keeps the daemon independent of the concrete
// vault store: production passes vaultAdapter over *vault.Vault,
// tests pass fakes.
package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// SecretBackend is the narrow vault surface the daemon binds to
// providers and tools.
type SecretBackend interface {
	// ResolveSecret decrypts one reference for a tool or provider
	// call after checking scopes, allowed tools, domains, lock and
	// profile gates. The value must be zeroed after use.
	ResolveSecret(ctx context.Context, req SecretCall) ([]byte, func(), error)
	// RedactBook returns the output redactor: literal values plus
	// their base64 and URL encodings, plus known key patterns.
	RedactBook() func(string) string
}

// SecretCall is one in-process resolution.
type SecretCall struct {
	// Ref is "secret://name" or a bare name.
	Ref string
	// Tool is the resolving consumer ("provider", "exec",
	// "browser", ...).
	Tool string
	// RunID identifies the run; recorded in secret_uses (20.1).
	RunID string
	// Project scopes delegate tasks (3.6).
	Project string
	// Delegated marks profile-delegate tasks: only entries scoped
	// to Project resolve.
	Delegated bool
	// Eval marks the eval profile: the vault never opens (23.2).
	Eval bool
	// Domain is the page domain for form fills.
	Domain string
	// Dest is one of env, header, form, file, provider (20.1).
	Dest string
}

// VaultAdapter binds *vault.Vault to SecretBackend without leaking
// vault types across the daemon seam.
type VaultAdapter struct {
	V *vault.Vault
}

// ResolveSecret implements SecretBackend.
func (a VaultAdapter) ResolveSecret(ctx context.Context, c SecretCall) ([]byte, func(), error) {
	res, err := a.V.Resolve(ctx, vault.ResolveRequest{
		Ref:       c.Ref,
		Tool:      c.Tool,
		RunID:     c.RunID,
		Project:   c.Project,
		Delegated: c.Delegated,
		Eval:      c.Eval,
		Domain:    c.Domain,
		Dest:      vault.DestKind(c.Dest),
	})
	if err != nil {
		return nil, nil, err
	}
	live := res.Value
	return live, func() { vault.ZeroBytes(live) }, nil
}

// RedactBook implements SecretBackend.
func (a VaultAdapter) RedactBook() func(string) string {
	return a.V.Redact
}

// ProviderHook returns the registry-shaped resolver for one run: the
// gateway calls it at provider-call time so vault values never sit in
// config or the journal. A nil backend resolves nothing (eval and
// vault-less setups fall through to environment and config).
func ProviderHook(be SecretBackend, runID string) func(ctx context.Context, name string) (string, error) {
	return func(ctx context.Context, name string) (string, error) {
		if be == nil {
			return "", fmt.Errorf("daemon: no vault bound for provider credential %q", name)
		}
		val, cleanup, err := be.ResolveSecret(ctx, SecretCall{
			Ref: name, Tool: "provider", RunID: runID, Dest: "provider",
		})
		if err != nil {
			return "", err
		}
		defer cleanup()
		return string(val), nil
	}
}

// ToolsRedactor returns the 13.4 step-5 filter for the executor: every
// tool output passes through it before the journal, context,
// telemetry, ATIF or blobs. A nil backend redacts nothing.
func ToolsRedactor(be SecretBackend) func(string) string {
	if be == nil {
		return func(s string) string { return s }
	}
	redact := be.RedactBook()
	if redact == nil {
		return func(s string) string { return s }
	}
	return redact
}

// isSecretRef reports secret:// references (bare names resolve too,
// but env maps only carry the explicit form).
func isSecretRef(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "secret://") ||
		strings.HasPrefix(strings.TrimSpace(s), "secret:")
}

// ResolveEnv resolves secret:// values inside an environment map for
// the exec destination (20.1), returning the resolved map. Values are
// zeroed by the returned cleanup after the child exits.
func ResolveEnv(ctx context.Context, be SecretBackend, env map[string]string, tool, runID, project string) (map[string]string, func(), error) {
	out := make(map[string]string, len(env))
	var live [][]byte
	cleanup := func() {
		for _, b := range live {
			for i := range b {
				b[i] = 0
			}
		}
	}
	for k, v := range env {
		if !isSecretRef(v) {
			out[k] = v
			continue
		}
		if be == nil {
			cleanup()
			return nil, nil, fmt.Errorf("daemon: secret %q needs a vault", v)
		}
		val, done, err := be.ResolveSecret(ctx, SecretCall{
			Ref: v, Tool: tool, RunID: runID, Project: project, Dest: "env",
		})
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		defer done()
		live = append(live, val)
		out[k] = string(val)
	}
	return out, cleanup, nil
}
