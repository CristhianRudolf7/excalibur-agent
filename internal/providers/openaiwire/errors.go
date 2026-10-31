package openaiwire

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/providers"
)

// maxErrorBody caps how much of a failure body is kept in the
// normalized error message. Bodies can carry long HTML pages.
const maxErrorBody = 2048

// statusKind maps an HTTP status to its retry class (Section 19.5).
// 429 is rate limited; 5xx and 408 are transient; 401/403/402 hold the
// request (auth/credentials/quota: pause and notify, no blind retry);
// anything else 4xx is an input or policy problem of this request.
func statusKind(status int) providers.ErrorKind {
	switch {
	case status == http.StatusTooManyRequests:
		return providers.KindRateLimited
	case status == http.StatusRequestTimeout ||
		(status >= 500 && status <= 599):
		return providers.KindTransient
	case status == http.StatusUnauthorized ||
		status == http.StatusForbidden ||
		status == http.StatusPaymentRequired:
		return providers.KindAuth
	default:
		return providers.KindInput
	}
}

// parseRetryAfter parses a Retry-After header value: delta seconds or
// an HTTP date. Invalid or missing values yield 0.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}

// errorCode flattens the polymorphic error code field to text.
func errorCode(code any) string {
	switch c := code.(type) {
	case nil:
		return ""
	case string:
		return c
	case float64:
		return strconv.FormatInt(int64(c), 10)
	default:
		return fmt.Sprintf("%v", c)
	}
}

// contentFilterCodes are wire error codes that mean the provider
// refused the content rather than rejecting the request shape.
var contentFilterCodes = map[string]bool{
	"content_filter":           true,
	"moderation_blocked":       true,
	"content_policy_violation": true,
	"safety_violation":         true,
	"responsible_ai_policy":    true,
}

// HTTPError converts a non-2xx response into a typed provider error,
// honoring Retry-After on 429/5xx (Section 19.5). Content refusals
// reported as errors classify as content rejected. Provider adapters
// reuse it for non chat-completions endpoints such as Responses.
func HTTPError(provider, model string, status int, headers http.Header, body []byte) *providers.ProviderError {
	return normalizeHTTPError(provider, model, status, headers, body)
}

// TransportError normalizes a failed HTTP round trip for adapters that
// post to their own endpoints. Canceled contexts surface as the
// context error itself so errors.Is keeps working.
func TransportError(provider, model string, err error, ctxErr error) error {
	return netError(provider, model, err, ctxErr)
}

// IsContentRefusal reports whether a wire error code or message means
// the provider refused the content rather than the request shape.
func IsContentRefusal(code, message string) bool {
	lower := strings.ToLower(code + " " + message)
	for token := range contentFilterCodes {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

// normalizeHTTPError converts a non-2xx response into a typed provider
// error, honoring Retry-After on 429/5xx (Section 19.5). Content
// refusals reported as errors classify as content rejected.
func normalizeHTTPError(provider, model string, status int, headers http.Header, body []byte) *providers.ProviderError {
	kind := statusKind(status)
	message := strings.TrimSpace(string(body))
	code := ""
	if len(body) > 0 {
		var we wireError
		if err := json.Unmarshal(body, &we); err == nil && we.Error != nil {
			code = strings.ToLower(errorCode(we.Error.Code) + " " + we.Error.Type)
			if we.Error.Message != "" {
				message = we.Error.Message
			}
		}
	}
	lower := strings.ToLower(message + " " + code)
	for token := range contentFilterCodes {
		if strings.Contains(lower, token) {
			kind = providers.KindContentRejected
			break
		}
	}
	if len(message) > maxErrorBody {
		message = message[:maxErrorBody] + "...(truncated)"
	}
	if message == "" {
		message = http.StatusText(status)
	}
	err := &providers.ProviderError{
		Kind:     kind,
		Provider: provider,
		Model:    model,
		Err:      fmt.Errorf("http %d: %s", status, message),
	}
	if kind == providers.KindRateLimited || kind == providers.KindTransient {
		err.RetryAfter = parseRetryAfter(headers.Get("Retry-After"), time.Now())
	}
	return err
}

// finishKind maps a Chat Completions finish reason to its error class.
// Empty and "stop" mean success; "length" hit the output limit;
// "content_filter" is a provider refusal. Tool-call finishes are
// success: the calls travel in the message.
func finishKind(reason string) (providers.ErrorKind, bool) {
	switch reason {
	case "", "stop", "tool_calls":
		return "", false
	case "length":
		return providers.KindTruncated, true
	case "content_filter":
		return providers.KindContentRejected, true
	default:
		return "", false
	}
}

// netError normalizes a failed HTTP round trip. Canceled contexts
// surface as the context error itself so callers can distinguish a
// cancellation from a retryable failure with errors.Is.
func netError(provider, model string, err error, ctxErr error) error {
	if ctxErr != nil {
		return ctxErr
	}
	var perr *providers.ProviderError
	if errors.As(err, &perr) {
		return err
	}
	return &providers.ProviderError{
		Kind:     providers.KindTransient,
		Provider: provider,
		Model:    model,
		Err:      err,
	}
}
