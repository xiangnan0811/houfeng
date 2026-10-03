package ipquality

import (
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestServiceCoverageRequiresSuccessfulBusinessConclusion(t *testing.T) {
	for _, scenario := range []struct {
		name, status, probe                       string
		successful, failed, skipped, unconfigured int
		failure                                   bool
	}{
		{name: "empty probe status", status: "unlocked", failed: 1, failure: true},
		{name: "unknown marked success", status: "unknown", probe: "success", failed: 1, failure: true},
		{name: "unlocked marked failure", status: "unlocked", probe: "failure", failed: 1, failure: true},
		{name: "blocked is successful observation", status: "blocked", probe: "success", successful: 1},
		{name: "partial is successful observation", status: "partial", probe: "success", successful: 1},
		{name: "unlocked is successful observation", status: "unlocked", probe: "success", successful: 1},
		{name: "skipped remains skipped", status: "unknown", probe: "skipped", skipped: 1},
		{name: "not configured remains unconfigured", status: "unknown", probe: "not_configured", unconfigured: 1},
		{name: "invalid conclusion", status: "available", probe: "success", failed: 1, failure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			rows := []agentapi.IPQualityServiceUnlockPayload{{Service: "netflix", Status: scenario.status, ProbeStatus: scenario.probe}}
			coverage := coverageFromResults(nil, rows)
			if coverage.ExpectedServiceCount != 1 || coverage.SuccessfulServiceCount != scenario.successful || coverage.FailedServiceCount != scenario.failed || coverage.SkippedServiceCount != scenario.skipped || coverage.NotConfiguredServiceCount != scenario.unconfigured {
				t.Fatalf("coverage=%#v for status=%s probe=%s", coverage, scenario.status, scenario.probe)
			}
			if failure := hasServiceProbeFailure(rows); failure != scenario.failure {
				t.Fatalf("hasServiceProbeFailure=%v want %v", failure, scenario.failure)
			}
			if rows[0].Status != scenario.status {
				t.Fatalf("coverage mutated business conclusion: %#v", rows[0])
			}
		})
	}
}

func TestEmptyServiceCollectionHasNoCoverageOrFailure(t *testing.T) {
	rows := collectDefaultServiceUnlocks(nil)
	if len(rows) != 0 {
		t.Fatalf("empty input produced diagnostics: %#v", rows)
	}
	coverage := coverageFromResults(nil, nil)
	if *coverage != (agentapi.IPQualityCoveragePayload{}) || hasServiceProbeFailure(nil) {
		t.Fatalf("empty coverage=%#v", coverage)
	}
}
