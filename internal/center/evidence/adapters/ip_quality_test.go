package adapters

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

var _ evidence.Kind = (*IPQualityAdapter)(nil)

func TestIPQualityCaptureFreezesStalePolicyAndExcludesRawDiagnostics(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-2 * time.Hour)
	latency := 25
	proxy := true
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: IPQualityEvidenceReport{
			ReportID:             "ipq_0123456789abcdef",
			ObservedAt:           observedAt,
			ReceivedAt:           observedAt.Add(time.Minute),
			AgentVersion:         "agent/v1",
			IPAddress:            "203.0.113.10",
			IPVersion:            4,
			Status:               "success",
			ASN:                  "AS64500",
			Organization:         "Example Transit",
			UseRegionCode:        "JP",
			UseRegionName:        "Tokyo",
			RegisteredRegionCode: "US",
			RegisteredRegionName: "California",
			RiskLevel:            "medium",
			StaleAfter:           time.Hour,
			Coverage: IPQualityCoverage{
				ExpectedProviderCount:   1,
				SuccessfulProviderCount: 1,
				ExpectedServiceCount:    1,
				SuccessfulServiceCount:  1,
			},
			Providers: []IPQualityProviderEvidence{{
				Provider: "ipapi.is", Status: "success", SourceType: "default", LatencyMS: &latency,
				RiskLevel: "medium", IsProxy: &proxy, RegionCode: "JP", RegionName: "Tokyo",
			}},
			Services: []IPQualityServiceEvidence{{
				Service: "netflix", Source: "builtin", Status: "unlocked", ProbeStatus: "success", Region: "JP",
			}},
		}},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{
			Clock:       func() time.Time { return now },
			NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil },
		},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key:             evidence.IPQualityReportV1Key(),
		SourceType:      string(recordauth.SourceKindVPS),
		SourceID:        "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: observedAt.Add(-time.Hour), End: now},
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.Quality.Status != evidence.QualityDegraded || preview.Quality.Partial || preview.Quality.SampleCount != 1 {
		t.Fatalf("preview Quality = %#v, want stale degraded point report", preview.Quality)
	}
	if preview.Sensitivity != evidence.SensitivityNormal {
		t.Fatalf("preview Sensitivity = %q, want normal without explicit topology selection", preview.Sensitivity)
	}

	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID:            preview.IntentID,
		Key:           selection.Key,
		Selection:     selection,
		PreviewDigest: sha256.Sum256([]byte("preview")),
		ValidUntil:    preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	canonical := snapshot.Bytes()
	for _, forbidden := range []string{
		"raw_json", "diagnostics_json", "extra_json", "fingerprint", "stdout", "stderr", "arbitrary raw payload",
		"203.0.113.10", "AS64500", "Example Transit", "Tokyo", "California",
	} {
		if bytes.Contains(canonical, []byte(forbidden)) {
			t.Fatalf("canonical payload contains forbidden/unselected value %q: %s", forbidden, canonical)
		}
	}
	for _, required := range []string{`"stale":true`, `"stale_after_seconds":3600`, `"ip_version":4`, `"report_id":"ipq_0123456789abcdef"`} {
		if !strings.Contains(string(canonical), required) {
			t.Fatalf("canonical payload = %s, want %s", canonical, required)
		}
	}
}

func TestIPQualityAdapterRejectsFailureReportsFromUserVisibleEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: validIPQualityTestReport(now.Add(-time.Hour))},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	adapter.source = staticIPQualitySource{report: func() IPQualityEvidenceReport {
		report := validIPQualityTestReport(now.Add(-time.Hour))
		report.Status = "failure"
		return report
	}()}

	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key:             evidence.IPQualityReportV1Key(),
		SourceType:      string(recordauth.SourceKindVPS),
		SourceID:        "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
	})
	if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
		t.Fatalf("PreviewCapture(failure report) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestIPQualityAdapterRejectsMalformedCustomSourceFacts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*IPQualityEvidenceReport)
	}{
		{name: "empty address", mutate: func(report *IPQualityEvidenceReport) { report.IPAddress = "" }},
		{name: "unspecified address", mutate: func(report *IPQualityEvidenceReport) { report.IPAddress = "0.0.0.0" }},
		{name: "address version mismatch", mutate: func(report *IPQualityEvidenceReport) { report.IPVersion = 6 }},
		{name: "sub-microsecond observation", mutate: func(report *IPQualityEvidenceReport) { report.ObservedAt = report.ObservedAt.Add(time.Nanosecond) }},
		{name: "sub-microsecond receipt", mutate: func(report *IPQualityEvidenceReport) { report.ReceivedAt = report.ReceivedAt.Add(time.Nanosecond) }},
		{name: "future receipt", mutate: func(report *IPQualityEvidenceReport) { report.ReceivedAt = now.Add(time.Microsecond) }},
		{name: "invalid provider status", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedProviderCount: 1, SuccessfulProviderCount: 1}
			report.Providers = []IPQualityProviderEvidence{{Provider: "custom", Status: "unknown", SourceType: "custom"}}
		}},
		{name: "non-canonical provider status", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedProviderCount: 1, SuccessfulProviderCount: 1}
			report.Providers = []IPQualityProviderEvidence{{Provider: "custom", Status: " success ", SourceType: "custom"}}
		}},
		{name: "non-canonical provider source type", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedProviderCount: 1, SuccessfulProviderCount: 1}
			report.Providers = []IPQualityProviderEvidence{{Provider: "custom", Status: "success", SourceType: " custom "}}
		}},
		{name: "invalid service probe status", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedServiceCount: 1, SuccessfulServiceCount: 1}
			report.Services = []IPQualityServiceEvidence{{Service: "netflix", Status: "unlocked", ProbeStatus: "unknown"}}
		}},
		{name: "invalid service status", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedServiceCount: 1, SuccessfulServiceCount: 1}
			report.Services = []IPQualityServiceEvidence{{Service: "netflix", Status: "maybe", ProbeStatus: "success"}}
		}},
		{name: "non-canonical assignment mode", mutate: func(report *IPQualityEvidenceReport) { report.AssignmentMode = " link " }},
		{name: "provider coverage does not match rows", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedProviderCount: 1, SuccessfulProviderCount: 1}
		}},
		{name: "service coverage does not match row status", mutate: func(report *IPQualityEvidenceReport) {
			report.Coverage = IPQualityCoverage{ExpectedServiceCount: 1, SuccessfulServiceCount: 1}
			report.Services = []IPQualityServiceEvidence{{Service: "netflix", Status: "unknown", ProbeStatus: "failure"}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := validIPQualityTestReport(now.Add(-time.Hour))
			tt.mutate(&report)
			adapter, err := NewIPQualityAdapter(
				staticIPQualitySource{report: report},
				monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
				AdapterOptions{Clock: func() time.Time { return now }},
			)
			if err != nil {
				t.Fatalf("NewIPQualityAdapter() error = %v", err)
			}
			_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
				RequestedWindow: evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
			})
			if !errors.Is(err, evidence.ErrInvalidCanonicalPayload) {
				t.Fatalf("PreviewCapture() error = %v, want ErrInvalidCanonicalPayload", err)
			}
		})
	}
}

func TestIPQualityAdapterCanonicalizesCustomSourceRowOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	report := validIPQualityTestReport(now.Add(-time.Hour))
	report.Coverage = IPQualityCoverage{
		ExpectedProviderCount: 2, SuccessfulProviderCount: 2,
		ExpectedServiceCount: 2, SuccessfulServiceCount: 2,
	}
	report.Providers = []IPQualityProviderEvidence{
		{Provider: "provider-b", Status: "success", SourceType: "optional"},
		{Provider: "provider-a", Status: "success", SourceType: "default"},
	}
	report.Services = []IPQualityServiceEvidence{
		{Service: "netflix", Source: "source-b", Status: "unlocked", ProbeStatus: "success"},
		{Service: "netflix", Source: "source-a", Status: "blocked", ProbeStatus: "success"},
	}

	_, first := captureIPQualityReadModelFixture(t, now, report)
	report.Providers[0], report.Providers[1] = report.Providers[1], report.Providers[0]
	report.Services[0], report.Services[1] = report.Services[1], report.Services[0]
	_, second := captureIPQualityReadModelFixture(t, now, report)
	if first.Hash() != second.Hash() {
		t.Fatalf("canonical hashes differ for equivalent source row order: %x != %x", first.Hash(), second.Hash())
	}
}

func TestIPQualityCaptureStripsRiskAndUnlockFactsFromFailedRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	report := validIPQualityTestReport(now.Add(-time.Hour))
	truth := true
	report.Providers = []IPQualityProviderEvidence{{
		Provider: "failed-provider", Status: "failure", SourceType: "default",
		RiskLevel: "high", RiskScore: "99", RegionCode: "US", RegionName: "Secret Region",
		IsProxy: &truth, IsTor: &truth, IsVPN: &truth, IsAbuser: &truth,
	}}
	report.Services = []IPQualityServiceEvidence{{
		Service: "failed-service", Source: "builtin", Status: "blocked", ProbeStatus: "failure",
		Region: "US", UnlockType: "blocked",
	}}
	report.Coverage = IPQualityCoverage{
		ExpectedProviderCount: 1, FailedProviderCount: 1,
		ExpectedServiceCount: 1, FailedServiceCount: 1,
	}
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: report},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{
			Clock:       func() time.Time { return now },
			NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil },
		},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key:                     evidence.IPQualityReportV1Key(),
		SourceType:              string(recordauth.SourceKindVPS),
		SourceID:                "vps_0123456789abcdef",
		RequestedWindow:         evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
		SensitiveTopologyFields: []string{"providers.region_code", "providers.region_name", "services.region"},
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	canonical := snapshot.Bytes()
	for _, forbidden := range []string{`"risk_level":"high"`, `"risk_score":"99"`, `"region_code":"US"`, `"region_name":"Secret Region"`, `"region":"US"`, `"status":"blocked"`, `"unlock_type":"blocked"`, `"is_proxy":true`} {
		if bytes.Contains(canonical, []byte(forbidden)) {
			t.Fatalf("canonical payload contains non-authoritative failed-row fact %s: %s", forbidden, canonical)
		}
	}
}

func TestIPQualityStaleBoundaryAndPartialQuality(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		mutate     func(*IPQualityEvidenceReport)
		wantStatus evidence.QualityStatus
		wantStale  bool
		wantPart   bool
	}{
		{name: "exact stale boundary remains fresh", wantStatus: evidence.QualityComplete},
		{name: "partial report needs review", mutate: func(report *IPQualityEvidenceReport) { report.Status = "partial" }, wantStatus: evidence.QualityPartial, wantPart: true},
		{name: "ambiguous assignment needs review", mutate: func(report *IPQualityEvidenceReport) { report.Ambiguous = true }, wantStatus: evidence.QualityPartial, wantPart: true},
		{name: "past stale boundary is degraded", mutate: func(report *IPQualityEvidenceReport) { report.ObservedAt = report.ObservedAt.Add(-time.Microsecond) }, wantStatus: evidence.QualityDegraded, wantStale: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := validIPQualityTestReport(now.Add(-time.Hour))
			report.StaleAfter = time.Hour
			if tt.mutate != nil {
				tt.mutate(&report)
			}
			adapter, err := NewIPQualityAdapter(
				staticIPQualitySource{report: report},
				monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
				AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil }},
			)
			if err != nil {
				t.Fatalf("NewIPQualityAdapter() error = %v", err)
			}
			preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
				Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
				RequestedWindow: evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
			})
			if err != nil {
				t.Fatalf("PreviewCapture() error = %v", err)
			}
			if preview.Quality.Status != tt.wantStatus || preview.Quality.Partial != tt.wantPart {
				t.Fatalf("Quality = %#v, want status=%q partial=%t", preview.Quality, tt.wantStatus, tt.wantPart)
			}
			payload := ipQualityPayload(report, tt.wantStale, map[string]struct{}{}, false, false)
			if payload["stale"] != tt.wantStale {
				t.Fatalf("stale = %#v, want %t", payload["stale"], tt.wantStale)
			}
		})
	}
}

func TestIPQualityCoverageGapForcesPartialAndClearsRiskConclusion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	report := validIPQualityTestReport(now.Add(-time.Hour))
	report.RiskLevel = "high"
	report.Coverage = IPQualityCoverage{ExpectedProviderCount: 2, SuccessfulProviderCount: 1, FailedProviderCount: 1}
	report.Providers = []IPQualityProviderEvidence{
		{Provider: "provider-a", Status: "success", SourceType: "default"},
		{Provider: "provider-b", Status: "failure", SourceType: "optional"},
	}
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: report},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.Quality.Status != evidence.QualityPartial || !preview.Quality.Partial {
		t.Fatalf("preview quality = %#v, want partial coverage", preview.Quality)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if bytes.Contains(snapshot.Bytes(), []byte(`"risk_level":"high"`)) || !bytes.Contains(snapshot.Bytes(), []byte(`"risk_level":""`)) {
		t.Fatalf("canonical payload = %s, want incomplete report risk conclusion cleared", snapshot.Bytes())
	}
}

func TestIPQualityCaptureIncludesOnlyExplicitTopologyFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	report := validIPQualityTestReport(now.Add(-time.Hour))
	report.ASN = "AS64500"
	report.Organization = "Example Transit"
	report.UseRegionName = "Tokyo"
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: report},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now }, NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
		RequestedWindow:         evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
		SensitiveTopologyFields: []string{"ip_address"},
	}
	preview, err := adapter.PreviewCapture(context.Background(), monitoringTestActor(t), selection)
	if err != nil {
		t.Fatalf("PreviewCapture() error = %v", err)
	}
	if preview.Sensitivity != evidence.SensitivitySensitiveTopology {
		t.Fatalf("Sensitivity = %q, want sensitive_topology", preview.Sensitivity)
	}
	snapshot, err := adapter.Capture(context.Background(), monitoringTestActor(t), evidence.Intent{
		ID: preview.IntentID, Key: selection.Key, Selection: selection,
		PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: preview.ValidUntil,
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	canonical := snapshot.Bytes()
	if !bytes.Contains(canonical, []byte(`"ip_address":"203.0.113.10"`)) {
		t.Fatalf("canonical payload = %s, want explicitly selected IP address", canonical)
	}
	for _, unselected := range []string{"AS64500", "Example Transit", "Tokyo"} {
		if bytes.Contains(canonical, []byte(unselected)) {
			t.Fatalf("canonical payload contains unselected topology %q: %s", unselected, canonical)
		}
	}
}

func TestIPQualityAuthorizationRunsBeforeSourceRead(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	source := &countingIPQualitySource{report: validIPQualityTestReport(now.Add(-time.Hour))}
	adapter, err := NewIPQualityAdapter(source, staticSourceResolver{err: recordauth.ErrDenied}, AdapterOptions{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	_, err = adapter.PreviewCapture(context.Background(), monitoringTestActor(t), evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: now.Add(-2 * time.Hour), End: now},
	})
	if !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("PreviewCapture() error = %v, want ErrDenied", err)
	}
	if source.calls != 0 {
		t.Fatalf("source reads = %d, want zero before authorization", source.calls)
	}
}

func TestIPQualityAdapterConformance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	adapter, err := NewIPQualityAdapter(
		staticIPQualitySource{report: validIPQualityTestReport(now.Add(time.Hour))},
		monitoringTestResolver(t, recordauth.SourceKindVPS, "vps_0123456789abcdef"),
		AdapterOptions{Clock: func() time.Time { return now.Add(2 * time.Hour) }, NewIntentID: func() (string, error) { return "evi_89abcdef0123456701234567", nil }},
	)
	if err != nil {
		t.Fatalf("NewIPQualityAdapter() error = %v", err)
	}
	selection := evidence.Selection{
		Key: evidence.IPQualityReportV1Key(), SourceType: string(recordauth.SourceKindVPS), SourceID: "vps_0123456789abcdef",
		RequestedWindow: evidence.TimeWindow{Start: now, End: now.Add(2 * time.Hour)},
	}
	fixture := evidence.ConformanceFixture{
		Actor: monitoringTestActor(t), Selection: selection,
		Intent:    evidence.Intent{ID: "evi_89abcdef0123456701234567", Key: selection.Key, Selection: selection, PreviewDigest: sha256.Sum256([]byte("preview")), ValidUntil: now.Add(2*time.Hour + evidence.CaptureIntentTTL)},
		Alignment: evidence.Alignment{Mode: evidence.AlignmentExact}, ExportMode: evidence.ExportModeSafe,
	}
	if err := evidence.VerifyKindConformance(context.Background(), adapter, fixture); err != nil {
		t.Fatalf("VerifyKindConformance() error = %v", err)
	}
}

func validIPQualityTestReport(observedAt time.Time) IPQualityEvidenceReport {
	return IPQualityEvidenceReport{
		ReportID: "ipq_0123456789abcdef", ObservedAt: observedAt, ReceivedAt: observedAt.Add(time.Minute),
		AgentVersion: "agent/v1", IPAddress: "203.0.113.10", IPVersion: 4, Status: "success",
		RiskLevel: "low", StaleAfter: 7 * 24 * time.Hour,
	}
}

type staticIPQualitySource struct {
	report IPQualityEvidenceReport
	err    error
}

type countingIPQualitySource struct {
	report IPQualityEvidenceReport
	calls  int
}

func (source *countingIPQualitySource) LoadIPQualityEvidence(
	context.Context,
	string,
	evidence.TimeWindow,
) (IPQualityEvidenceReport, error) {
	source.calls++
	return source.report, nil
}

func (source staticIPQualitySource) LoadIPQualityEvidence(
	context.Context,
	string,
	evidence.TimeWindow,
) (IPQualityEvidenceReport, error) {
	return source.report, source.err
}
