package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// ResolveRequest is one in-process secret resolution. The executor
// (Section 13.4) builds it just before running a tool; the model only
// ever sees the secret:// reference and metadata.
type ResolveRequest struct {
	// Ref is the secret reference ("secret://name" or bare "name").
	Ref string
	// Tool is the resolving tool ("exec", "browser", ...).
	Tool string
	// RunID identifies the execution asking (recorded in secret_uses).
	RunID string
	// Project scopes delegate tasks (Section 3.6): only entries whose
	// scopes include Project (or general entries, when not delegated)
	// resolve.
	Project string
	// Delegated marks profile-delegate tasks: general-scope entries do
	// NOT resolve, only entries scoped to Project.
	Delegated bool
	// Eval marks the eval profile: the vault never opens (23.2).
	Eval bool
	// Domain is the page domain for DestForm checks.
	Domain string
	// Dest is where the value is injected.
	Dest DestKind
}

// ResolveResult carries the decrypted value. Callers must Zero the
// value after injection; values are []byte and cleared after use.
type ResolveResult struct {
	Name  string
	Value []byte
}

// SecretResolver resolves a vault name to its value. It is the hook
// the H10 vault connects to the provider gateway: assign
// vault.AsSecretResolver to registry.SecretResolver-shaped fields.
// The signatures match structurally; adapt with SecretResolverFunc.
type SecretResolver func(ctx context.Context, name string) (string, error)

// SecretResolverFunc adapts a Vault to provider-gateway SecretResolver
// fields (vault priority over env and config, Section 20.1).
func (v *Vault) SecretResolverFunc(runID string) SecretResolver {
	return func(ctx context.Context, name string) (string, error) {
		res, err := v.Resolve(ctx, ResolveRequest{
			Ref:   name,
			Tool:  "provider",
			RunID: runID,
			Dest:  DestProvider,
		})
		if err != nil {
			return "", err
		}
		defer ZeroBytes(res.Value)
		return string(res.Value), nil
	}
}

// ParseRef splits "secret://name" (or a bare name) into the entry name.
func ParseRef(ref string) (string, error) {
	name := strings.TrimSpace(ref)
	name = strings.TrimPrefix(name, "secret://")
	name = strings.TrimPrefix(name, "secret:")
	if err := validateName(name); err != nil {
		return "", err
	}
	return name, nil
}

// Resolve decrypts one entry inside the tool process after checking
// expiry, scopes, allowed tools and (for forms) domains. It records
// the use (never the value) in secret_uses. On a locked vault it
// returns the unlock question instead of failing.
func (v *Vault) Resolve(ctx context.Context, req ResolveRequest) (ResolveResult, error) {
	if req.Eval {
		return ResolveResult{}, ErrEvalBlocked
	}
	name, err := ParseRef(req.Ref)
	if err != nil {
		return ResolveResult{}, err
	}
	v.mu.Lock()
	if v.dek == nil {
		v.mu.Unlock()
		return ResolveResult{}, needsUnlock(name)
	}
	dek := *v.dek
	v.mu.Unlock()

	var md Metadata
	var id string
	var entryVer int
	var nonce, sealed []byte
	err = v.db.Read(ctx, func(ctx context.Context, q store.Queryer) error {
		rows, rerr := q.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,created_at,updated_at,last_used_at,expires_at,entry_version,dek_version,ciphertext,nonce FROM secrets WHERE name=?`, name)
		if rerr != nil {
			return rerr
		}
		defer func() { _ = rows.Close() }()
		if !rows.Next() {
			return ErrNotFound
		}
		var metaJSON, scopesJSON, toolsJSON, domainsJSON string
		var created, updated string
		var lastUsed, expires *string
		var ev, dv int
		var ct, n []byte
		var rowID, rowName, rowType string
		if rerr := rows.Scan(&rowID, &rowName, &rowType, &metaJSON, &scopesJSON, &toolsJSON, &domainsJSON, &created, &updated, &lastUsed, &expires, &ev, &dv, &ct, &n); rerr != nil {
			return rerr
		}
		if rerr := rows.Err(); rerr != nil {
			return rerr
		}
		md, rerr = metadataFromParts(rowID, rowName, rowType, metaJSON, scopesJSON, toolsJSON, domainsJSON, created, updated, lastUsed, expires, ev, dv)
		if rerr != nil {
			return rerr
		}
		if dv != v.currentDEKVersion() {
			// Entry sealed with an older DEK: resolve that version's
			// key row and unwrap. Rotation keeps old rows until all
			// entries migrate, so mixed versions decrypt.
			krow, kerr := v.readKeysRowQ(ctx, q, dv)
			if kerr != nil {
				return kerr
			}
			kek, kerr := v.kekForDecrypt(ctx, krow)
			if kerr != nil {
				return kerr
			}
			dk, kerr := unwrapDEK(kek, dv, krow.wrapNonce, krow.wrapped)
			zero(kek)
			if kerr != nil {
				return kerr
			}
			dek = dk
			zero(dk[:])
		}
		id, entryVer, nonce, sealed = rowID, ev, n, ct
		return nil
	})
	if err != nil {
		return ResolveResult{}, err
	}
	if md.ExpiresAt != nil && time.Now().After(*md.ExpiresAt) {
		return ResolveResult{}, fmt.Errorf("%w: %q expired at %s", ErrDenied, name, md.ExpiresAt.Format(time.RFC3339))
	}
	if err := checkPolicy(md, req); err != nil {
		return ResolveResult{}, err
	}
	plain, err := openEntry(dek, id, name, md.Type, entryVer, nonce, sealed)
	if err != nil {
		return ResolveResult{}, err
	}
	v.recordUse(ctx, name, req)
	v.Touch()
	return ResolveResult{Name: name, Value: plain}, nil
}

// metadataFromParts assembles Metadata from raw column strings.
func metadataFromParts(id, name, typ, metaJSON, scopesJSON, toolsJSON, domainsJSON, created, updated string, lastUsed, expires *string, ev, dv int) (Metadata, error) {
	var md Metadata
	md.ID = id
	md.Name = name
	md.Type = typ
	md.Meta = map[string]string{}
	_ = json.Unmarshal([]byte(metaJSON), &md.Meta)
	_ = json.Unmarshal([]byte(scopesJSON), &md.Scopes)
	_ = json.Unmarshal([]byte(toolsJSON), &md.AllowedTools)
	_ = json.Unmarshal([]byte(domainsJSON), &md.Domains)
	var err error
	md.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return Metadata{}, err
	}
	md.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	if err != nil {
		return Metadata{}, err
	}
	if lastUsed != nil {
		if t, terr := time.Parse(time.RFC3339Nano, *lastUsed); terr == nil {
			md.LastUsedAt = &t
		}
	}
	if expires != nil {
		if t, terr := time.Parse(time.RFC3339Nano, *expires); terr == nil {
			md.ExpiresAt = &t
		}
	}
	md.EntryVersion = ev
	md.DEKVersion = dv
	return md, nil
}
func (v *Vault) kekForDecrypt(_ context.Context, krow keysRow) ([]byte, error) {
	switch krow.kekKind {
	case kekSecretService:
		if v.opts.Keyring == nil {
			return nil, fmt.Errorf("%w: no keyring configured", ErrNoKey)
		}
		stored, err := v.opts.Keyring.Get(v.opts.KeyringService, v.opts.KeyringUser)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoKey, err)
		}
		return stored, nil
	default:
		return nil, fmt.Errorf("%w: passphrase needed to decrypt dek v%d", ErrNoKey, krow.dekVersion)
	}
}

// checkPolicy enforces scopes, allowed_tools and form domains.
func checkPolicy(md Metadata, req ResolveRequest) error {
	if req.Delegated {
		// Delegate tasks resolve only entries scoped to their project
		// (Section 20.1); general entries stay hidden.
		allowed := false
		for _, s := range md.Scopes {
			if s == req.Project && req.Project != "" {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: secret %q is not scoped to project %q", ErrDenied, md.Name, req.Project)
		}
	} else if req.Project != "" {
		allowed := false
		for _, s := range md.Scopes {
			if s == ScopeGeneral || s == req.Project {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: secret %q is not scoped to project %q", ErrDenied, md.Name, req.Project)
		}
	}
	if len(md.AllowedTools) > 0 && req.Tool != "" {
		allowed := false
		for _, t := range md.AllowedTools {
			if strings.EqualFold(t, req.Tool) || t == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: secret %q is not allowed for %q", ErrDenied, md.Name, req.Tool)
		}
	}
	if req.Dest == DestForm && len(md.Domains) > 0 {
		domain := normalizeDomains([]string{req.Domain})
		if len(domain) == 0 {
			return fmt.Errorf("%w: secret %q needs a page domain for form fill", ErrDenied, md.Name)
		}
		allowed := false
		for _, d := range md.Domains {
			if d == domain[0] || (strings.HasPrefix(d, "*.") && strings.HasSuffix(domain[0], d[1:])) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: secret %q is not allowed for domain %q", ErrDenied, md.Name, req.Domain)
		}
	}
	return nil
}

// recordUse appends a secret_uses row (tool, run, dest, project, time;
// never the value). Failures are best-effort: use tracking must not
// break tool execution.
func (v *Vault) recordUse(ctx context.Context, name string, req ResolveRequest) {
	tool := req.Tool
	if tool == "" {
		tool = "unknown"
	}
	_ = v.db.Write(ctx, store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO secret_uses(secret_name,tool,run_id,dest,project,at) VALUES(?,?,?,?,?,?)`,
			name, tool, req.RunID, string(req.Dest), req.Project, utcNow())
		return err
	})
	// Update last_used_at on the entry.
	_ = v.db.Write(ctx, store.PriorityBackground, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `UPDATE secrets SET last_used_at=? WHERE name=?`, utcNow(), name)
		return err
	})
}
