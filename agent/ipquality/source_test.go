package ipquality

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestHTTPErrorClassificationUsesTypedCauses(t *testing.T) {
	tests := []struct {
		name, code string
		err        error
	}{
		{"nil", "", nil},
		{"non_json", "non_json_response", &sourceHTTPError{code: "non_json_response", status: 503, cause: errors.New("response-secret")}},
		{"http_429", "http_status", &sourceHTTPError{code: "http_status", status: 429}},
		{"http_503", "http_status", &sourceHTTPError{code: "http_status", status: 503}},
		{"source_boundary_precedes_cause", "non_json_response", &sourceHTTPError{code: "non_json_response", cause: context.DeadlineExceeded}},
		{"invalid_source_code", "request_failed", &sourceHTTPError{code: "token-secret"}},
		{"deadline", "timeout", context.DeadlineExceeded},
		{"dns", "dns_failed", &net.DNSError{Err: "dns-secret", Name: "secret.example", IsNotFound: true}},
		{"dns_timeout", "timeout", &net.DNSError{Err: "dns-secret", IsTimeout: true}},
		{"joined_dns_timeout", "timeout", errors.Join(errors.New("request-secret"), fmt.Errorf("nested-secret: %w", &net.DNSError{IsTimeout: true}))},
		{"operation_wrapped_dns_timeout", "timeout", &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("nested-secret: %w", &net.DNSError{IsTimeout: true})}},
		{"refused", "connection_refused", &net.OpError{Op: "dial", Net: "tcp6", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}},
		{"network_unreachable", "network_unreachable", syscall.ENETUNREACH},
		{"host_unreachable", "network_unreachable", syscall.EHOSTUNREACH},
		{"tls_record", "tls_failed", tls.RecordHeaderError{Msg: "tls-secret"}},
		{"tls_alert", "tls_failed", tls.AlertError(40)},
		{"tls_unknown_authority", "tls_failed", x509.UnknownAuthorityError{Cert: &x509.Certificate{}}},
		{"tls_hostname", "tls_failed", x509.HostnameError{Certificate: &x509.Certificate{}, Host: "secret.example"}},
		{"tls_invalid_certificate", "tls_failed", x509.CertificateInvalidError{Cert: &x509.Certificate{}}},
		{"tls_verification", "tls_failed", &tls.CertificateVerificationError{Err: errors.New("certificate-secret")}},
		{"canceled", "request_failed", context.Canceled},
		{"string_lookalikes", "request_failed", errors.New("timeout context deadline non_json_response http status 503 secret-token")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := errorCodeForHTTPError(test.err); got != test.code {
				t.Fatalf("classification = %s, want %s", got, test.code)
			}
			if test.err != nil {
				wrapped := &url.Error{Op: "Get", URL: "https://secret.example/?token=secret-token", Err: fmt.Errorf("outer-secret: %w", test.err)}
				if got := errorCodeForHTTPError(wrapped); got != test.code {
					t.Fatalf("wrapped classification = %s, want %s", got, test.code)
				}
			}
		})
	}
}

func TestSourceHTTPErrorKeepsCauseWithoutExposingIt(t *testing.T) {
	cause := errors.New("https://secret.example/?token=private-secret response-secret")
	for _, code := range []string{"non_json_response", "http_status"} {
		err := &sourceHTTPError{code: code, status: 503, cause: cause}
		if !errors.Is(err, cause) || err.status != 503 {
			t.Fatal("source boundary lost typed cause or HTTP status")
		}
		for _, forbidden := range []string{"secret", "https://", "token="} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("unsafe source error: %s", err.Error())
			}
		}
	}
}
