package browser

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

var passwordField = regexp.MustCompile(`(?is)<input\b[^>]*\btype\s*=\s*["']?password\b`)
var otpField = regexp.MustCompile(`(?is)<input\b[^>]*(?:autocomplete\s*=\s*["']?one-time-code|(?:name|id)\s*=\s*["']?(?:otp|totp|verification[_-]?code)\b)`)
var captchaEmbed = regexp.MustCompile(`(?is)<(?:iframe|script)\b[^>]*(?:recaptcha|hcaptcha|challenges\.cloudflare\.com|turnstile)`)

// detectBlock examines loaded HTML, never executes page-provided instructions.
func detectBlock(rawURL, html string, status int) string {
	lower := strings.ToLower(html)
	if captchaEmbed.MatchString(html) || strings.Contains(lower, "verify you are human") || strings.Contains(lower, "checking your browser") {
		return capabilities.BlockedCaptcha
	}
	if otpField.MatchString(html) {
		return capabilities.Blocked2FA
	}
	u, _ := url.Parse(rawURL)
	login := false
	if u != nil {
		for _, part := range strings.Split(strings.ToLower(u.Path), "/") {
			if part == "login" || part == "signin" || part == "sign-in" {
				login = true
			}
		}
	}
	if passwordField.MatchString(html) || login {
		return capabilities.BlockedLogin
	}
	if status == 403 || status == 429 || strings.Contains(lower, "access denied") || strings.Contains(lower, "request blocked") {
		return capabilities.BlockedBySite
	}
	return ""
}
