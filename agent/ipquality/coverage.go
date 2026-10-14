package ipquality

import (
	"encoding/json"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func serviceProbeSucceeded(result agentapi.IPQualityServiceUnlockPayload) bool {
	if result.ProbeStatus != sourceStatusSuccess {
		return false
	}
	switch result.Status {
	case "unlocked", "blocked", "partial":
		return true
	default:
		return false
	}
}

func coverageFromResults(providers []agentapi.IPQualityProviderResultPayload, services []agentapi.IPQualityServiceUnlockPayload) *agentapi.IPQualityCoveragePayload {
	coverage := &agentapi.IPQualityCoveragePayload{
		ExpectedProviderCount: len(providers),
		ExpectedServiceCount:  len(services),
	}
	for _, provider := range providers {
		switch provider.Status {
		case sourceStatusSuccess, "":
			coverage.SuccessfulProviderCount++
		case sourceStatusFailure:
			coverage.FailedProviderCount++
		case sourceStatusSkipped:
			coverage.SkippedProviderCount++
		case sourceStatusNotConfigured:
			coverage.NotConfiguredProviderCount++
		}
	}
	for _, service := range services {
		switch service.ProbeStatus {
		case sourceStatusSkipped:
			coverage.SkippedServiceCount++
		case sourceStatusNotConfigured:
			coverage.NotConfiguredServiceCount++
		default:
			if serviceProbeSucceeded(service) {
				coverage.SuccessfulServiceCount++
			} else {
				coverage.FailedServiceCount++
			}
		}
	}
	return coverage
}

func hasDefaultSourceFailure(providers []agentapi.IPQualityProviderResultPayload) bool {
	for _, provider := range providers {
		if provider.SourceType == sourceTypeDefault && provider.Status == sourceStatusFailure {
			return true
		}
	}
	return false
}

func hasServiceProbeFailure(services []agentapi.IPQualityServiceUnlockPayload) bool {
	for _, service := range services {
		switch service.ProbeStatus {
		case sourceStatusSkipped, sourceStatusNotConfigured:
			continue
		default:
			if !serviceProbeSucceeded(service) {
				return true
			}
		}
	}
	return false
}

func diagnosticsJSON(startedAt time.Time, discoveryCandidates, ipCandidates map[string]string, ipVersion int, egressAmbiguous bool, providers []agentapi.IPQualityProviderResultPayload) json.RawMessage {
	payload := map[string]any{
		"source_version":         "v2",
		"collection_revision":    3,
		"service_probe_revision": 2,
		"elapsed_ms":             int(time.Since(startedAt).Milliseconds()),
	}
	if ipVersion == 4 || ipVersion == 6 {
		payload["ip_address_family"] = ipVersion
	}
	if len(discoveryCandidates) > 0 {
		payload["discovery_ip_candidates"] = discoveryCandidates
	}
	if len(ipCandidates) > 0 {
		payload["ip_candidates"] = ipCandidates
	}
	if egressAmbiguous {
		payload["egress_ambiguous"] = true
	}
	for _, provider := range providers {
		if provider.ErrorCode == "ip_mismatch" {
			payload["ip_conflict"] = true
			break
		}
	}
	return extraJSON(payload)
}

func rawEnvelopeV2(providers map[string]sourceRawEnvelope, services map[string]sourceRawEnvelope, diagnostics json.RawMessage) json.RawMessage {
	envelope := map[string]any{}
	if len(providers) > 0 {
		providerValues := map[string]any{}
		for provider, raw := range providers {
			providerValues[provider] = rawEnvelopeValue(raw)
		}
		envelope["providers"] = providerValues
	}
	if len(services) > 0 {
		serviceValues := map[string]any{}
		for service, raw := range services {
			serviceValues[service] = rawEnvelopeValue(raw)
		}
		envelope["services"] = serviceValues
	}
	if len(diagnostics) > 0 {
		var value any
		if json.Unmarshal(diagnostics, &value) == nil {
			envelope["diagnostics"] = value
		}
	}
	payload, err := json.Marshal(sanitizeRawJSONValue(envelope))
	if err != nil || len(payload) == 0 {
		return nil
	}
	if len(payload) <= maxRawJSONBytes {
		return json.RawMessage(payload)
	}
	payload, err = json.Marshal(map[string]any{
		"truncated":   true,
		"reason":      "raw_json_size_limit",
		"diagnostics": envelope["diagnostics"],
	})
	if err != nil || len(payload) > maxRawJSONBytes {
		return nil
	}
	return json.RawMessage(payload)
}

func rawEnvelopeValue(raw sourceRawEnvelope) map[string]any {
	value := map[string]any{
		"status": raw.Status,
	}
	if raw.ErrorCode != "" {
		value["error_code"] = raw.ErrorCode
	}
	if raw.ErrorSummary != "" {
		value["error_summary"] = raw.ErrorSummary
	}
	if len(raw.Raw) > 0 {
		var decoded any
		if json.Unmarshal(raw.Raw, &decoded) == nil {
			value["raw"] = decoded
		}
	}
	return value
}
