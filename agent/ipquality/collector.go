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
	defaultLookupURL  = "https://ipapi.is/?q=self"
	defaultServiceURL = "https://ipapi.is/unlock/{service}"
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
	if lookupURL == "" {
		lookupURL = defaultLookupURL
	}
	serviceURL := strings.TrimSpace(opts.ServiceURL)
	if serviceURL == "" {
		serviceURL = defaultServiceURL
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &HTTPCollector{
		client:       client,
		lookupURL:    lookupURL,
		serviceURL:   serviceURL,
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
	report.RawJSON = rawEnvelope(lookupRaw, serviceRaw)
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
	raw := json.RawMessage(append([]byte(nil), body...))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, raw, fmt.Errorf("http status %d", response.StatusCode)
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, raw, err
	}
	return payload, raw, nil
}

func applyLookupPayload(report *agentapi.IPQualityReportPayload, payload map[string]any) {
	report.IPAddress = stringFromMap(payload, "ip", "ip_address", "query")
	report.IPVersion = intFromMap(payload, "version", "ip_version")
	report.ASN = stringFromMap(payload, "asn", "as", "as_number")
	report.Organization = stringFromMap(payload, "organization", "org", "isp")
	report.Latitude = floatPtrFromMap(payload, "latitude", "lat")
	report.Longitude = floatPtrFromMap(payload, "longitude", "lon", "lng")
	report.UseRegionCode = stringFromMap(payload, "use_region_code", "country_code", "country")
	report.UseRegionName = stringFromMap(payload, "use_region_name", "country_name")
	report.RegisteredRegionCode = stringFromMap(payload, "registered_region_code", "registered_country_code")
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
		result.Provider = "lookup"
	}
	return []agentapi.IPQualityProviderResultPayload{result}
}

func providerResultFromMap(payload map[string]any) agentapi.IPQualityProviderResultPayload {
	return agentapi.IPQualityProviderResultPayload{
		Provider:     stringFromMap(payload, "provider", "source"),
		UsageType:    stringFromMap(payload, "usage_type", "type"),
		CompanyType:  stringFromMap(payload, "company_type", "company"),
		RiskLevel:    stringFromMap(payload, "risk_level", "risk"),
		RiskScore:    stringFromMap(payload, "risk_score", "score"),
		RegionCode:   stringFromMap(payload, "region_code", "country_code", "country"),
		RegionName:   stringFromMap(payload, "region_name", "country_name"),
		IsProxy:      boolFromMap(payload, "is_proxy", "proxy"),
		IsTor:        boolFromMap(payload, "is_tor", "tor"),
		IsVPN:        boolFromMap(payload, "is_vpn", "vpn"),
		IsServer:     boolFromMap(payload, "is_server", "server", "hosting"),
		IsAbuser:     boolFromMap(payload, "is_abuser", "abuser", "abuse"),
		IsRobot:      boolFromMap(payload, "is_robot", "robot", "bot"),
		ErrorCode:    stringFromMap(payload, "error_code"),
		ErrorSummary: stringFromMap(payload, "error_summary", "message"),
	}
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
