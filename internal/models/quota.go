package models

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// ErrProviderQuotaExhausted means all configured credentials are permanently
// unavailable for this process. It is infrastructure exhaustion, not work failure.
var ErrProviderQuotaExhausted = errors.New("provider_quota_exhausted")

// ExhaustedKeysEnabled controls the process-local credential quarantine.
func ExhaustedKeysEnabled() bool { return os.Getenv("EXCALIBUR_EXHAUSTED_KEYS") != "0" }

// QuotaProbeEnabled controls EXCALIBUR_QUOTA_PROBE (default ON). When on,
// a quota or billing response only quarantines its credential after a live
// probe on the same credential repeats the billing evidence. When off, the
// first recognized billing response quarantines directly (the W18 rule).
func QuotaProbeEnabled() bool { return os.Getenv("EXCALIBUR_QUOTA_PROBE") != "0" }

var exhaustedCredentials sync.Map

func credentialID(endpoint, key string) [32]byte {
	return sha256.Sum256([]byte(endpoint + "\x00" + key))
}
func credentialExhausted(id [32]byte) bool {
	if !ExhaustedKeysEnabled() {
		return false
	}
	_, ok := exhaustedCredentials.Load(id)
	return ok
}
func exhaustCredential(id [32]byte, evidence ...quotaEvidence) {
	if !ExhaustedKeysEnabled() {
		return
	}
	if _, loaded := exhaustedCredentials.LoadOrStore(id, true); !loaded {
		// Neither credential, hash, endpoint nor provider error text belongs in logs.
		var ev quotaEvidence
		if len(evidence) > 0 {
			ev = evidence[0]
		}
		slog.Warn("provider credential exhausted; disabled for the rest of this process", "status", ev.Status, "code", ev.Code, "type", ev.Type)
	}
}
func quotaExhaustedError(name string) error {
	return &providers.ProviderError{Kind: providers.KindAuth, Provider: name, Err: ErrProviderQuotaExhausted}
}

// Only exact structured billing identifiers qualify. Message text can mention
// billing in rate-limit guidance and must never quarantine a credential.
func permanentQuota(code string) bool {
	switch code {
	case "insufficient_quota", "billing_hard_limit", "billing_hard_limit_reached", "billing_not_active", "billing_error", "insufficient_balance", "credit_balance_too_low", "insufficient_credit", "credit_balance_exhausted":
		return true
	}
	return false
}

type quotaEvidence struct {
	Status int
	Code   string
	Type   string
}

// Diagnostic fields are untrusted provider strings. Retain only identifiers,
// never messages, URLs, arbitrary strings or credentials echoed by a server.
func quotaIdentifier(s string) string {
	if s != "" && !permanentQuota(s) && s != "rate_limit_exceeded" && s != "tokens" && s != "requests" {
		return "unrecognized"
	}
	return s
}

// observeQuota preserves the body for the adapter while retaining error codes
// which its normalization may omit. Error inspection is bounded to 64 KiB.
func observeQuota(ctx context.Context, resp *http.Response) {
	if !ExhaustedKeysEnabled() {
		return
	}
	state, ok := ctx.Value(delayKey{}).(*responseDelay)
	if !ok {
		return
	}
	quota := resp.StatusCode == http.StatusPaymentRequired
	ev := quotaEvidence{Status: resp.StatusCode}
	if resp.Body != nil && (resp.StatusCode == 429 || quota) {
		original := resp.Body
		body, _ := io.ReadAll(io.LimitReader(original, 64<<10))
		resp.Body = &restoredBody{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
		var envelope struct {
			Error struct {
				Code any    `json:"code"`
				Type string `json:"type"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			code, _ := envelope.Error.Code.(string)
			ev.Code, ev.Type = quotaIdentifier(code), quotaIdentifier(envelope.Error.Type)
			quota = quota || (state.provider == "openai" && resp.StatusCode == 429 && (permanentQuota(code) || permanentQuota(envelope.Error.Type)))
		}
	}
	state.mu.Lock()
	state.quota = state.quota || quota
	if quota {
		state.quotaEvidence = ev
	}
	state.mu.Unlock()
}

type restoredBody struct {
	io.Reader
	io.Closer
}
