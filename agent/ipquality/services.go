package ipquality

import "houfeng/internal/contracts/agentapi"

func collectDefaultServiceUnlocks(services []string) []serviceProbeOutcome {
	outcomes := make([]serviceProbeOutcome, len(services))
	for index, service := range services {
		source, ok := defaultServiceSource(service)
		if !ok {
			outcomes[index] = serviceProbeOutcome{Result: agentapi.IPQualityServiceUnlockPayload{
				Service:      service,
				Source:       "default_probe_registry",
				Status:       "unknown",
				ProbeStatus:  sourceStatusSkipped,
				ErrorCode:    "unsupported_service",
				ErrorSummary: "service is not supported by default IP quality probes",
			}}
			continue
		}
		outcomes[index] = serviceProbeOutcome{Result: agentapi.IPQualityServiceUnlockPayload{
			Service:      service,
			Source:       source,
			Status:       "unknown",
			ProbeStatus:  sourceStatusSkipped,
			ErrorCode:    "unsupported_default_probe",
			ErrorSummary: "safe default probe is not available without verified business evidence",
		}}
	}
	return outcomes
}

func defaultServiceSource(service string) (string, bool) {
	switch service {
	case "netflix":
		return "netflix_title_probe", true
	case "chatgpt":
		return "openai_status_probe", true
	case "youtube-premium":
		return "youtube_premium_page_probe", true
	case "amazon-prime-video":
		return "prime_video_page_probe", true
	case "disney-plus":
		return "disney_default_probe", true
	case "tiktok":
		return "tiktok_home_probe", true
	case "reddit":
		return "reddit_home_probe", true
	default:
		return "", false
	}
}
