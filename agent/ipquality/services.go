package ipquality

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type serviceProbe struct {
	service string
	source  string
	url     string
	parse   func([]byte, *http.Response) agentapi.IPQualityServiceUnlockPayload
}

func collectDefaultServiceUnlock(ctx context.Context, collector *HTTPCollector, service string) serviceProbeOutcome {
	probe, ok := defaultServiceProbes()[service]
	if !ok {
		return serviceProbeOutcome{Result: agentapi.IPQualityServiceUnlockPayload{
			Service:      service,
			Source:       "default_probe_registry",
			Status:       "unknown",
			ProbeStatus:  sourceStatusSkipped,
			ErrorCode:    "unsupported_service",
			ErrorSummary: "service is not supported by default IP quality probes",
		}}
	}
	if probe.url == "" {
		return serviceProbeOutcome{Result: agentapi.IPQualityServiceUnlockPayload{
			Service:      probe.service,
			Source:       probe.source,
			Status:       "unknown",
			ProbeStatus:  sourceStatusSkipped,
			ErrorCode:    "unsupported_default_probe",
			ErrorSummary: "safe default probe is not available without optional service configuration",
		}}
	}

	startedAt := time.Now()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.url, nil)
	if err != nil {
		return serviceFailure(probe.service, probe.source, "invalid_request", err.Error(), elapsedMillis(startedAt), nil)
	}
	request.Header.Set("Accept", "text/html,application/json")
	request.Header.Set("User-Agent", collector.userAgent)
	response, err := collector.client.Do(request)
	latency := elapsedMillis(startedAt)
	if err != nil {
		return serviceFailure(probe.service, probe.source, errorCodeForHTTPError(err), err.Error(), latency, nil)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRawJSONBytes+1))
	if err != nil {
		return serviceFailure(probe.service, probe.source, "read_failed", err.Error(), latency, nil)
	}
	if len(body) > maxRawJSONBytes {
		body = body[:maxRawJSONBytes]
	}
	raw := rawForServiceResponse(body, response)
	if serviceProbeHTTPStatusFailure(response.StatusCode) {
		return serviceFailure(probe.service, probe.source, "http_status", fmt.Sprintf("http status %d", response.StatusCode), latency, raw)
	}
	result := probe.parse(body, response)
	result.Service = probe.service
	result.Source = probe.source
	result.ProbeStatus = sourceStatusSuccess
	result.LatencyMS = latency
	if len(result.ExtraJSON) == 0 {
		result.ExtraJSON = extraJSON(map[string]any{
			"http_status":  response.StatusCode,
			"content_type": safeHeader(response.Header, "Content-Type"),
		})
	}
	return serviceProbeOutcome{Result: result, Raw: raw}
}

func serviceProbeHTTPStatusFailure(status int) bool {
	if status >= 200 && status < 300 {
		return false
	}
	switch status {
	case http.StatusForbidden, http.StatusUnavailableForLegalReasons:
		return false
	default:
		return true
	}
}

func defaultServiceProbes() map[string]serviceProbe {
	return map[string]serviceProbe{
		"netflix": {
			service: "netflix",
			source:  "netflix_title_probe",
			url:     "https://www.netflix.com/title/81215567",
			parse:   parseNetflixProbe,
		},
		"chatgpt": {
			service: "chatgpt",
			source:  "openai_status_probe",
			url:     "https://chat.openai.com/backend-api/compliance",
			parse:   parseChatGPTProbe,
		},
		"youtube-premium": {
			service: "youtube-premium",
			source:  "youtube_premium_page_probe",
			url:     "https://www.youtube.com/premium",
			parse:   parseYouTubePremiumProbe,
		},
		"amazon-prime-video": {
			service: "amazon-prime-video",
			source:  "prime_video_page_probe",
			url:     "https://www.primevideo.com/",
			parse:   parsePrimeVideoProbe,
		},
		"disney-plus": {
			service: "disney-plus",
			source:  "disney_default_probe",
		},
		"tiktok": {
			service: "tiktok",
			source:  "tiktok_home_probe",
			url:     "https://www.tiktok.com/",
			parse:   parseTikTokProbe,
		},
		"reddit": {
			service: "reddit",
			source:  "reddit_home_probe",
			url:     "https://www.reddit.com/",
			parse:   parseRedditProbe,
		},
	}
}

func parseNetflixProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	text := strings.ToLower(string(body))
	status := "unlocked"
	unlockType := "full"
	if response.StatusCode == http.StatusForbidden || strings.Contains(text, "not available") || strings.Contains(text, "unavailable") {
		status = "blocked"
		unlockType = ""
	}
	region := firstNonEmpty(regionFromText(string(body)), safeHeader(response.Header, "X-Netflix-Country"))
	return agentapi.IPQualityServiceUnlockPayload{
		Status:     status,
		Region:     region,
		UnlockType: unlockType,
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
			"region":      region,
		}),
	}
}

func parseChatGPTProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	text := strings.ToLower(string(body))
	status := "unlocked"
	unlockType := "web"
	errorCode := ""
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnavailableForLegalReasons ||
		strings.Contains(text, "unsupported_country") || strings.Contains(text, "blocked") {
		status = "blocked"
		unlockType = ""
		errorCode = "region_blocked"
	}
	return agentapi.IPQualityServiceUnlockPayload{
		Status:     status,
		Region:     firstNonEmpty(regionFromText(string(body)), safeHeader(response.Header, "CF-IPCountry")),
		UnlockType: unlockType,
		ErrorCode:  errorCode,
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
			"cf_country":  safeHeader(response.Header, "CF-IPCountry"),
		}),
	}
}

func parseYouTubePremiumProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	text := strings.ToLower(string(body))
	status := "unlocked"
	unlockType := "premium"
	if response.StatusCode == http.StatusForbidden || strings.Contains(text, "not available") || strings.Contains(text, "not currently available") {
		status = "blocked"
		unlockType = ""
	}
	region := firstNonEmpty(regionFromText(string(body)), safeHeader(response.Header, "X-Goog-Country"))
	return agentapi.IPQualityServiceUnlockPayload{
		Status:     status,
		Region:     region,
		UnlockType: unlockType,
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
			"region":      region,
		}),
	}
}

func parsePrimeVideoProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	text := strings.ToLower(string(body))
	region := firstNonEmpty(regionFromText(string(body)), currentTerritoryFromText(string(body)))
	status := "unlocked"
	if response.StatusCode == http.StatusForbidden || strings.Contains(text, "not available") || strings.Contains(text, "geo") && strings.Contains(text, "blocked") {
		status = "blocked"
	}
	return agentapi.IPQualityServiceUnlockPayload{
		Status:     status,
		Region:     region,
		UnlockType: "prime",
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
			"territory":   region,
		}),
	}
}

func parseTikTokProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	text := strings.ToLower(string(body))
	status := "unlocked"
	errorCode := ""
	if response.StatusCode == http.StatusForbidden || strings.Contains(text, "captcha") || strings.Contains(text, "challenge") {
		status = "unknown"
		errorCode = "challenge"
	}
	return agentapi.IPQualityServiceUnlockPayload{
		Status:    status,
		Region:    firstNonEmpty(regionFromText(string(body)), safeHeader(response.Header, "X-Tt-Region")),
		ErrorCode: errorCode,
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
		}),
	}
}

func parseRedditProbe(body []byte, response *http.Response) agentapi.IPQualityServiceUnlockPayload {
	status := "unlocked"
	errorCode := ""
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnavailableForLegalReasons {
		status = "blocked"
		errorCode = "region_blocked"
	}
	return agentapi.IPQualityServiceUnlockPayload{
		Status:    status,
		Region:    regionFromText(string(body)),
		ErrorCode: errorCode,
		ExtraJSON: extraJSON(map[string]any{
			"http_status": response.StatusCode,
		}),
	}
}

func rawForServiceResponse(body []byte, response *http.Response) json.RawMessage {
	if raw := rawFromBytes(body); raw != nil {
		return raw
	}
	text := string(body)
	if len(text) > 2048 {
		text = text[:2048]
	}
	return extraJSON(map[string]any{
		"http_status":  response.StatusCode,
		"content_type": safeHeader(response.Header, "Content-Type"),
		"body_sample":  text,
	})
}

var (
	countryCodeRE      = regexp.MustCompile(`(?i)(countryCode|country_code|data-country-code|Region|region)["'=:\s]+["']?([A-Z]{2})`)
	currentTerritoryRE = regexp.MustCompile(`(?i)currentTerritory["'=:\s]+["']?([A-Z]{2})`)
)

func regionFromText(text string) string {
	match := countryCodeRE.FindStringSubmatch(text)
	if len(match) >= 3 {
		return strings.ToUpper(match[2])
	}
	return ""
}

func currentTerritoryFromText(text string) string {
	match := currentTerritoryRE.FindStringSubmatch(text)
	if len(match) >= 2 {
		return strings.ToUpper(match[1])
	}
	return ""
}
