package ipqualitydata

import (
	"houfeng/internal/contracts/agentapi"
	"testing"
)

func TestAggregateServiceChecks(t *testing.T) {
	for _, test := range []struct{ service, first, second, status, kind string }{
		{"chatgpt", "available", "available", "unlocked", ""},
		{"chatgpt", "available", "unknown", "partial", ""},
		{"chatgpt", "available", "restricted", "partial", ""},
		{"chatgpt", "unknown", "available", "partial", ""},
		{"chatgpt", "restricted", "available", "partial", ""},
		{"chatgpt", "unknown", "unknown", "unknown", ""},
		{"chatgpt", "restricted", "restricted", "blocked", ""},
		{"chatgpt", "restricted", "unknown", "unknown", ""},
		{"netflix", "available", "unknown", "unlocked", "catalog_sample"},
		{"netflix", "restricted", "restricted", "partial", "sample_titles_unavailable"},
		{"netflix", "restricted", "unknown", "unknown", ""},
		{"netflix", "unknown", "available", "unlocked", "catalog_sample"},
		{"netflix", "restricted", "available", "unlocked", "catalog_sample"},
		{"netflix", "unknown", "unknown", "unknown", ""},
		{"youtube-premium", "available", "", "unlocked", ""},
		{"youtube-premium", "restricted", "", "blocked", ""},
		{"amazon-prime-video", "unknown", "", "unknown", ""},
		{"disney-plus", "restricted", "", "blocked", ""},
		{"tiktok", "available", "", "unlocked", ""},
		{"reddit", "unknown", "", "unknown", ""},
	} {
		checks := []agentapi.IPQualityServiceCheck{{Status: test.first}}
		if test.second != "" {
			checks = append(checks, agentapi.IPQualityServiceCheck{Status: test.second})
		}
		status, kind := AggregateServiceChecks(test.service, checks)
		if status != test.status || kind != test.kind {
			t.Errorf("%s %s/%s = %s/%s, want %s/%s", test.service, test.first, test.second, status, kind, test.status, test.kind)
		}
	}
}
