package ipquality

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	return providerSourceOutcome{
		Result: agentapi.IPQualityProviderResultPayload{
			Provider:     provider,
			Status:       sourceStatusFailure,
			SourceType:   sourceType,
			LatencyMS:    latency,
			ErrorCode:    code,
			ErrorSummary: summary,
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

func errorCodeForHTTPError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "context deadline") || strings.Contains(message, "timeout") {
		return "timeout"
	}
	if strings.Contains(message, "non_json_response") {
		return "non_json_response"
	}
	if strings.Contains(message, "http status") {
		return "http_status"
	}
	return "request_failed"
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

func httpStatusError(status int) error {
	return fmt.Errorf("http status %d", status)
}

func safeHeader(headers http.Header, key string) string {
	value := strings.TrimSpace(headers.Get(key))
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
