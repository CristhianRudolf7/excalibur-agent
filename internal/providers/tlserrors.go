package providers

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

// IsCertificateError reports whether err is a TLS certificate
// verification failure: unknown authority (the CA-less task-image
// case of Section 23.2), hostname mismatch, expiry or any other
// x509/tls verification error. Such failures are configuration
// errors of this deployment, not provider outages: retrying or
// failing over cannot fix them, so adapters classify them as
// KindInput (Section 19.5, defect w8-bench-fixes/2).
func IsCertificateError(err error) bool {
	if err == nil {
		return false
	}
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return true
	}
	var unknownAuth x509.UnknownAuthorityError
	if errors.As(err, &unknownAuth) {
		return true
	}
	var hostname x509.HostnameError
	if errors.As(err, &hostname) {
		return true
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		return true
	}
	var constraint x509.ConstraintViolationError
	if errors.As(err, &constraint) {
		return true
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "x509:") {
		return true
	}
	if !strings.Contains(lower, "certificate") {
		return false
	}
	for _, marker := range []string{
		"unknown authority",
		"verify failed",
		"not trusted",
		"signed by unknown",
		"expired",
		"hostname",
		"is valid for",
		"does not match",
		"unable to get local issuer",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// TransportKind maps a failed HTTP round trip to its retry class for
// adapters without their own wire classifier: a canceled context
// stays a context error upstream (callers check ctx first);
// certificate failures are non-retryable configuration errors;
// everything else is transient (Section 19.5).
func TransportKind(err error) ErrorKind {
	if IsCertificateError(err) {
		return KindInput
	}
	return KindTransient
}
