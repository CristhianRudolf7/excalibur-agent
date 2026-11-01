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
	"strings"
	"sync"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// ErrProviderQuotaExhausted means all configured credentials are permanently
// unavailable for this process. It is infrastructure exhaustion, not work failure.
var ErrProviderQuotaExhausted = errors.New("provider_quota_exhausted")

// ExhaustedKeysEnabled controls the process-local credential quarantine.
func ExhaustedKeysEnabled() bool { return os.Getenv("EXCALIBUR_EXHAUSTED_KEYS") != "0" }

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
func exhaustCredential(id [32]byte) {
	if !ExhaustedKeysEnabled() {
		return
	}
	if _, loaded := exhaustedCredentials.LoadOrStore(id, true); !loaded {
		// Neither credential, hash, endpoint nor provider error text belongs in logs.
		slog.Warn("provider credential exhausted; disabled for the rest of this process")
	}
}
func quotaExhaustedError(name string) error {
	return &providers.ProviderError{Kind: providers.KindAuth, Provider: name, Err: ErrProviderQuotaExhausted}
}
func permanentQuota(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"insufficient_quota", "billing_hard_limit", "billing_not_active", "billing quota", "billing_error", "insufficient_balance", "credit_balance_too_low", "insufficient_credit", "credit balance is too low", "no credits remaining", "insufficient credits", "credits exhausted", "credit balance exhausted", "exhausted your credits", "payment required"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
func quotaError(err error) bool {
	if !ExhaustedKeysEnabled() || err == nil {
		return false
	}
	var pe *providers.ProviderError
	return errors.As(err, &pe) && permanentQuota(pe.Error())
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
	if resp.Body != nil && (resp.StatusCode == 429 || resp.StatusCode == 403 || quota) {
		original := resp.Body
		body, _ := io.ReadAll(io.LimitReader(original, 64<<10))
		resp.Body = &restoredBody{Reader: io.MultiReader(bytes.NewReader(body), original), Closer: original}
		var envelope struct {
			Error struct {
				Code    any    `json:"code"`
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			codes, _ := json.Marshal(envelope.Error.Code)
			quota = quota || permanentQuota(string(codes)+" "+envelope.Error.Type+" "+envelope.Error.Message)
		}
	}
	state.mu.Lock()
	state.quota = state.quota || quota
	state.mu.Unlock()
}

type restoredBody struct {
	io.Reader
	io.Closer
}
