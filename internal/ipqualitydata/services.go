// Package ipqualitydata contains pure IP quality parsing and assessment rules.
package ipqualitydata

import "houfeng/internal/contracts/agentapi"

// AggregateServiceChecks maps independently observed checks to the legacy carrier.
// A Netflix sample rejection is not evidence that the entire service is blocked.
func AggregateServiceChecks(service string, checks []agentapi.IPQualityServiceCheck) (status, unlockType string) {
	available, restricted := 0, 0
	for _, check := range checks {
		switch check.Status {
		case "available":
			available++
		case "restricted":
			restricted++
		}
	}
	switch service {
	case "netflix":
		if available > 0 {
			return "unlocked", "catalog_sample"
		}
		if len(checks) == 2 && restricted == 2 {
			return "partial", "sample_titles_unavailable"
		}
		return "unknown", ""
	case "chatgpt":
		if len(checks) == 2 && available == 2 {
			return "unlocked", ""
		}
		if len(checks) == 2 && restricted == 2 {
			return "blocked", ""
		}
		if available > 0 {
			return "partial", ""
		}
		return "unknown", ""
	default:
		if len(checks) != 1 {
			return "unknown", ""
		}
		if available == 1 {
			return "unlocked", ""
		}
		if restricted == 1 {
			return "blocked", ""
		}
		return "unknown", ""
	}
}
