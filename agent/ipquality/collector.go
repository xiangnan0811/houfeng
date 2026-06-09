package ipquality

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
)

const (
	defaultLookupURL  = "https://api.ipapi.is"
	defaultServiceURL = ""
	defaultUserAgent  = "houfeng-agent/ip-quality"
	maxRawJSONBytes   = 128 * 1024
)

type Collector interface {
	Collect(context.Context, *agentapi.IPQualityPlan, time.Time) agentapi.IPQualityReportPayload
}

type HTTPCollectorOptions struct {
	Client       *http.Client
	LookupURL    string
	ServiceURL   string
	AgentVersion string
	Fingerprint  string
	SyncBatchID  string
	UserAgent    string
}

type HTTPCollector struct {
	client       *http.Client
	lookupURL    string
	serviceURL   string
	customLookup bool
	agentVersion string
	fingerprint  string
	syncBatchID  string
	userAgent    string
}

func NewHTTPCollector(opts HTTPCollectorOptions) *HTTPCollector {
	client := opts.Client
	if client == nil {
		client = http.DefaultClient
	}
	lookupURL := strings.TrimSpace(opts.LookupURL)
	customLookup := lookupURL != ""
	if lookupURL == "" {
		lookupURL = defaultLookupURL
	}
	serviceURL := strings.TrimSpace(opts.ServiceURL)
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &HTTPCollector{
		client:       client,
		lookupURL:    lookupURL,
		serviceURL:   serviceURL,
		customLookup: customLookup,
		agentVersion: opts.AgentVersion,
		fingerprint:  opts.Fingerprint,
		syncBatchID:  opts.SyncBatchID,
		userAgent:    userAgent,
	}
}

func (c *HTTPCollector) WithMetadata(agentVersion, fingerprint, syncBatchID string) *HTTPCollector {
	cloned := *c
	cloned.agentVersion = agentVersion
	cloned.fingerprint = fingerprint
	cloned.syncBatchID = syncBatchID
	return &cloned
}

func (c *HTTPCollector) Collect(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) agentapi.IPQualityReportPayload {
	if !c.customLookup {
		return c.collectDefault(ctx, plan, observedAt)
	}
	return c.collectLegacy(ctx, plan, observedAt)
}

func (c *HTTPCollector) collectLegacy(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) agentapi.IPQualityReportPayload {
	report := agentapi.IPQualityReportPayload{
		ObservedAt:   observedAt.UTC(),
		AgentVersion: c.agentVersion,
		Fingerprint:  c.fingerprint,
		SyncBatchID:  c.syncBatchID,
		IPAddress:    "0.0.0.0",
		IPVersion:    4,
		Status:       agentapi.IPQualityStatusFailure,
	}
	if plan == nil || !plan.Enabled {
		report.ErrorCode = "disabled"
		report.ErrorSummary = "ip quality collection disabled"
		return report
	}
	timeout := time.Duration(plan.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	collectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	lookupPayload, lookupRaw, err := c.getJSON(collectCtx, c.lookupURL)
	if err != nil {
		report.ErrorCode = "lookup_failed"
		report.ErrorSummary = err.Error()
		report.RawJSON = rawEnvelope(lookupRaw, nil)
		return report
	}
	applyLookupPayload(&report, lookupPayload)
	if parsedIP := net.ParseIP(report.IPAddress); parsedIP == nil {
		report.IPAddress = "0.0.0.0"
		report.IPVersion = 4
	}
	if report.IPVersion != 4 && report.IPVersion != 6 {
		if strings.Contains(report.IPAddress, ":") {
			report.IPVersion = 6
		} else {
			report.IPVersion = 4
		}
	}
	report.Status = agentapi.IPQualityStatusSuccess

	serviceRaw := make(map[string]json.RawMessage)
	if c.serviceURL != "" {
		for _, service := range normalizedServices(plan.Services) {
			unlock, raw, err := c.collectServiceUnlock(collectCtx, service)
			if len(raw) > 0 {
				serviceRaw[service] = raw
			}
			if err != nil {
				report.Status = agentapi.IPQualityStatusPartial
				unlock = agentapi.IPQualityServiceUnlockPayload{
					Service:      service,
					Status:       "unknown",
					ErrorCode:    "probe_failed",
					ErrorSummary: err.Error(),
				}
			}
			report.ServiceUnlocks = append(report.ServiceUnlocks, unlock)
		}
	}
	report.RawJSON = rawEnvelope(lookupRaw, serviceRaw)
	return report
}

func (c *HTTPCollector) collectDefault(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) agentapi.IPQualityReportPayload {
	startedAt := time.Now()
	report := agentapi.IPQualityReportPayload{
		ObservedAt:   observedAt.UTC(),
		AgentVersion: c.agentVersion,
		Fingerprint:  c.fingerprint,
		SyncBatchID:  c.syncBatchID,
		IPAddress:    "0.0.0.0",
		IPVersion:    4,
		Status:       agentapi.IPQualityStatusFailure,
	}
	if plan == nil || !plan.Enabled {
		report.ErrorCode = "disabled"
		report.ErrorSummary = "ip quality collection disabled"
		return report
	}
	timeout := time.Duration(plan.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	collectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	providerRaw := map[string]sourceRawEnvelope{}
	providerOutcomes := make([]providerSourceOutcome, 0)
	ipCandidates := map[string]string{}
	targetIP := ""
	for _, source := range defaultProviderSources() {
		sourceCtx, sourceCancel := context.WithTimeout(collectCtx, perSourceTimeout(timeout))
		outcome := source.Collect(sourceCtx, c, targetIP)
		sourceCancel()
		providerOutcomes = append(providerOutcomes, outcome)
		report.ProviderResults = append(report.ProviderResults, outcome.Result)
		providerRaw[outcome.Result.Provider] = sourceRawEnvelopeFromProvider(outcome)
		if outcome.Result.Status == sourceStatusSuccess && validIPAddress(outcome.IPAddress) {
			ipCandidates[outcome.Result.Provider] = outcome.IPAddress
			if targetIP == "" {
				targetIP = outcome.IPAddress
			}
		}
	}
	for _, result := range optionalProviderDiagnostics() {
		report.ProviderResults = append(report.ProviderResults, result)
		providerRaw[result.Provider] = sourceRawEnvelope{Status: result.Status, ErrorCode: result.ErrorCode, ErrorSummary: result.ErrorSummary}
	}

	successfulProviders := successfulProviderOutcomes(providerOutcomes)
	report.Coverage = coverageFromResults(report.ProviderResults, nil)
	if len(successfulProviders) == 0 {
		report.ErrorCode = "lookup_failed"
		report.ErrorSummary = "all default IP quality provider sources failed"
		report.DiagnosticsJSON = diagnosticsJSON(startedAt, ipCandidates)
		report.RawJSON = rawEnvelopeV2(providerRaw, nil, report.DiagnosticsJSON)
		return report
	}

	preferred := preferredProviderOutcome(successfulProviders)
	collectedProviders := append([]agentapi.IPQualityProviderResultPayload(nil), report.ProviderResults...)
	applyLookupPayload(&report, preferred.Payload)
	report.ProviderResults = collectedProviders
	if validIPAddress(preferred.IPAddress) {
		report.IPAddress = preferred.IPAddress
	}
	if parsedIP := net.ParseIP(report.IPAddress); parsedIP == nil {
		report.IPAddress = "0.0.0.0"
		report.IPVersion = 4
	} else if parsedIP.To4() == nil {
		report.IPVersion = 6
	} else {
		report.IPVersion = 4
	}
	applyReportFallbacksFromProviders(&report, successfulProviders)

	serviceRaw := map[string]sourceRawEnvelope{}
	for _, service := range normalizedServices(plan.Services) {
		serviceCtx, serviceCancel := context.WithTimeout(collectCtx, perSourceTimeout(timeout))
		outcome := collectDefaultServiceUnlock(serviceCtx, c, service)
		serviceCancel()
		report.ServiceUnlocks = append(report.ServiceUnlocks, outcome.Result)
		serviceRaw[service] = sourceRawEnvelopeFromService(outcome)
	}
	report.Coverage = coverageFromResults(report.ProviderResults, report.ServiceUnlocks)
	report.DiagnosticsJSON = diagnosticsJSON(startedAt, ipCandidates)
	report.RawJSON = rawEnvelopeV2(providerRaw, serviceRaw, report.DiagnosticsJSON)
	report.Status = agentapi.IPQualityStatusSuccess
	if hasDefaultSourceFailure(report.ProviderResults) || hasServiceProbeFailure(report.ServiceUnlocks) || hasIPCandididateConflict(ipCandidates) {
		report.Status = agentapi.IPQualityStatusPartial
	}
	return report
}

func (c *HTTPCollector) collectServiceUnlock(ctx context.Context, service string) (agentapi.IPQualityServiceUnlockPayload, json.RawMessage, error) {
	url := strings.ReplaceAll(c.serviceURL, "{service}", service)
	payload, raw, err := c.getJSON(ctx, url)
	if err != nil {
		return agentapi.IPQualityServiceUnlockPayload{}, raw, err
	}
	result := agentapi.IPQualityServiceUnlockPayload{
		Service:      service,
		Status:       stringFromMap(payload, "status", "unlock_status"),
		Region:       stringFromMap(payload, "region", "unlock_region", "country", "country_code"),
		UnlockType:   stringFromMap(payload, "unlock_type", "type"),
		ErrorCode:    stringFromMap(payload, "error_code"),
		ErrorSummary: stringFromMap(payload, "error_summary", "message"),
	}
	if result.Status == "" {
		if unlocked := boolFromMap(payload, "unlocked"); unlocked != nil && *unlocked {
			result.Status = "unlocked"
		} else if unlocked != nil {
			result.Status = "blocked"
		} else {
			result.Status = "unknown"
		}
	}
	return result, raw, nil
}

func (c *HTTPCollector) getJSON(ctx context.Context, url string) (map[string]any, json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRawJSONBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(body) > maxRawJSONBytes {
		body = body[:maxRawJSONBytes]
	}
	if !looksLikeJSONObject(body) {
		return nil, nil, fmt.Errorf("non_json_response: http status %d content-type %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	raw := json.RawMessage(append([]byte(nil), body...))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, raw, fmt.Errorf("http status %d", response.StatusCode)
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, nil, fmt.Errorf("non_json_response: %w", err)
	}
	return payload, raw, nil
}

func looksLikeJSONObject(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func perSourceTimeout(total time.Duration) time.Duration {
	if total <= 0 {
		return 5 * time.Second
	}
	timeout := total / 2
	if timeout > 5*time.Second {
		timeout = 5 * time.Second
	}
	if timeout < 250*time.Millisecond {
		timeout = 250 * time.Millisecond
	}
	if timeout > total {
		return total
	}
	return timeout
}

func applyLookupPayload(report *agentapi.IPQualityReportPayload, payload map[string]any) {
	report.IPAddress = stringFromMap(payload, "ip", "ip_address", "query")
	report.IPVersion = intFromMap(payload, "version", "ip_version")
	report.ASN = asnStringFromPayload(payload)
	report.Organization = firstNonEmpty(
		stringFromMap(payload, "organization", "org", "isp"),
		stringFromNestedMap(payload, "asn", "org", "organization", "name"),
		stringFromNestedMap(payload, "company", "name"),
	)
	report.Latitude = floatPtrFromMap(payload, "latitude", "lat")
	if report.Latitude == nil {
		report.Latitude = floatPtrFromNestedMap(payload, "location", "latitude", "lat")
	}
	report.Longitude = floatPtrFromMap(payload, "longitude", "lon", "lng")
	if report.Longitude == nil {
		report.Longitude = floatPtrFromNestedMap(payload, "location", "longitude", "lon", "lng")
	}
	report.UseRegionCode = firstNonEmpty(
		stringFromMap(payload, "use_region_code", "country_code", "country"),
		stringFromNestedMap(payload, "location", "country_code"),
	)
	report.UseRegionName = firstNonEmpty(
		stringFromMap(payload, "use_region_name", "country_name"),
		stringFromNestedMap(payload, "location", "country"),
	)
	report.RegisteredRegionCode = firstNonEmpty(
		stringFromMap(payload, "registered_region_code", "registered_country_code"),
		stringFromNestedMap(payload, "asn", "country", "country_code"),
	)
	report.RegisteredRegionName = stringFromMap(payload, "registered_region_name", "registered_country_name")
	report.RiskLevel = stringFromMap(payload, "risk_level", "risk")
	report.ProviderResults = providerResultsFromLookup(payload)
}

func providerResultsFromLookup(payload map[string]any) []agentapi.IPQualityProviderResultPayload {
	values, ok := payload["provider_results"].([]any)
	if !ok {
		values, ok = payload["providers"].([]any)
	}
	if ok {
		out := make([]agentapi.IPQualityProviderResultPayload, 0, len(values))
		for _, value := range values {
			providerMap, ok := value.(map[string]any)
			if !ok {
				continue
			}
			result := providerResultFromMap(providerMap)
			if result.Provider != "" {
				out = append(out, result)
			}
		}
		return out
	}
	result := providerResultFromMap(payload)
	if result.Provider == "" {
		if hasIPAPIShape(payload) {
			result.Provider = "ipapi.is"
		} else {
			result.Provider = "lookup"
		}
	}
	return []agentapi.IPQualityProviderResultPayload{result}
}

func providerResultFromMap(payload map[string]any) agentapi.IPQualityProviderResultPayload {
	return agentapi.IPQualityProviderResultPayload{
		Provider:     stringFromMap(payload, "provider", "source"),
		UsageType:    firstNonEmpty(stringFromMap(payload, "usage_type", "type"), stringFromNestedMap(payload, "asn", "type")),
		CompanyType:  firstNonEmpty(stringFromMap(payload, "company_type", "company"), stringFromNestedMap(payload, "company", "type")),
		RiskLevel:    stringFromMap(payload, "risk_level", "risk"),
		RiskScore:    stringFromMap(payload, "risk_score", "score"),
		RegionCode:   firstNonEmpty(stringFromMap(payload, "region_code", "country_code", "country"), stringFromNestedMap(payload, "location", "country_code")),
		RegionName:   firstNonEmpty(stringFromMap(payload, "region_name", "country_name"), stringFromNestedMap(payload, "location", "country")),
		IsProxy:      boolFromMap(payload, "is_proxy", "proxy"),
		IsTor:        boolFromMap(payload, "is_tor", "tor"),
		IsVPN:        boolFromMap(payload, "is_vpn", "vpn"),
		IsServer:     boolFromMap(payload, "is_server", "is_datacenter", "server", "hosting", "datacenter"),
		IsAbuser:     boolFromMap(payload, "is_abuser", "abuser", "abuse"),
		IsRobot:      boolFromMap(payload, "is_robot", "is_crawler", "robot", "bot", "crawler"),
		ErrorCode:    stringFromMap(payload, "error_code"),
		ErrorSummary: stringFromMap(payload, "error_summary", "message"),
	}
}

func hasIPAPIShape(payload map[string]any) bool {
	if _, ok := payload["asn"].(map[string]any); ok {
		return true
	}
	if _, ok := payload["company"].(map[string]any); ok {
		return true
	}
	if _, ok := payload["location"].(map[string]any); ok {
		return true
	}
	return false
}

func asnStringFromPayload(payload map[string]any) string {
	value := firstNonEmpty(
		stringFromMap(payload, "asn", "as", "as_number"),
		stringFromNestedMap(payload, "asn", "asn", "as", "as_number"),
	)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(strings.TrimSpace(value))
	if strings.HasPrefix(upper, "AS") {
		return upper
	}
	return "AS" + value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nestedMap(values map[string]any, key string) map[string]any {
	child, ok := values[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}

func stringFromNestedMap(values map[string]any, key string, nestedKeys ...string) string {
	child := nestedMap(values, key)
	if child == nil {
		return ""
	}
	return stringFromMap(child, nestedKeys...)
}

func floatPtrFromNestedMap(values map[string]any, key string, nestedKeys ...string) *float64 {
	child := nestedMap(values, key)
	if child == nil {
		return nil
	}
	return floatPtrFromMap(child, nestedKeys...)
}

func rawEnvelope(lookup json.RawMessage, services map[string]json.RawMessage) json.RawMessage {
	envelope := map[string]any{}
	if len(lookup) > 0 {
		var lookupValue any
		if json.Unmarshal(lookup, &lookupValue) == nil {
			envelope["lookup"] = sanitizeRawJSONValue(lookupValue)
		}
	}
	if len(services) > 0 {
		serviceValues := map[string]any{}
		for service, raw := range services {
			var value any
			if json.Unmarshal(raw, &value) == nil {
				serviceValues[service] = sanitizeRawJSONValue(value)
			}
		}
		if len(serviceValues) > 0 {
			envelope["services"] = serviceValues
		}
	}
	payload, err := json.Marshal(envelope)
	if err != nil || len(payload) == 0 {
		return nil
	}
	if len(payload) > maxRawJSONBytes {
		envelope["truncated"] = true
		delete(envelope, "services")
		payload, err = json.Marshal(envelope)
		if err != nil || len(payload) == 0 {
			return nil
		}
		if len(payload) > maxRawJSONBytes {
			return nil
		}
	}
	return json.RawMessage(payload)
}

func sanitizeRawJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveRawJSONKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitizeRawJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, sanitizeRawJSONValue(child))
		}
		return out
	default:
		return value
	}
}

func isSensitiveRawJSONKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	return normalized == "authorization" ||
		normalized == "cookie" ||
		normalized == "set_cookie" ||
		normalized == "token" ||
		normalized == "access_token" ||
		normalized == "refresh_token" ||
		normalized == "api_key" ||
		normalized == "apikey" ||
		normalized == "secret" ||
		strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_key") ||
		strings.Contains(normalized, "password")
}

func normalizedServices(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		service := strings.ToLower(strings.TrimSpace(value))
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		out = append(out, service)
	}
	return out
}

func stringFromMap(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case string:
			return strings.TrimSpace(typed)
		case json.Number:
			return typed.String()
		case float64:
			return fmt.Sprintf("%.0f", typed)
		case bool:
			if typed {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func intFromMap(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if n, err := typed.Int64(); err == nil {
				return int(n)
			}
		case float64:
			return int(typed)
		case string:
			if strings.Contains(typed, "6") {
				return 6
			}
			if strings.Contains(typed, "4") {
				return 4
			}
		}
	}
	return 0
}

func floatPtrFromMap(values map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			if n, err := typed.Float64(); err == nil {
				return &n
			}
		case float64:
			n := typed
			return &n
		}
	}
	return nil
}

func boolFromMap(values map[string]any, keys ...string) *bool {
	for _, key := range keys {
		value, ok := values[key]
		if !ok || value == nil {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return &typed
		case string:
			switch strings.ToLower(strings.TrimSpace(typed)) {
			case "true", "yes", "1":
				v := true
				return &v
			case "false", "no", "0":
				v := false
				return &v
			}
		}
	}
	return nil
}
