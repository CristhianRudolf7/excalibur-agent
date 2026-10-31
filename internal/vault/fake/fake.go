// Package fake provides an in-memory vault stand-in for tests of
// vault clients (tools executor, provider gateway, intake). It never
// touches disk and stores values in memory only.
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Vault is a minimal in-memory fake of the secrets vault: references,
// policy checks and redaction markers without encryption.
type Vault struct {
	mu      sync.Mutex
	values  map[string]string
	meta    map[string]map[string]string
	scopes  map[string][]string
	tools   map[string][]string
	domains map[string][]string
	locked  bool
	uses    map[string]int
}

// New returns an empty unlocked fake vault.
func New() *Vault {
	return &Vault{
		values:  map[string]string{},
		meta:    map[string]map[string]string{},
		scopes:  map[string][]string{},
		tools:   map[string][]string{},
		domains: map[string][]string{},
		uses:    map[string]int{},
	}
}

// Set stores a value.
func (v *Vault) Set(name, value string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.values[name] = value
}

// SetPolicy stores scopes, allowed tools and domains for name.
func (v *Vault) SetPolicy(name string, scopes, tools, domains []string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.scopes[name] = scopes
	v.tools[name] = tools
	v.domains[name] = domains
}

// SetLocked controls the locked state.
func (v *Vault) SetLocked(locked bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.locked = locked
}

// Resolve returns the value for ref ("secret://name" or bare name),
// enforcing the fake policy. It mirrors vault.Resolve errors in kind.
func (v *Vault) Resolve(_ context.Context, ref, tool, project, domain string) (string, error) {
	name := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(ref, "secret://"), "secret:"))
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.locked {
		return "", fmt.Errorf("vault: vault is locked")
	}
	val, ok := v.values[name]
	if !ok {
		return "", fmt.Errorf("vault: secret not found")
	}
	if scopes, restricted := v.scopes[name]; restricted && len(scopes) > 0 {
		allowed := false
		for _, s := range scopes {
			if s == "general" || s == project {
				allowed = true
			}
		}
		if project == "" && len(scopes) == 1 && scopes[0] == "general" {
			allowed = true
		}
		if !allowed {
			return "", fmt.Errorf("vault: secret %q is not scoped to project %q", name, project)
		}
	}
	if tools, ok := v.tools[name]; ok && len(tools) > 0 && tool != "" {
		allowed := false
		for _, t := range tools {
			if strings.EqualFold(t, tool) || t == "*" {
				allowed = true
			}
		}
		if !allowed {
			return "", fmt.Errorf("vault: secret %q is not allowed for %q", name, tool)
		}
	}
	if domains, ok := v.domains[name]; ok && len(domains) > 0 && domain != "" {
		allowed := false
		for _, d := range domains {
			if d == domain {
				allowed = true
			}
		}
		if !allowed {
			return "", fmt.Errorf("vault: secret %q is not allowed for domain %q", name, domain)
		}
	}
	v.uses[name]++
	return val, nil
}

// Uses reports how often name resolved.
func (v *Vault) Uses(name string) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.uses[name]
}

// Redact replaces stored values with ‹secret:name› markers.
func (v *Vault) Redact(s string) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	for name, val := range v.values {
		if val != "" {
			s = strings.ReplaceAll(s, val, "‹secret:"+name+"›")
		}
	}
	return s
}
