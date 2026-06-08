package agentapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func intPtr(v int) *int {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func TestSyncRequestRoundTripWithObservations(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:00:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.SyncRequest{
		MonitoringInstanceID: "mi_001",
		SyncToken:            "sync-token",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "v1.2.3",
			Fingerprint:  "fp_001",
			SyncBatchID:  "batch_001",
		}},
		HostSamples: []agentapi.HostSamplePayload{{
			ObservedAt:           observedAt,
			AgentVersion:         "v1.2.3",
			Fingerprint:          "fp_001",
			SyncBatchID:          "batch_001",
			CPUUsagePct:          12.5,
			Load1:                0.22,
			Load5:                0.18,
			Load15:               0.10,
			MemUsedPct:           61.3,
			MemAvailableBytes:    1073741824,
			MemTotalBytes:        8589934592,
			SwapUsedPct:          5.1,
			DiskUsedPct:          71.2,
			DiskTotalBytes:       107374182400,
			InodeUsedPct:         44.8,
			NetInBytesPerSec:     1200,
			NetOutBytesPerSec:    2400,
			CPUIOWaitPct:         1.2,
			CPUStealPct:          0.4,
			DiskReadBytesPerSec:  800,
			DiskWriteBytesPerSec: 1600,
			DiskBusyPct:          9.7,
			UptimeSeconds:        7200,
			MaintenanceContext:   true,
		}},
		ProbeObservations: []agentapi.ProbeObservationPayload{{
			TargetID:      "tg_001",
			ProbeItemID:   "pi_001",
			ProbeKind:     agentapi.ProbeKindHTTP,
			ObservedAt:    observedAt,
			AgentVersion:  "v1.2.3",
			Fingerprint:   "fp_001",
			SyncBatchID:   "batch_001",
			ResultKind:    agentapi.ProbeResultSuccess,
			LatencyMS:     intPtr(83),
			HTTPStatus:    intPtr(200),
			TLSExpiryDays: intPtr(30),
		}},
		IPQualityReports: []agentapi.IPQualityReportPayload{{
			ObservedAt:    observedAt,
			AgentVersion:  "v1.2.3",
			Fingerprint:   "fp_001",
			SyncBatchID:   "batch_001",
			IPAddress:     "203.0.113.10",
			IPVersion:     4,
			Status:        agentapi.IPQualityStatusSuccess,
			ASN:           "AS64500",
			Organization:  "Example Network",
			UseRegionCode: "US",
			RiskLevel:     "low",
			RawJSON:       json.RawMessage(`{"Info":{"ASN":"AS64500"}}`),
			ProviderResults: []agentapi.IPQualityProviderResultPayload{{
				Provider:    "ipinfo",
				UsageType:   "hosting",
				CompanyType: "hosting",
				RegionCode:  "US",
				IsServer:    boolPtr(true),
				IsVPN:       boolPtr(false),
			}},
			ServiceUnlocks: []agentapi.IPQualityServiceUnlockPayload{{
				Service:    "netflix",
				Status:     "unlocked",
				Region:     "US",
				UnlockType: "full",
			}},
		}},
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var roundTrip agentapi.SyncRequest
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal sync request: %v", err)
	}

	if len(roundTrip.HostSamples) != 1 {
		t.Fatalf("len(HostSamples) = %d, want %d", len(roundTrip.HostSamples), 1)
	}

	if len(roundTrip.ProbeObservations) != 1 {
		t.Fatalf("len(ProbeObservations) = %d, want %d", len(roundTrip.ProbeObservations), 1)
	}
	if len(roundTrip.IPQualityReports) != 1 {
		t.Fatalf("len(IPQualityReports) = %d, want %d", len(roundTrip.IPQualityReports), 1)
	}

	if roundTrip.ProbeObservations[0].TargetID != "tg_001" {
		t.Fatalf("ProbeObservations[0].TargetID = %q, want %q", roundTrip.ProbeObservations[0].TargetID, "tg_001")
	}
	if roundTrip.IPQualityReports[0].ProviderResults[0].Provider != "ipinfo" {
		t.Fatalf("Provider = %q, want ipinfo", roundTrip.IPQualityReports[0].ProviderResults[0].Provider)
	}
	if roundTrip.IPQualityReports[0].ServiceUnlocks[0].Service != "netflix" {
		t.Fatalf("Service = %q, want netflix", roundTrip.IPQualityReports[0].ServiceUnlocks[0].Service)
	}
}

func TestSyncRequestOmitsObservationAdjunctsWithoutHeartbeatCarrier(t *testing.T) {
	payload, err := json.Marshal(agentapi.SyncRequest{MonitoringInstanceID: "mi_001", SyncToken: "sync-token"})
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if _, ok := got["heartbeats"]; ok {
		t.Fatalf("payload unexpectedly included heartbeats: %s", payload)
	}
	if _, ok := got["host_samples"]; ok {
		t.Fatalf("payload unexpectedly included host_samples: %s", payload)
	}
	if _, ok := got["probe_observations"]; ok {
		t.Fatalf("payload unexpectedly included probe_observations: %s", payload)
	}
	if _, ok := got["ip_quality_reports"]; ok {
		t.Fatalf("payload unexpectedly included ip_quality_reports: %s", payload)
	}
}

func TestProbeObservationPayloadRoundTripPreservesSuccessSemantics(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:05:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    agentapi.ProbeKindHTTP,
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultSuccess,
		LatencyMS:    intPtr(83),
		HTTPStatus:   intPtr(200),
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal probe observation: %v", err)
	}

	var roundTrip agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal probe observation: %v", err)
	}

	if roundTrip.ProbeKind != agentapi.ProbeKindHTTP {
		t.Fatalf("ProbeKind = %q, want %q", roundTrip.ProbeKind, agentapi.ProbeKindHTTP)
	}
	if roundTrip.ResultKind != agentapi.ProbeResultSuccess {
		t.Fatalf("ResultKind = %q, want %q", roundTrip.ResultKind, agentapi.ProbeResultSuccess)
	}
	if roundTrip.LatencyMS == nil {
		t.Fatalf("LatencyMS = nil, want non-nil")
	}
	if *roundTrip.LatencyMS != 83 {
		t.Fatalf("*LatencyMS = %d, want 83", *roundTrip.LatencyMS)
	}
	if roundTrip.HTTPStatus == nil {
		t.Fatalf("HTTPStatus = nil, want non-nil")
	}
	if *roundTrip.HTTPStatus != 200 {
		t.Fatalf("*HTTPStatus = %d, want 200", *roundTrip.HTTPStatus)
	}
}

func TestProbeObservationPayloadRoundTripDistinguishesZeroLatencyAndHTTPStatusFromMissing(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:06:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	withZero := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    agentapi.ProbeKindHTTP,
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultSuccess,
		LatencyMS:    intPtr(0),
		HTTPStatus:   intPtr(0),
	}

	payloadWithZero, err := json.Marshal(withZero)
	if err != nil {
		t.Fatalf("marshal probe observation with zero latency/http status: %v", err)
	}

	var zeroFields map[string]any
	if err := json.Unmarshal(payloadWithZero, &zeroFields); err != nil {
		t.Fatalf("unmarshal payload with zero latency/http status into map: %v", err)
	}
	if got, ok := zeroFields["latency_ms"]; !ok || got.(float64) != 0 {
		t.Fatalf("latency_ms = %#v, present=%v, want present zero", got, ok)
	}
	if got, ok := zeroFields["http_status"]; !ok || got.(float64) != 0 {
		t.Fatalf("http_status = %#v, present=%v, want present zero", got, ok)
	}

	var roundTripWithZero agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payloadWithZero, &roundTripWithZero); err != nil {
		t.Fatalf("unmarshal payload with zero latency/http status: %v", err)
	}
	if roundTripWithZero.LatencyMS == nil {
		t.Fatalf("LatencyMS = nil, want pointer to zero")
	}
	if *roundTripWithZero.LatencyMS != 0 {
		t.Fatalf("*LatencyMS = %d, want 0", *roundTripWithZero.LatencyMS)
	}
	if roundTripWithZero.HTTPStatus == nil {
		t.Fatalf("HTTPStatus = nil, want pointer to zero")
	}
	if *roundTripWithZero.HTTPStatus != 0 {
		t.Fatalf("*HTTPStatus = %d, want 0", *roundTripWithZero.HTTPStatus)
	}

	missing := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    agentapi.ProbeKindHTTP,
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultSuccess,
	}

	payloadMissing, err := json.Marshal(missing)
	if err != nil {
		t.Fatalf("marshal probe observation without latency/http status: %v", err)
	}

	var missingFields map[string]any
	if err := json.Unmarshal(payloadMissing, &missingFields); err != nil {
		t.Fatalf("unmarshal payload without latency/http status into map: %v", err)
	}
	if _, ok := missingFields["latency_ms"]; ok {
		t.Fatalf("latency_ms unexpectedly present in missing payload: %s", payloadMissing)
	}
	if _, ok := missingFields["http_status"]; ok {
		t.Fatalf("http_status unexpectedly present in missing payload: %s", payloadMissing)
	}

	var roundTripMissing agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payloadMissing, &roundTripMissing); err != nil {
		t.Fatalf("unmarshal payload without latency/http status: %v", err)
	}
	if roundTripMissing.LatencyMS != nil {
		t.Fatalf("LatencyMS = %v, want nil", *roundTripMissing.LatencyMS)
	}
	if roundTripMissing.HTTPStatus != nil {
		t.Fatalf("HTTPStatus = %v, want nil", *roundTripMissing.HTTPStatus)
	}
}

func TestProbeObservationPayloadRoundTripDistinguishesZeroTLSExpiryFromMissing(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:07:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	withZero := agentapi.ProbeObservationPayload{
		TargetID:      "tg_001",
		ProbeItemID:   "pi_001",
		ProbeKind:     agentapi.ProbeKindTLS,
		ObservedAt:    observedAt,
		AgentVersion:  "v1.2.3",
		Fingerprint:   "fp_001",
		SyncBatchID:   "batch_001",
		ResultKind:    agentapi.ProbeResultSuccess,
		TLSExpiryDays: intPtr(0),
	}

	payloadWithZero, err := json.Marshal(withZero)
	if err != nil {
		t.Fatalf("marshal probe observation with zero tls expiry: %v", err)
	}

	var zeroFields map[string]any
	if err := json.Unmarshal(payloadWithZero, &zeroFields); err != nil {
		t.Fatalf("unmarshal payload with zero tls expiry into map: %v", err)
	}
	if got, ok := zeroFields["tls_expiry_days"]; !ok || got.(float64) != 0 {
		t.Fatalf("tls_expiry_days = %#v, present=%v, want present zero", got, ok)
	}

	var roundTripWithZero agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payloadWithZero, &roundTripWithZero); err != nil {
		t.Fatalf("unmarshal payload with zero tls expiry: %v", err)
	}
	if roundTripWithZero.TLSExpiryDays == nil {
		t.Fatalf("TLSExpiryDays = nil, want pointer to zero")
	}
	if *roundTripWithZero.TLSExpiryDays != 0 {
		t.Fatalf("*TLSExpiryDays = %d, want 0", *roundTripWithZero.TLSExpiryDays)
	}

	missing := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    agentapi.ProbeKindTLS,
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultSuccess,
	}

	payloadMissing, err := json.Marshal(missing)
	if err != nil {
		t.Fatalf("marshal probe observation without tls expiry: %v", err)
	}

	var missingFields map[string]any
	if err := json.Unmarshal(payloadMissing, &missingFields); err != nil {
		t.Fatalf("unmarshal payload without tls expiry into map: %v", err)
	}
	if _, ok := missingFields["tls_expiry_days"]; ok {
		t.Fatalf("tls_expiry_days unexpectedly present in missing payload: %s", payloadMissing)
	}

	var roundTripMissing agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payloadMissing, &roundTripMissing); err != nil {
		t.Fatalf("unmarshal payload without tls expiry: %v", err)
	}
	if roundTripMissing.TLSExpiryDays != nil {
		t.Fatalf("TLSExpiryDays = %v, want nil", *roundTripMissing.TLSExpiryDays)
	}
}

func TestProbeObservationPayloadRoundTripPreservesFailureErrorCode(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-24T10:10:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.ProbeObservationPayload{
		TargetID:     "tg_001",
		ProbeItemID:  "pi_001",
		ProbeKind:    agentapi.ProbeKindTCP,
		ObservedAt:   observedAt,
		AgentVersion: "v1.2.3",
		Fingerprint:  "fp_001",
		SyncBatchID:  "batch_001",
		ResultKind:   agentapi.ProbeResultFailure,
		ErrorCode:    agentapi.ProbeErrorTimeout,
		ErrorSummary: "timeout waiting for dial",
		IsBackfilled: true,
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal probe observation: %v", err)
	}

	var roundTrip agentapi.ProbeObservationPayload
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal probe observation: %v", err)
	}

	if roundTrip.ResultKind != agentapi.ProbeResultFailure {
		t.Fatalf("ResultKind = %q, want %q", roundTrip.ResultKind, agentapi.ProbeResultFailure)
	}
	if roundTrip.ErrorCode != agentapi.ProbeErrorTimeout {
		t.Fatalf("ErrorCode = %q, want %q", roundTrip.ErrorCode, agentapi.ProbeErrorTimeout)
	}
}
