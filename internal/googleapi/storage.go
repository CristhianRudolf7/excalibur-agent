package googleapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

func (p *OAuth) path() string   { return filepath.Join(p.opts.ConfigDir, "google-connection.json") }
func (p *OAuth) marker() string { return filepath.Join(p.opts.ConfigDir, "google-connection-imported") }
func (p *OAuth) load(ctx context.Context) error {
	// A fallback file is newer than the vault: it may contain a disconnect
	// tombstone written while the vault was locked. Always honor it first.
	raw, err := os.ReadFile(p.path())
	if err == nil {
		defer vault.ZeroBytes(raw)
		if json.Unmarshal(raw, &p.token) != nil {
			return errStorage
		}
		p.loaded = true
		p.warning = "file_storage"
		if p.opts.Vault != nil && !p.opts.Vault.IsLocked() {
			return p.persist(ctx)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errStorage
	}
	if v := p.opts.Vault; v != nil {
		if v.IsLocked() {
			if _, err := v.Describe(ctx, vaultName); err == nil {
				p.warning = "vault_locked"
				return nil
			} else if !errors.Is(err, vault.ErrNotFound) {
				return errStorage
			}
		} else {
			resolved, err := v.Resolve(ctx, vault.ResolveRequest{Ref: vaultName, Tool: "googleapi", Dest: vault.DestProvider})
			if err == nil {
				defer vault.ZeroBytes(resolved.Value)
				if json.Unmarshal(resolved.Value, &p.token) != nil {
					return errStorage
				}
				p.loaded = true
				p.warning = ""
				return nil
			}
			if !errors.Is(err, vault.ErrNotFound) {
				return errStorage
			}
		}
	}
	p.loaded = true
	if _, err := os.Stat(p.marker()); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return errStorage
	}
	raw, err = os.ReadFile(p.opts.ImportPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errStorage
	}
	defer vault.ZeroBytes(raw)
	var imported struct {
		tokenResponse
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(raw, &imported) != nil || imported.RefreshToken == "" {
		return errors.New("googleapi: invalid import token file")
	}
	info, err := os.Stat(p.opts.ImportPath)
	if err != nil {
		return errStorage
	}
	issued := info.ModTime()
	// This is a trusted local import, not an unverified browser identity.
	// iat preserves the real issue date instead of extending the testing lifetime.
	var claims struct {
		Email  string `json:"email"`
		Issued int64  `json:"iat"`
	}
	if parts := strings.Split(imported.IDToken, "."); len(parts) == 3 {
		if body, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			_ = json.Unmarshal(body, &claims)
			vault.ZeroBytes(body)
		}
	}
	if claims.Issued > 0 {
		issued = time.Unix(claims.Issued, 0)
	}
	p.token = tokenRecord{Email: claims.Email, Scopes: strings.Fields(imported.Scope), RefreshToken: imported.RefreshToken, RefreshIssued: issued, AccessToken: imported.AccessToken, AccessExpiry: issued.Add(time.Duration(imported.ExpiresIn) * time.Second)}
	if p.ttl > 0 {
		p.token.RefreshExpiry = issued.Add(p.ttl)
	}
	if err := p.persist(ctx); err != nil {
		return err
	}
	return nil
}

func (p *OAuth) persist(ctx context.Context) error {
	raw, err := json.Marshal(p.token)
	if err != nil {
		return errStorage
	}
	defer vault.ZeroBytes(raw)
	// The marker has no credentials and prevents re-import after disconnection.
	if err := atomicPrivateFile(p.marker(), []byte("imported\n")); err != nil {
		return errStorage
	}
	if v := p.opts.Vault; v != nil && !v.IsLocked() {
		if p.token.AccessToken == "" && p.token.RefreshToken == "" {
			if err := v.Delete(ctx, vaultName); err != nil && !errors.Is(err, vault.ErrNotFound) {
				return errStorage
			}
			// Retain the reason across restarts, but no token values.
			if err := atomicPrivateFile(p.path(), raw); err != nil {
				return errStorage
			}
		} else {
			_, err := v.Set(ctx, vault.SetParams{Name: vaultName, Type: vault.TypeToken, Value: raw, Scopes: []string{vault.ScopeGeneral}, AllowedTools: []string{"googleapi"}})
			if err != nil {
				return errStorage
			}
			if err := os.Remove(p.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
				return errStorage
			}
		}
		p.warning = ""
		return nil
	}
	if err := atomicPrivateFile(p.path(), raw); err != nil {
		return errStorage
	}
	p.warning = "file_storage"
	return nil
}
func atomicPrivateFile(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".google-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
