package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

const credentialLifetime = time.Hour

type ephemeralCredential struct {
	run     string
	expires time.Time
}

func credentialHostName(host string) string {
	sum := sha256.Sum256([]byte(host))
	return "credentials-" + hex.EncodeToString(sum[:12])
}

// rememberedLocked reads policy and field metadata only. Reusing a reference
// never decrypts a value into the model or extends an ephemeral entry's lifetime.
func (r *CredentialRegistry) rememberedLocked(ctx context.Context, a credentialArgs) map[string]string {
	if r.vault == nil || r.vault.IsLocked() {
		return nil
	}
	for _, f := range a.Fields {
		if f == "otp" {
			return nil
		}
	}
	entries, err := r.vault.ListMetadata(ctx)
	if err != nil {
		return nil
	}
	bundles := map[string]map[string]string{}
	for _, md := range entries {
		base := md.Meta["credential_bundle"]
		if base == "" || md.Meta["credential_site"] != a.Site || md.Meta["credential_remembered"] != "true" || md.Meta["credential_field"] == "otp" || md.ExpiresAt != nil || len(md.Domains) != 1 || md.Domains[0] != a.Site || len(md.AllowedTools) != 1 || md.AllowedTools[0] != "browser_act" || len(md.Scopes) != 1 || md.Scopes[0] != vault.ScopeGeneral {
			continue
		}
		field := md.Meta["credential_field"]
		if !credentialFields[field] || md.Name != base+"-"+field {
			continue
		}
		if bundles[base] == nil {
			bundles[base] = map[string]string{}
		}
		bundles[base][field] = "secret://" + base + "." + field
	}
	// Bundle IDs contain a random request suffix. Any complete matching bundle
	// is usable; fields from distinct submissions are never mixed.
	for _, refs := range bundles {
		selected := map[string]string{}
		for _, f := range a.Fields {
			if refs[f] == "" {
				break
			}
			selected[f] = refs[f]
		}
		if len(selected) == len(a.Fields) {
			return selected
		}
	}
	return nil
}

// Submit receives values from a pending secure form. Values go directly into the
// vault and are zeroed even on validation, lock, cancellation or storage errors.
func (r *CredentialRegistry) Submit(ctx context.Context, id string, values map[string][]byte, remember bool) error {
	defer clearCredentialValues(values)
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.pending[id]
	if !ok {
		return &api.Error{Code: api.CodeUnknownTarget, Message: "credential request is no longer pending"}
	}
	if !time.Now().Before(w.deadline) {
		r.resolveLocked(w, "timeout", nil)
		return &api.Error{Code: api.CodeConflict, Message: "credential request timed out"}
	}
	if len(values) != len(w.request.Fields) {
		return &api.Error{Code: api.CodeInvalidParams, Message: "values must match the requested fields"}
	}
	for _, f := range w.request.Fields {
		if len(values[f]) == 0 || len(values[f]) > 16384 {
			return &api.Error{Code: api.CodeInvalidParams, Message: "requested values must contain between 1 and 16384 bytes"}
		}
	}
	if r.vault == nil {
		return &api.Error{Code: api.CodeInternal, Message: "vault unavailable"}
	}
	if r.vault.IsLocked() {
		return credentialVaultError(vault.ErrLocked)
	}
	ephemeralCount := 0
	for _, f := range w.request.Fields {
		if !remember || f == "otp" {
			ephemeralCount++
		}
	}
	if len(r.ephemeral)+ephemeralCount > 512 {
		return &api.Error{Code: api.CodeConflict, Message: "ephemeral credential capacity unavailable"}
	}
	sum := sha256.Sum256([]byte(id))
	base := credentialHostName(w.request.Site) + "-" + hex.EncodeToString(sum[:6])
	expires := time.Now().UTC().Add(credentialLifetime)
	refs, err := r.storeBundleLocked(ctx, base, w.request.Site, w.run, values, remember, nil, expires)
	if err != nil {
		return err
	}
	for _, f := range w.request.Fields {
		if !remember || f == "otp" {
			r.ephemeral[base+"-"+f] = ephemeralCredential{run: w.run, expires: expires}
		}
	}
	if ephemeralCount > 0 {
		r.startCleanerLocked()
	}
	r.resolveLocked(w, "provided", refs)
	return nil
}

func clearCredentialValues(values map[string][]byte) {
	for field, value := range values {
		vault.ZeroBytes(value)
		delete(values, field)
	}
}

// storeBundleLocked creates a complete bundle or removes its partial entries.
// Callers hold r.mu and erase their owned values on every exit path.
func (r *CredentialRegistry) storeBundleLocked(ctx context.Context, base, site, run string, values map[string][]byte, remember bool, extra map[string]string, expires time.Time) (map[string]string, error) {
	fields := make([]string, 0, len(values))
	for field := range values {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	refs := map[string]string{}
	created := []string{}
	rollback := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, name := range created {
			if err := r.vault.Delete(cleanup, name); err != nil && !errors.Is(err, vault.ErrNotFound) {
				r.ephemeral[name] = ephemeralCredential{run: run, expires: time.Now()}
				r.startCleanerLocked()
			}
		}
	}
	for _, f := range fields {
		persistent := remember && f != "otp"
		meta := map[string]string{"credential_bundle": base, "credential_site": site, "credential_field": f, "credential_remembered": "false", "credential_run": run}
		var expiry *time.Time
		if persistent {
			meta["credential_remembered"] = "true"
			delete(meta, "credential_run")
		} else {
			expiry = &expires
		}
		for key, value := range extra {
			meta[key] = value
		}
		name := base + "-" + f
		_, err := r.vault.Create(ctx, vault.CreateParams{Name: name, Type: vault.TypeWebsitePassword, Value: values[f], Metadata: meta, AllowedTools: []string{"browser_act"}, Domains: []string{site}, ExpiresAt: expiry})
		if err != nil {
			rollback()
			return nil, credentialVaultError(err)
		}
		created = append(created, name)
		refs[f] = "secret://" + base + "." + f
	}
	return refs, nil
}

func credentialVaultError(err error) error {
	if errors.Is(err, vault.ErrLocked) {
		return &api.Error{Code: api.CodeAppError, Message: "vault_locked", Data: map[string]string{"code": "vault_locked"}}
	}
	return &api.Error{Code: api.CodeInternal, Message: "credential vault storage failed"}
}

func (r *CredentialRegistry) startCleanerLocked() {
	if r.cleaning || r.closed {
		return
	}
	r.cleaning = true
	safego.Go("credentials-expiry", nil, func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				r.cleanup("", now)
			case <-r.stop:
				return
			}
		}
	})
}

func (r *CredentialRegistry) cleanup(run string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(run, now)
}

func (r *CredentialRegistry) cleanupLocked(run string, now time.Time) {
	if r.vault == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for name, entry := range r.ephemeral {
		if run != "" && entry.run != run || run == "" && now.Before(entry.expires) {
			continue
		}
		if err := r.vault.Delete(ctx, name); err == nil || errors.Is(err, vault.ErrNotFound) {
			delete(r.ephemeral, name)
		}
	}
}

// recoverEphemeral removes encrypted values left by interrupted runs, even if
// the vault is locked. Remembered values are never selected for this cleanup.
func (r *CredentialRegistry) recoverEphemeral(ctx context.Context) error {
	if r.vault == nil {
		return nil
	}
	entries, err := r.vault.ListMetadata(ctx)
	if err != nil {
		return err
	}
	for _, md := range entries {
		if md.Meta["credential_run"] != "" && md.Meta["credential_remembered"] == "false" && strings.HasPrefix(md.Name, "credentials-") {
			if err := r.vault.Delete(ctx, md.Name); err != nil && !errors.Is(err, vault.ErrNotFound) {
				return errors.New("ephemeral credential recovery failed")
			}
		}
	}
	return nil
}
