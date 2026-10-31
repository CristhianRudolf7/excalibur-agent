package wireengine

import (
	"context"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers/registry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// VaultResolver adapts a *vault.Vault to the provider registry
// SecretResolver hook (Sections 19.2, 20.1): the vault has priority,
// then the environment, then the explicit config. runID scopes the
// secret_uses record. A nil vault returns nil so callers fall back to
// environment and config; the eval profile never passes a vault
// (Section 23.2: keys arrive by environment).
func VaultResolver(v *vault.Vault, runID string) registry.SecretResolver {
	if v == nil {
		return nil
	}
	inner := v.SecretResolverFunc(runID)
	return registry.SecretResolver(func(ctx context.Context, name string) (string, error) {
		return inner(ctx, name)
	})
}

// VaultRedactor returns the executor redaction hook over the vault
// (Section 20.1 step 5): every tool output passes through it before
// truncation, the journal, the context, telemetry, ATIF and blobs.
// A nil vault still redacts known key patterns.
func VaultRedactor(v *vault.Vault) tools.Redactor {
	if v == nil {
		r := vault.NewRedactor()
		return r.Redact
	}
	return v.Redact
}

// ExecSecretResolver returns the per-tool secret resolver for exec env
// entries (Section 20.1: env destination). It resolves secret://
// references with the exec tool identity and records use. A nil vault
// returns nil so env passes through unchanged.
func ExecSecretResolver(v *vault.Vault, runID, project string, delegated, eval bool) func(context.Context, string) (string, error) {
	if v == nil {
		return nil
	}
	return func(ctx context.Context, ref string) (string, error) {
		res, err := v.Resolve(ctx, vault.ResolveRequest{
			Ref: ref, Tool: "exec", RunID: runID, Project: project,
			Delegated: delegated, Eval: eval, Dest: vault.DestEnv,
		})
		if err != nil {
			return "", vault.ResolveError(err, ref)
		}
		defer vault.ZeroBytes(res.Value)
		return string(res.Value), nil
	}
}
