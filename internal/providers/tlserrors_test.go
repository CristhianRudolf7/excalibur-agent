package providers

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"testing"
)

func TestIsCertificateError(t *testing.T) {
	// The exact pilot failure: a task image without CA certificates.
	pilot := &url.Error{
		Op:  "Post",
		URL: "https://api.openai.com/v1/responses",
		Err: &tls.CertificateVerificationError{
			Err: x509.UnknownAuthorityError{},
		},
	}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pilot unknown authority", pilot, true},
		{"bare unknown authority", x509.UnknownAuthorityError{}, true},
		{"hostname", x509.HostnameError{}, true},
		{"invalid", x509.CertificateInvalidError{}, true},
		{"constraint", x509.ConstraintViolationError{}, true},
		{"tls wrapper", &tls.CertificateVerificationError{Err: errors.New("boom")}, true},
		{"message unknown authority",
			fmt.Errorf(`Post "https://api.openai.com/v1/responses": tls: failed to verify certificate: x509: certificate signed by unknown authority`),
			true},
		{"message expired",
			errors.New(`certificate has expired or is not yet valid`),
			true},
		{"message hostname",
			errors.New(`certificate is valid for example.com, not example.org`),
			true},
		{"connection refused", errors.New("dial tcp: connection refused"), false},
		{"timeout", errors.New("net/http: timeout awaiting response headers"), false},
		{"reset", errors.New("read: connection reset by peer"), false},
		{"auth text", errors.New("http 401: Incorrect API key"), false},
	}
	for _, tc := range cases {
		if got := IsCertificateError(tc.err); got != tc.want {
			t.Errorf("%s: IsCertificateError = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTransportKind(t *testing.T) {
	if got := TransportKind(errors.New("dial tcp: connection refused")); got != KindTransient {
		t.Fatalf("plain transport failure = %s, want transient", got)
	}
	if got := TransportKind(fmt.Errorf("tls: failed to verify certificate: x509: certificate signed by unknown authority")); got != KindInput {
		t.Fatalf("certificate failure = %s, want input (non-retryable)", got)
	}
	if (&ProviderError{Kind: TransportKind(errors.New("boom"))}).Retryable() != true {
		t.Fatal("transient transport failure must stay retryable")
	}
	if (&ProviderError{Kind: TransportKind(fmt.Errorf("x509: certificate is valid for a, not b"))}).Retryable() {
		t.Fatal("certificate failure must not be retryable")
	}
}
