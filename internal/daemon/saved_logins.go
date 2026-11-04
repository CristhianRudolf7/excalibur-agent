package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

// SaveLogin stores a complete remembered login without exposing its values to
// conversation intake. All owned value buffers are erased, including on errors.
func (r *CredentialRegistry) SaveLogin(ctx context.Context, site, label, startURL string, values map[string][]byte) error {
	defer clearCredentialValues(values)
	invalid := &api.Error{Code: api.CodeInvalidParams, Message: "invalid saved login parameters"}
	a := credentialArgs{Site: site}
	if a.validate() != nil {
		return invalid
	}
	label = strings.TrimSpace(label)
	if utf8.RuneCountInString(label) > 80 || len(startURL) > 512 {
		return invalid
	}
	if startURL != "" {
		u, err := url.Parse(startURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return invalid
		}
	}
	if len(values["password"]) == 0 || (len(values["username"]) == 0 && len(values["email"]) == 0) {
		return invalid
	}
	for field, value := range values {
		if (field != "password" && field != "username" && field != "email") || len(value) == 0 || len(value) > 16384 {
			return invalid
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.vault == nil {
		return credentialVaultError(vault.ErrNotFound)
	}
	if r.vault.IsLocked() {
		return credentialVaultError(vault.ErrLocked)
	}
	old, err := r.vault.ListMetadata(ctx)
	if err != nil {
		return credentialVaultError(err)
	}
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return credentialVaultError(err)
	}
	base := credentialHostName(a.Site) + "-" + hex.EncodeToString(suffix[:])
	_, err = r.storeBundleLocked(ctx, base, a.Site, "", values, true, map[string]string{"credential_label": label, "credential_start_url": startURL}, time.Time{})
	if err != nil {
		return err
	}
	// The vault exposes individual entry writes, not transactions. Keep the old
	// bundle usable until the complete replacement has been created.
	for _, md := range old {
		if md.Meta["credential_bundle"] != "" && md.Meta["credential_site"] == a.Site && md.Meta["credential_remembered"] == "true" {
			if err := r.vault.Delete(ctx, md.Name); err != nil {
				return credentialVaultError(err)
			}
		}
	}
	return nil
}

// savedLogin contains display metadata only. Listing never resolves a secret.
type savedLogin struct{ Label, StartURL, Site string }

func (r *CredentialRegistry) savedLogins(ctx context.Context) []savedLogin {
	if r == nil || r.vault == nil || r.vault.IsLocked() {
		return nil
	}
	entries, err := r.vault.ListMetadata(ctx)
	if err != nil {
		return nil
	}
	type bundle struct {
		login  savedLogin
		fields map[string]bool
	}
	bundles := map[string]*bundle{}
	for _, md := range entries {
		base, site, field := md.Meta["credential_bundle"], md.Meta["credential_site"], md.Meta["credential_field"]
		if base == "" || site == "" || md.Name != base+"-"+field || !credentialFields[field] || md.Meta["credential_remembered"] != "true" || md.ExpiresAt != nil || len(md.Domains) != 1 || md.Domains[0] != site || len(md.AllowedTools) != 1 || md.AllowedTools[0] != "browser_act" || len(md.Scopes) != 1 || md.Scopes[0] != vault.ScopeGeneral {
			continue
		}
		if bundles[base] == nil {
			bundles[base] = &bundle{login: savedLogin{Label: md.Meta["credential_label"], StartURL: md.Meta["credential_start_url"], Site: site}, fields: map[string]bool{}}
		}
		bundles[base].fields[field] = true
	}
	out := []savedLogin{}
	for _, b := range bundles {
		if b.fields["password"] && (b.fields["username"] || b.fields["email"]) {
			out = append(out, b.login)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Site != out[j].Site {
			return out[i].Site < out[j].Site
		}
		return out[i].Label < out[j].Label
	})
	// Auto-lock may have occurred while reading metadata.
	if r.vault.IsLocked() {
		return nil
	}
	return out
}

func savedLoginHint(logins []savedLogin) string {
	if len(logins) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\nSaved logins (display metadata; use request_credentials with the exact login host to receive refs; no form appears):")
	for _, login := range logins[:min(len(logins), 20)] {
		out.WriteString("\n\"")
		out.WriteString(cleanLoginHint(login.Label))
		out.WriteString("\"")
		if login.StartURL != "" {
			out.WriteString(" start ")
			out.WriteString(cleanLoginHint(login.StartURL))
		}
		out.WriteString(" login host ")
		out.WriteString(cleanLoginHint(login.Site))
		out.WriteByte('.')
	}
	out.WriteString("\nA site can redirect to another login host. Start from its start URL, then request credentials for the host of the page actually showing the login form.")
	out.WriteString("\nVoice transcripts can distort names and acronyms. When the request plausibly means exactly one saved login, such as by naming the same kind of site, use it instead of asking.")
	return out.String()
}

func cleanLoginHint(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '"' || r == '\'' || r == '`' || unicode.In(r, unicode.Pi, unicode.Pf) {
			return -1
		}
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, value))
}

// browserAssistantLimits changes only the conversational assistant's budget.
func browserAssistantLimits(defs []tools.Definition, steps int, timeout time.Duration) (int, time.Duration) {
	for _, def := range defs {
		if def.Name != "request_credentials" && !strings.HasPrefix(def.Name, "browser_") {
			continue
		}
		if n, err := strconv.Atoi(os.Getenv("EXCALIBUR_MASTER_STEPS")); err != nil || n <= 0 || n > 100 {
			steps = 48
		}
		return steps, max(timeout, 12*time.Minute)
	}
	return steps, timeout
}
