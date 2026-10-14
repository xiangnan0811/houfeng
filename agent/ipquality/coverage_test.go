package ipquality

import (
	"context"
	"net/http"
	"strings"
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
	rows := NewHTTPCollector(HTTPCollectorOptions{}).collectDefaultServiceUnlocks(context.Background(), nil, 4)
	if len(rows) != 0 {
		t.Fatalf("empty input produced diagnostics: %#v", rows)
	}
	coverage := coverageFromResults(nil, nil)
	if *coverage != (agentapi.IPQualityCoveragePayload{}) || hasServiceProbeFailure(nil) {
		t.Fatalf("empty coverage=%#v", coverage)
	}
}

func TestDefaultHTTPCollectorUsesIndependentDirectFamilyTransports(t *testing.T) {
	collector := NewHTTPCollector(HTTPCollectorOptions{})
	clients := []*http.Client{collector.client, collector.serviceHTTPClient(4), collector.serviceHTTPClient(6)}
	transports := make(map[*http.Transport]bool, len(clients))
	for _, client := range clients {
		transport, ok := client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil || transport == http.DefaultTransport {
			t.Fatalf("transport=%#v, want independent direct standard transport", client.Transport)
		}
		if transports[transport] {
			t.Fatal("database and address-family clients must not share transports")
		}
		transports[transport] = true
	}
	for _, scenario := range []struct {
		family  int
		address string
	}{
		{family: 4, address: "[::1]:1"},
		{family: 6, address: "127.0.0.1:1"},
	} {
		transport := collector.serviceHTTPClient(scenario.family).Transport.(*http.Transport)
		connection, err := transport.DialContext(context.Background(), "tcp", scenario.address)
		if connection != nil {
			_ = connection.Close()
			t.Fatalf("family %d dialed the opposite address family", scenario.family)
		}
		if err == nil || !strings.Contains(err.Error(), "no suitable address") {
			t.Fatalf("family %d error=%v, want rejected opposite-family address before connection", scenario.family, err)
		}
	}
}

func TestHTTPCollectorPreservesInjectedAndLegacyClients(t *testing.T) {
	injected := &http.Client{Timeout: 123}
	for _, lookupURL := range []string{"", "https://custom.example/lookup"} {
		collector := NewHTTPCollector(HTTPCollectorOptions{Client: injected, LookupURL: lookupURL})
		if collector.client != injected || collector.serviceHTTPClient(4) != injected || collector.serviceHTTPClient(6) != injected {
			t.Fatal("explicit client must remain the request entry point for every address family")
		}
		cloned := collector.WithMetadata("version", "fingerprint", "batch")
		if cloned.client != injected || cloned.serviceHTTPClient(4) != injected || cloned.serviceHTTPClient(6) != injected {
			t.Fatal("metadata cloning must preserve client instances")
		}
	}
	legacy := NewHTTPCollector(HTTPCollectorOptions{LookupURL: "https://custom.example/lookup"})
	if legacy.client != http.DefaultClient || legacy.serviceHTTPClient(4) != http.DefaultClient || legacy.serviceHTTPClient(6) != http.DefaultClient {
		t.Fatal("explicit legacy lookup must preserve the original default-client semantics")
	}
}

func TestDefaultServicePreservesUnsupportedAndCancelledOutcomes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	collector := NewHTTPCollector(HTTPCollectorOptions{})
	outcomes := collector.collectDefaultServiceUnlocks(ctx, []string{"netflix", "not-a-service"}, 6)
	if outcomes[0].Result.ProbeStatus != sourceStatusFailure || outcomes[0].Result.Status != "unknown" ||
		outcomes[0].Result.Region != "" || outcomes[0].Result.UnlockType != "" {
		t.Fatalf("cancelled service=%#v, want failure without business facts", outcomes[0])
	}
	if outcomes[1].Result.ProbeStatus != sourceStatusSkipped || outcomes[1].Result.ErrorCode != "unsupported_service" {
		t.Fatalf("unsupported service=%#v", outcomes[1])
	}
}
