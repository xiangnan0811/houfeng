package adapters

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
)

const ipQualityCalculationVersion = "ip-quality-evidence/v1"

type IPQualityEvidenceSource interface {
	LoadIPQualityEvidence(context.Context, string, evidence.TimeWindow) (IPQualityEvidenceReport, error)
}

type IPQualityEvidenceReport struct {
	ReportID             string
	ObservedAt           time.Time
	ReceivedAt           time.Time
	AgentVersion         string
	IPAddress            string
	IPVersion            int
	Status               string
	ASN                  string
	Organization         string
	Latitude             *float64
	Longitude            *float64
	UseRegionCode        string
	UseRegionName        string
	RegisteredRegionCode string
	RegisteredRegionName string
	RiskLevel            string
	IsBackfilled         bool
	Ambiguous            bool
	AssignmentMode       string
	StaleAfter           time.Duration
	Coverage             IPQualityCoverage
	Providers            []IPQualityProviderEvidence
	Services             []IPQualityServiceEvidence
}

type IPQualityCoverage struct {
	ExpectedProviderCount      int
	SuccessfulProviderCount    int
	FailedProviderCount        int
	SkippedProviderCount       int
	NotConfiguredProviderCount int
	ExpectedServiceCount       int
	SuccessfulServiceCount     int
	FailedServiceCount         int
	SkippedServiceCount        int
	NotConfiguredServiceCount  int
}

type IPQualityProviderEvidence struct {
	Provider    string
	Status      string
	SourceType  string
	LatencyMS   *int
	UsageType   string
	CompanyType string
	RiskLevel   string
	RiskScore   string
	RegionCode  string
	RegionName  string
	IsProxy     *bool
	IsTor       *bool
	IsVPN       *bool
	IsServer    *bool
	IsAbuser    *bool
	IsRobot     *bool
	ErrorCode   string
}

type IPQualityServiceEvidence struct {
	Service     string
	Source      string
	Status      string
	ProbeStatus string
	LatencyMS   *int
	Region      string
	UnlockType  string
	ErrorCode   string
}

type IPQualityAdapter struct {
	source     IPQualityEvidenceSource
	resolver   EvidenceSourceResolver
	options    AdapterOptions
	descriptor evidence.Descriptor
}

func NewIPQualityAdapter(
	source IPQualityEvidenceSource,
	resolver EvidenceSourceResolver,
	options AdapterOptions,
) (*IPQualityAdapter, error) {
	descriptor := ipQualityDescriptor()
	if source == nil || resolver == nil {
		return nil, fmt.Errorf("%w: nil IP quality adapter dependency", evidence.ErrInvalidKindDescriptor)
	}
	if err := descriptor.Validate(); err != nil {
		return nil, err
	}
	return &IPQualityAdapter{source: source, resolver: resolver, options: options, descriptor: descriptor}, nil
}

func (adapter *IPQualityAdapter) Descriptor() evidence.Descriptor {
	return adapter.descriptor
}

func (adapter *IPQualityAdapter) ValidateSelection(_ context.Context, _ evidence.ActorScope, selection evidence.Selection) error {
	if selection.Key != evidence.IPQualityReportV1Key() ||
		selection.SourceType != string(recordauth.SourceKindVPS) || selection.SourceID == "" ||
		!validEvidenceWindow(selection.RequestedWindow) || len(selection.Metrics) != 0 || selection.Precision != 0 {
		return fmt.Errorf("%w: IP quality selection", evidence.ErrInvalidCanonicalPayload)
	}
	if !validTopologySelection(adapter.descriptor, selection.SensitiveTopologyFields) {
		return fmt.Errorf("%w: IP quality topology selection", evidence.ErrInvalidCanonicalPayload)
	}
	return nil
}

func (adapter *IPQualityAdapter) PreviewCapture(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evidence.Preview, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.Preview{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, selection)
	if err != nil {
		return evidence.Preview{}, err
	}
	intentID, err := newAdapterIntentID(adapter.options)
	if err != nil {
		return evidence.Preview{}, err
	}
	now := adapterNow(adapter.options)
	return evidence.Preview{
		IntentID:                intentID,
		Key:                     adapter.descriptor.Key,
		Selection:               selection,
		Subject:                 evaluated.resolved.Subject,
		Source:                  evaluated.resolved.Source,
		RequestedWindow:         selection.RequestedWindow,
		ActualWindow:            evaluated.actualWindow,
		ObservedAt:              evaluated.report.ObservedAt.UTC(),
		SourceRevision:          evaluated.report.ReportID,
		SourceWatermark:         evaluated.report.ReceivedAt.UTC().Format(time.RFC3339Nano),
		ProducerVersion:         evaluated.report.AgentVersion,
		CalculationVersion:      ipQualityCalculationVersion,
		Units:                   evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "normalized IP quality facts"},
		Quality:                 evaluated.quality,
		Sensitivity:             evaluated.sensitivity,
		ActualPrecision:         evidence.DurationSemantics{Applicable: false, Reason: "point-in-time report"},
		BucketWidth:             evidence.DurationSemantics{Applicable: false, Reason: "point-in-time report"},
		QuotaOutcome:            evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:               immutableEvidenceRetention(),
		Redaction:               evaluated.previewRedaction,
		EstimatedCanonicalBytes: evaluated.canonical.Size(),
		SourceDigest:            evaluated.canonical.Hash(),
		RendererVersion:         adapter.descriptor.Conformance.RendererVersion,
		PreviewedAt:             now,
		ValidUntil:              now.Add(evidence.CaptureIntentTTL),
	}, nil
}

func (adapter *IPQualityAdapter) Capture(
	ctx context.Context,
	actor evidence.ActorScope,
	intent evidence.Intent,
) (evidence.CanonicalSnapshot, error) {
	if intent.Key != adapter.descriptor.Key || !evidence.ValidCaptureIntentID(intent.ID) ||
		intent.PreviewDigest == [32]byte{} || intent.ValidUntil.IsZero() {
		return evidence.CanonicalSnapshot{}, fmt.Errorf("%w: IP quality intent", evidence.ErrInvalidCanonicalPayload)
	}
	if err := adapter.ValidateSelection(ctx, actor, intent.Selection); err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	evaluated, err := adapter.evaluate(ctx, actor, intent.Selection)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	captureRedaction, err := evidence.NormalizeCaptureRedaction(adapter.descriptor, evaluated.previewRedaction)
	if err != nil {
		return evidence.CanonicalSnapshot{}, err
	}
	now := adapterNow(adapter.options)
	envelope := evidence.SnapshotEnvelope{
		Key:                adapter.descriptor.Key,
		Subject:            evaluated.resolved.Subject,
		Source:             evaluated.resolved.Source,
		Authorization:      evaluated.resolved.Authorization,
		RequestedWindow:    intent.Selection.RequestedWindow,
		ActualWindow:       evaluated.actualWindow,
		ObservedAt:         evaluated.report.ObservedAt.UTC(),
		CapturedAt:         now,
		ReferencedAt:       now,
		SourceRevision:     evaluated.report.ReportID,
		SourceWatermark:    evaluated.report.ReceivedAt.UTC().Format(time.RFC3339Nano),
		SourceDigest:       evaluated.canonical.Hash(),
		ProducerVersion:    evaluated.report.AgentVersion,
		CalculationVersion: ipQualityCalculationVersion,
		Units:              evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "normalized IP quality facts"},
		Quality:            evaluated.quality,
		Sensitivity:        evaluated.sensitivity,
		ActualPrecision:    evidence.DurationSemantics{Applicable: false, Reason: "point-in-time report"},
		BucketWidth:        evidence.DurationSemantics{Applicable: false, Reason: "point-in-time report"},
		QuotaOutcome:       evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention:          immutableEvidenceRetention(),
		Redaction:          captureRedaction,
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(adapter.descriptor, envelope, evaluated.payload, evaluated.mode)
	return snapshot, err
}

func (adapter *IPQualityAdapter) Authorize(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evidence.AuthorizationScope, error) {
	if err := adapter.ValidateSelection(ctx, actor, selection); err != nil {
		return evidence.AuthorizationScope{}, err
	}
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evidence.AuthorizationScope{}, err
	}
	return resolved.Authorization, nil
}

func (adapter *IPQualityAdapter) Summarize(snapshot evidence.CanonicalSnapshot) evidence.Summary {
	return summarizeIPQualitySnapshot(adapter.descriptor, snapshot)
}

func (adapter *IPQualityAdapter) Compare(left, right evidence.CanonicalSnapshot, alignment evidence.Alignment) evidence.Comparison {
	return compareIPQualitySnapshots(adapter.descriptor, left, right, alignment)
}

func (adapter *IPQualityAdapter) Export(snapshot evidence.CanonicalSnapshot, mode evidence.ExportMode) evidence.ExportMaterial {
	return exportEvidenceSnapshot(adapter.descriptor, snapshot, mode)
}

type evaluatedIPQuality struct {
	report           IPQualityEvidenceReport
	resolved         ResolvedEvidenceSource
	payload          map[string]any
	canonical        evidence.CanonicalPayload
	previewRedaction []evidence.FieldDecision
	actualWindow     evidence.TimeWindow
	quality          evidence.Quality
	sensitivity      evidence.Sensitivity
	mode             evidence.RedactionMode
}

func (adapter *IPQualityAdapter) evaluate(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (evaluatedIPQuality, error) {
	resolved, err := resolveEvidenceSource(ctx, adapter.resolver, actor, selection)
	if err != nil {
		return evaluatedIPQuality{}, err
	}
	report, err := adapter.source.LoadIPQualityEvidence(ctx, selection.SourceID, selection.RequestedWindow)
	if err != nil {
		return evaluatedIPQuality{}, err
	}
	report = normalizeIPQualityEvidenceReport(report)
	now := adapterNow(adapter.options)
	address, addressErr := netip.ParseAddr(report.IPAddress)
	validAddressVersion := addressErr == nil && !address.IsUnspecified() &&
		((report.IPVersion == 4 && address.Is4()) || (report.IPVersion == 6 && address.Is6() && !address.Is4In6()))
	observedAt := report.ObservedAt.UTC()
	pointEnd := observedAt.Add(time.Microsecond)
	if report.ReportID == "" || report.AgentVersion == "" || report.StaleAfter <= 0 || report.ReceivedAt.IsZero() ||
		report.StaleAfter%time.Second != 0 || !validAddressVersion ||
		!postgresTimestampRepresentable(report.ObservedAt) || !postgresTimestampRepresentable(report.ReceivedAt) ||
		report.ReceivedAt.Before(report.ObservedAt) || report.ReceivedAt.After(now) ||
		!validIPQualityCoverage(report.Coverage) || !validIPQualityRows(report) ||
		uint64(1+len(report.Providers)+len(report.Services)) > evidence.MaxSnapshotDataPoints ||
		(report.Status != "success" && report.Status != "partial") ||
		observedAt.Before(selection.RequestedWindow.Start) || !observedAt.Before(selection.RequestedWindow.End) ||
		pointEnd.After(selection.RequestedWindow.End) || now.Before(observedAt) {
		return evaluatedIPQuality{}, fmt.Errorf("%w: IP quality source", evidence.ErrInvalidCanonicalPayload)
	}
	selected := make(map[string]struct{}, len(selection.SensitiveTopologyFields))
	for _, path := range selection.SensitiveTopologyFields {
		selected[path] = struct{}{}
	}
	includeTopology := len(selected) > 0
	mode := evidence.RedactionNormalOnly
	if includeTopology {
		mode = evidence.RedactionIncludeSensitiveTopology
	}
	stale := now.Sub(observedAt) > report.StaleAfter
	coverageIncomplete := ipQualityCoverageIncomplete(report.Coverage)
	payload := ipQualityPayload(report, stale, selected, !includeTopology, coverageIncomplete)
	canonical, redaction, err := evidence.CanonicalizePayload(adapter.descriptor, payload, mode)
	if err != nil {
		return evaluatedIPQuality{}, err
	}
	redaction.Decisions = appendForbiddenPreviewDecisions(adapter.descriptor, redaction.Decisions)
	quality := evidence.Quality{
		Status:          evidence.QualityComplete,
		SampleCount:     1,
		BackfilledCount: boolCount(report.IsBackfilled),
		BucketCount:     1,
		DataPointCount:  uint64(1 + len(report.Providers) + len(report.Services)),
	}
	if report.Status == "partial" || report.Ambiguous || coverageIncomplete {
		quality.Status = evidence.QualityPartial
		quality.Partial = true
	} else if stale {
		quality.Status = evidence.QualityDegraded
	}
	sensitivity := evidence.SensitivityNormal
	if includeTopology {
		sensitivity = evidence.SensitivitySensitiveTopology
	}
	return evaluatedIPQuality{
		report: report, resolved: resolved, payload: payload, canonical: canonical,
		previewRedaction: redaction.Decisions,
		actualWindow:     evidence.TimeWindow{Start: observedAt, End: pointEnd},
		quality:          quality, sensitivity: sensitivity, mode: mode,
	}, nil
}

func ipQualityPayload(report IPQualityEvidenceReport, stale bool, selected map[string]struct{}, includeAllTopology, coverageIncomplete bool) map[string]any {
	riskLevel := report.RiskLevel
	if report.Status != "success" || report.Ambiguous || coverageIncomplete {
		riskLevel = ""
	}
	payload := map[string]any{
		"report_id":           report.ReportID,
		"observed_at":         report.ObservedAt.UTC().Format(time.RFC3339Nano),
		"received_at":         report.ReceivedAt.UTC().Format(time.RFC3339Nano),
		"ip_version":          report.IPVersion,
		"status":              report.Status,
		"stale_after_seconds": int64(report.StaleAfter / time.Second),
		"is_backfilled":       report.IsBackfilled,
		"ambiguous":           report.Ambiguous,
		"risk_level":          riskLevel,
		"coverage": map[string]any{
			"expected_provider_count":       report.Coverage.ExpectedProviderCount,
			"successful_provider_count":     report.Coverage.SuccessfulProviderCount,
			"failed_provider_count":         report.Coverage.FailedProviderCount,
			"skipped_provider_count":        report.Coverage.SkippedProviderCount,
			"not_configured_provider_count": report.Coverage.NotConfiguredProviderCount,
			"expected_service_count":        report.Coverage.ExpectedServiceCount,
			"successful_service_count":      report.Coverage.SuccessfulServiceCount,
			"failed_service_count":          report.Coverage.FailedServiceCount,
			"skipped_service_count":         report.Coverage.SkippedServiceCount,
			"not_configured_service_count":  report.Coverage.NotConfiguredServiceCount,
		},
	}
	payload["stale"] = stale
	addTopology := func(path string, value any) {
		if includeAllTopology {
			payload[path] = value
			return
		}
		if _, ok := selected[path]; ok {
			payload[path] = value
		}
	}
	addTopology("ip_address", report.IPAddress)
	addTopology("asn", report.ASN)
	addTopology("organization", report.Organization)
	addTopology("latitude", report.Latitude)
	addTopology("longitude", report.Longitude)
	addTopology("use_region_code", report.UseRegionCode)
	addTopology("use_region_name", report.UseRegionName)
	addTopology("registered_region_code", report.RegisteredRegionCode)
	addTopology("registered_region_name", report.RegisteredRegionName)
	addTopology("assignment_mode", report.AssignmentMode)

	providers := make([]any, 0, len(report.Providers))
	for _, provider := range report.Providers {
		riskLevel, riskScore := "", ""
		regionCode, regionName := "", ""
		var isProxy, isTor, isVPN, isServer, isAbuser, isRobot *bool
		if provider.Status == "success" {
			riskLevel, riskScore = provider.RiskLevel, provider.RiskScore
			regionCode, regionName = provider.RegionCode, provider.RegionName
			isProxy, isTor, isVPN = provider.IsProxy, provider.IsTor, provider.IsVPN
			isServer, isAbuser, isRobot = provider.IsServer, provider.IsAbuser, provider.IsRobot
		}
		item := map[string]any{
			"provider": provider.Provider, "status": provider.Status, "source_type": provider.SourceType,
			"latency_ms": provider.LatencyMS, "usage_type": provider.UsageType, "company_type": provider.CompanyType,
			"risk_level": riskLevel, "risk_score": riskScore,
			"is_proxy": isProxy, "is_tor": isTor, "is_vpn": isVPN,
			"is_server": isServer, "is_abuser": isAbuser, "is_robot": isRobot,
			"error_code": provider.ErrorCode,
		}
		if includeAllTopology || topologySelected(selected, "providers.region_code") {
			item["region_code"] = regionCode
		}
		if includeAllTopology || topologySelected(selected, "providers.region_name") {
			item["region_name"] = regionName
		}
		providers = append(providers, item)
	}
	payload["providers"] = providers
	services := make([]any, 0, len(report.Services))
	for _, service := range report.Services {
		status, region, unlockType := "unknown", "", ""
		if service.ProbeStatus == "success" {
			status, region, unlockType = service.Status, service.Region, service.UnlockType
		}
		item := map[string]any{
			"service": service.Service, "source": service.Source, "status": status,
			"probe_status": service.ProbeStatus, "latency_ms": service.LatencyMS,
			"unlock_type": unlockType, "error_code": service.ErrorCode,
		}
		if includeAllTopology || topologySelected(selected, "services.region") {
			item["region"] = region
		}
		services = append(services, item)
	}
	payload["services"] = services
	return payload
}

func validIPQualityCoverage(coverage IPQualityCoverage) bool {
	providerCounts := []int{
		coverage.ExpectedProviderCount, coverage.SuccessfulProviderCount, coverage.FailedProviderCount,
		coverage.SkippedProviderCount, coverage.NotConfiguredProviderCount,
	}
	serviceCounts := []int{
		coverage.ExpectedServiceCount, coverage.SuccessfulServiceCount, coverage.FailedServiceCount,
		coverage.SkippedServiceCount, coverage.NotConfiguredServiceCount,
	}
	for _, count := range append(providerCounts, serviceCounts...) {
		if count < 0 || uint64(count) > evidence.MaxSnapshotDataPoints {
			return false
		}
	}
	providerTotal := coverage.SuccessfulProviderCount + coverage.FailedProviderCount + coverage.SkippedProviderCount + coverage.NotConfiguredProviderCount
	serviceTotal := coverage.SuccessfulServiceCount + coverage.FailedServiceCount + coverage.SkippedServiceCount + coverage.NotConfiguredServiceCount
	return providerTotal == coverage.ExpectedProviderCount && serviceTotal == coverage.ExpectedServiceCount
}

func validIPQualityRows(report IPQualityEvidenceReport) bool {
	providerCounts := map[string]int{}
	providerIDs := make(map[string]struct{}, len(report.Providers))
	for _, provider := range report.Providers {
		if strings.TrimSpace(provider.Provider) == "" || provider.Provider != strings.TrimSpace(provider.Provider) ||
			!validIPQualitySourceStatus(provider.Status) ||
			!validIPQualitySourceType(provider.SourceType) || (provider.LatencyMS != nil && *provider.LatencyMS < 0) {
			return false
		}
		if _, duplicate := providerIDs[provider.Provider]; duplicate {
			return false
		}
		providerIDs[provider.Provider] = struct{}{}
		providerCounts[provider.Status]++
	}
	serviceCounts := map[string]int{}
	serviceIDs := make(map[string]struct{}, len(report.Services))
	for _, service := range report.Services {
		if strings.TrimSpace(service.Service) == "" || service.Service != strings.TrimSpace(service.Service) ||
			service.Source != strings.TrimSpace(service.Source) || !validIPQualityServiceStatus(service.Status) ||
			!validIPQualitySourceStatus(service.ProbeStatus) || (service.LatencyMS != nil && *service.LatencyMS < 0) {
			return false
		}
		identity := service.Service + "\x00" + service.Source
		if _, duplicate := serviceIDs[identity]; duplicate {
			return false
		}
		serviceIDs[identity] = struct{}{}
		serviceCounts[service.ProbeStatus]++
	}
	return validIPQualityAssignmentMode(report.AssignmentMode) &&
		len(report.Providers) == report.Coverage.ExpectedProviderCount &&
		providerCounts["success"] == report.Coverage.SuccessfulProviderCount &&
		providerCounts["failure"] == report.Coverage.FailedProviderCount &&
		providerCounts["skipped"] == report.Coverage.SkippedProviderCount &&
		providerCounts["not_configured"] == report.Coverage.NotConfiguredProviderCount &&
		len(report.Services) == report.Coverage.ExpectedServiceCount &&
		serviceCounts["success"] == report.Coverage.SuccessfulServiceCount &&
		serviceCounts["failure"] == report.Coverage.FailedServiceCount &&
		serviceCounts["skipped"] == report.Coverage.SkippedServiceCount &&
		serviceCounts["not_configured"] == report.Coverage.NotConfiguredServiceCount
}

func validIPQualitySourceStatus(value string) bool {
	switch value {
	case "success", "failure", "skipped", "not_configured":
		return true
	default:
		return false
	}
}

func validIPQualitySourceType(value string) bool {
	switch value {
	case "default", "optional", "custom":
		return true
	default:
		return false
	}
}

func validIPQualityServiceStatus(value string) bool {
	switch value {
	case "unlocked", "blocked", "unknown":
		return true
	default:
		return false
	}
}

func validIPQualityAssignmentMode(value string) bool {
	switch value {
	case "", "link", "ip_match":
		return true
	default:
		return false
	}
}

func normalizeIPQualityEvidenceReport(report IPQualityEvidenceReport) IPQualityEvidenceReport {
	report.Latitude = cloneFloat64Pointer(report.Latitude)
	report.Longitude = cloneFloat64Pointer(report.Longitude)
	report.Providers = append([]IPQualityProviderEvidence(nil), report.Providers...)
	for index := range report.Providers {
		report.Providers[index].LatencyMS = cloneIntPointer(report.Providers[index].LatencyMS)
		report.Providers[index].IsProxy = cloneBoolPointer(report.Providers[index].IsProxy)
		report.Providers[index].IsTor = cloneBoolPointer(report.Providers[index].IsTor)
		report.Providers[index].IsVPN = cloneBoolPointer(report.Providers[index].IsVPN)
		report.Providers[index].IsServer = cloneBoolPointer(report.Providers[index].IsServer)
		report.Providers[index].IsAbuser = cloneBoolPointer(report.Providers[index].IsAbuser)
		report.Providers[index].IsRobot = cloneBoolPointer(report.Providers[index].IsRobot)
	}
	sort.Slice(report.Providers, func(left, right int) bool {
		if report.Providers[left].Provider != report.Providers[right].Provider {
			return report.Providers[left].Provider < report.Providers[right].Provider
		}
		return report.Providers[left].SourceType < report.Providers[right].SourceType
	})
	report.Services = append([]IPQualityServiceEvidence(nil), report.Services...)
	for index := range report.Services {
		report.Services[index].LatencyMS = cloneIntPointer(report.Services[index].LatencyMS)
	}
	sort.Slice(report.Services, func(left, right int) bool {
		if report.Services[left].Service != report.Services[right].Service {
			return report.Services[left].Service < report.Services[right].Service
		}
		return report.Services[left].Source < report.Services[right].Source
	})
	return report
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func cloneBoolPointer(value *bool) *bool {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func ipQualityCoverageIncomplete(coverage IPQualityCoverage) bool {
	return (coverage.ExpectedProviderCount > 0 && coverage.SuccessfulProviderCount < coverage.ExpectedProviderCount) ||
		(coverage.ExpectedServiceCount > 0 && coverage.SuccessfulServiceCount < coverage.ExpectedServiceCount)
}

func topologySelected(selected map[string]struct{}, path string) bool {
	_, ok := selected[path]
	return ok
}

func ipQualityDescriptor() evidence.Descriptor {
	normal := []string{
		"report_id", "observed_at", "received_at", "ip_version", "status", "stale", "stale_after_seconds", "is_backfilled", "ambiguous", "risk_level",
		"coverage.expected_provider_count", "coverage.successful_provider_count", "coverage.failed_provider_count", "coverage.skipped_provider_count", "coverage.not_configured_provider_count",
		"coverage.expected_service_count", "coverage.successful_service_count", "coverage.failed_service_count", "coverage.skipped_service_count", "coverage.not_configured_service_count",
		"providers.provider", "providers.status", "providers.source_type", "providers.latency_ms", "providers.usage_type", "providers.company_type", "providers.risk_level", "providers.risk_score",
		"providers.is_proxy", "providers.is_tor", "providers.is_vpn", "providers.is_server", "providers.is_abuser", "providers.is_robot", "providers.error_code",
		"services.service", "services.source", "services.status", "services.probe_status", "services.latency_ms", "services.unlock_type", "services.error_code",
	}
	topology := []string{
		"ip_address", "asn", "organization", "latitude", "longitude", "use_region_code", "use_region_name",
		"registered_region_code", "registered_region_name", "assignment_mode", "providers.region_code", "providers.region_name", "services.region",
	}
	forbidden := []string{"raw_json", "diagnostics_json", "providers.extra_json", "services.extra_json", "fingerprint", "stdout", "stderr", "raw_payload"}
	fields := make([]evidence.FieldDefinition, 0, len(normal)+len(topology)+len(forbidden))
	for _, path := range normal {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityNormal})
	}
	for _, path := range topology {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivitySensitiveTopology})
	}
	for _, path := range forbidden {
		fields = append(fields, evidence.FieldDefinition{Path: path, Sensitivity: evidence.SensitivityForbidden})
	}
	return evidence.Descriptor{
		Key: evidence.IPQualityReportV1Key(), Fields: fields,
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "ip_quality_report_v1", MaxCanonicalBytes: evidence.MaxCanonicalPayloadBytes,
		},
	}
}

func validTopologySelection(descriptor evidence.Descriptor, fields []string) bool {
	allowed := make(map[string]struct{})
	for _, field := range descriptor.Fields {
		if field.Sensitivity == evidence.SensitivitySensitiveTopology {
			allowed[field.Path] = struct{}{}
		}
	}
	for index, field := range fields {
		if _, ok := allowed[field]; !ok || (index > 0 && fields[index-1] >= field) {
			return false
		}
	}
	return true
}

func appendForbiddenPreviewDecisions(descriptor evidence.Descriptor, decisions []evidence.FieldDecision) []evidence.FieldDecision {
	out := append([]evidence.FieldDecision(nil), decisions...)
	for _, field := range descriptor.Fields {
		if field.Sensitivity == evidence.SensitivityForbidden {
			out = append(out, evidence.FieldDecision{Path: field.Path, Sensitivity: field.Sensitivity, Action: evidence.RedactionActionForbidden})
		}
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Path < out[right].Path })
	return out
}

func boolCount(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
