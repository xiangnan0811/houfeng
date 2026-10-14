package ipquality

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"

	"houfeng/internal/contracts/agentapi"
)

const (
	sourceStatusSuccess       = "success"
	sourceStatusFailure       = "failure"
	sourceStatusSkipped       = "skipped"
	sourceStatusNotConfigured = "not_configured"

	sourceTypeDefault  = "default"
	sourceTypeOptional = "optional"
)

type providerSource interface {
	Name() string
	Collect(context.Context, *HTTPCollector, string) providerSourceOutcome
}

type providerSourceOutcome struct {
	Result    agentapi.IPQualityProviderResultPayload
	Payload   map[string]any
	Raw       json.RawMessage
	IPAddress string
}

type serviceProbeOutcome struct {
	Result agentapi.IPQualityServiceUnlockPayload
	Raw    json.RawMessage
}

type sourceRawEnvelope struct {
	Status       string          `json:"status"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorSummary string          `json:"error_summary,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

func sourceRawEnvelopeFromProvider(outcome providerSourceOutcome) sourceRawEnvelope {
	return sourceRawEnvelope{
		Status:       outcome.Result.Status,
		ErrorCode:    outcome.Result.ErrorCode,
		ErrorSummary: outcome.Result.ErrorSummary,
		Raw:          outcome.Raw,
	}
}

func sourceRawEnvelopeFromService(outcome serviceProbeOutcome) sourceRawEnvelope {
	return sourceRawEnvelope{
		Status:       outcome.Result.ProbeStatus,
		ErrorCode:    outcome.Result.ErrorCode,
		ErrorSummary: outcome.Result.ErrorSummary,
		Raw:          outcome.Raw,
	}
}

func validIPAddress(value string) bool {
	parsed := net.ParseIP(strings.TrimSpace(value))
	return parsed != nil && !parsed.IsUnspecified()
}

func elapsedMillis(start time.Time) *int {
	ms := int(time.Since(start).Milliseconds())
	if ms < 0 {
		ms = 0
	}
	return &ms
}

func sourceFailure(provider, sourceType, code, summary string, latency *int, raw json.RawMessage) providerSourceOutcome {
	safeSummary := "provider reported failure"
	if code == "invalid_response" {
		safeSummary = "invalid provider response"
	} else if code == "missing_target_ip" {
		safeSummary = summary
	}
	return providerSourceOutcome{
		Result: agentapi.IPQualityProviderResultPayload{
			Provider:     provider,
			Status:       sourceStatusFailure,
			SourceType:   sourceType,
			LatencyMS:    latency,
			ErrorCode:    code,
			ErrorSummary: safeSummary,
		},
		Raw: raw,
	}
}

func serviceFailure(service, source, code, summary string, latency *int, raw json.RawMessage) serviceProbeOutcome {
	return serviceProbeOutcome{
		Result: agentapi.IPQualityServiceUnlockPayload{
			Service:      service,
			Source:       source,
			Status:       "unknown",
			ProbeStatus:  sourceStatusFailure,
			LatencyMS:    latency,
			ErrorCode:    code,
			ErrorSummary: summary,
		},
		Raw: raw,
	}
}

// sourceHTTPError classifies the JSON/HTTP boundary without exposing response
// bodies or the transport's potentially credential-bearing error text.
type sourceHTTPError struct {
	code   string
	status int
	cause  error
}

func (e *sourceHTTPError) Error() string {
	if e.code == "non_json_response" {
		return "source returned an invalid JSON object"
	}
	return "source returned an unsuccessful HTTP status"
}

func (e *sourceHTTPError) Unwrap() error { return e.cause }

func errorCodeForHTTPError(err error) string {
	if err == nil {
		return ""
	}
	var sourceErr *sourceHTTPError
	if errors.As(err, &sourceErr) {
		switch sourceErr.code {
		case "non_json_response", "http_status":
			return sourceErr.code
		default:
			return "request_failed"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || httpErrorTimedOut(err) {
		return "timeout"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns_failed"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "connection_refused"
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return "network_unreachable"
	}
	var certificateErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var alertErr tls.AlertError
	var authorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidCertificateErr x509.CertificateInvalidError
	if errors.As(err, &certificateErr) || errors.As(err, &recordErr) ||
		errors.As(err, &alertErr) || errors.As(err, &authorityErr) || errors.As(err, &hostnameErr) || errors.As(err, &invalidCertificateErr) {
		return "tls_failed"
	}
	return "request_failed"
}

// Wrappers such as url.Error only inspect their immediate cause for Timeout.
// Walk the cause tree so an intervening %w wrapper cannot hide a typed timeout.
func httpErrorTimedOut(err error) bool {
	for err != nil {
		if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
			return true
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			for _, cause := range joined.Unwrap() {
				if httpErrorTimedOut(cause) {
					return true
				}
			}
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

func extraJSON(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(sanitizeRawJSONValue(value))
	if err != nil || len(payload) == 0 {
		return nil
	}
	if len(payload) > maxRawJSONBytes {
		payload, err = json.Marshal(map[string]any{
			"truncated": true,
			"reason":    "extra_json_size_limit",
		})
		if err != nil {
			return nil
		}
	}
	return json.RawMessage(payload)
}

func rawFromBytes(body []byte) json.RawMessage {
	if len(body) == 0 || !looksLikeJSONObject(body) {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil
	}
	return extraJSON(value)
}
