package browser

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/capabilities"
)

var passwordField = regexp.MustCompile(`(?is)<input\b[^>]*\btype\s*=\s*["']?password\b`)
var otpField = regexp.MustCompile(`(?is)<input\b[^>]*(?:autocomplete\s*=\s*["']?one-time-code|(?:name|id)\s*=\s*["']?(?:otp|totp|verification[_-]?code)\b)`)
var captchaEmbed = regexp.MustCompile(`(?is)<(?:iframe|script)\b[^>]*(?:recaptcha|hcaptcha|challenges\.cloudflare\.com|turnstile)[^>]*>`)

// Score-based reCAPTCHA shows no challenge: the page keeps working and only its
// badge appears. These tags alone are not a block; any other embed still is.
var scoreCaptcha = regexp.MustCompile(`(?is)recaptcha/(?:api|enterprise)\.js\?(?:[^"'>]*&(?:amp;)?)?render=[^"'&>]*|recaptcha/(?:api2|enterprise)/anchor\?[^"'>]*\bsize=invisible|gstatic\.com/recaptcha/releases/`)
var explicitCaptcha = regexp.MustCompile(`(?is)\brender=(?:explicit|onload)\b`)

func captchaChallenge(html string) bool {
	for _, tag := range captchaEmbed.FindAllString(html, -1) {
		if !scoreCaptcha.MatchString(tag) || explicitCaptcha.MatchString(tag) {
			return true
		}
	}
	return false
}

// detectBlock examines loaded HTML, never executes page-provided instructions.
func detectBlock(rawURL, html string, status int) string {
	lower := strings.ToLower(html)
	if captchaChallenge(html) || strings.Contains(lower, "verify you are human") || strings.Contains(lower, "checking your browser") {
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
