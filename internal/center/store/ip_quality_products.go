package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipidentity"
)

const (
	ipQualityAgentProductMaxBytes    = 64 << 10
	ipQualityProviderDetailsMaxBytes = 8 << 10
	ipQualityServiceChecksMaxBytes   = 16 << 10
	ipQualityIntelligenceDeadline    = 10 * time.Minute

	ipQualityProductBatchMarker    = "-- ip quality product batch"
	ipQualityProductProviderMarker = "-- ip quality product providers"
)

type ipQualityProductWrite struct {
	productJSON []byte
	summaryJSON []byte
	details     [][]byte
	checks      [][]byte
	enqueue     bool
}

// LoadIPQualityProducts reads product views for the selected reports inside the
// caller's snapshot. Report ID and intelligence state are applied around
// BuildProductView; stored summaries are not recomputed.
func LoadIPQualityProducts(ctx context.Context, tx pgx.Tx, reportIDs []string) (map[string]ipquality.IPQualityProductView, error) {
	return loadIPQualityProducts(ctx, tx, reportIDs)
}

func loadIPQualityProducts(ctx context.Context, db ipQualityDB, reportIDs []string) (map[string]ipquality.IPQualityProductView, error) {
	ids := uniqueIPQualityReportIDs(reportIDs)
	out := make(map[string]ipquality.IPQualityProductView, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	stored, err := queryIPQualityProductRows(ctx, db, ids)
	if err != nil {
		return nil, err
	}
	revisionIDs := make([]string, 0, len(stored))
	for id, row := range stored {
		if !isJSONNull(row.productJSON) {
			revisionIDs = append(revisionIDs, id)
		}
	}
	providers, err := queryIPQualityProductProviders(ctx, db, revisionIDs)
	if err != nil {
		return nil, err
	}
	for id, row := range stored {
		view, ok, err := buildStoredIPQualityProduct(row, providers[id])
		if err != nil {
			return nil, err
		}
		if ok {
			out[id] = view
		}
	}
	return out, nil
}

type ipQualityStoredProductRow struct {
	reportID                string
	ipAddress               string
	ipVersion               int
	observedAt              time.Time
	productJSON             []byte
	agentSummaryJSON        []byte
	intelligenceState       *string
	completedAt             *time.Time
	snapshotJSON            []byte
	intelligenceSummaryJSON []byte
}

func queryIPQualityProductRows(ctx context.Context, db ipQualityDB, reportIDs []string) (map[string]ipQualityStoredProductRow, error) {
	rows, err := db.Query(ctx, `
		-- ip quality product batch
		select report.report_id,
			report.ip_address,
			report.ip_version,
			report.observed_at,
			report.product_json,
			report.product_summary_json,
			intelligence.state,
			intelligence.completed_at,
			intelligence.data_json,
			intelligence.summary_json
		from public.ip_quality_reports as report
		left join public.ip_quality_intelligence as intelligence
			on intelligence.report_id = report.report_id
		where report.report_id = any($1)`, reportIDs)
	if err != nil {
		return nil, fmt.Errorf("query ip quality products: %w", err)
	}
	defer rows.Close()
	out := make(map[string]ipQualityStoredProductRow, len(reportIDs))
	for rows.Next() {
		var row ipQualityStoredProductRow
		if err := rows.Scan(
			&row.reportID,
			&row.ipAddress,
			&row.ipVersion,
			&row.observedAt,
			&row.productJSON,
			&row.agentSummaryJSON,
			&row.intelligenceState,
			&row.completedAt,
			&row.snapshotJSON,
			&row.intelligenceSummaryJSON,
		); err != nil {
			return nil, fmt.Errorf("scan ip quality product: %w", err)
		}
		out[row.reportID] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality products: %w", err)
	}
	return out, nil
}

func queryIPQualityProductProviders(ctx context.Context, db ipQualityDB, reportIDs []string) (map[string][]ipquality.ProviderResultWrite, error) {
	out := make(map[string][]ipquality.ProviderResultWrite, len(reportIDs))
	if len(reportIDs) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		-- ip quality product providers
		select provider_row.report_id,
			provider_row.provider,
			provider_row.status,
			provider_row.source_type,
			provider_row.latency_ms,
			provider_row.usage_type,
			provider_row.company_type,
			provider_row.risk_level,
			provider_row.risk_score,
			provider_row.region_code,
			provider_row.region_name,
			provider_row.is_proxy,
			provider_row.is_tor,
			provider_row.is_vpn,
			provider_row.is_server,
			provider_row.is_abuser,
			provider_row.is_robot,
			provider_row.error_code,
			provider_row.error_summary,
			provider_row.extra_json,
			provider_row.details_json
		from public.ip_quality_provider_results as provider_row
		where provider_row.report_id = any($1)
		order by provider_row.report_id, provider_row.provider`, reportIDs)
	if err != nil {
		return nil, fmt.Errorf("query ip quality product providers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var reportID string
		var provider ipquality.ProviderResultWrite
		var extraJSON, detailsJSON []byte
		if err := rows.Scan(
			&reportID,
			&provider.Provider,
			&provider.Status,
			&provider.SourceType,
			&provider.LatencyMS,
			&provider.UsageType,
			&provider.CompanyType,
			&provider.RiskLevel,
			&provider.RiskScore,
			&provider.RegionCode,
			&provider.RegionName,
			&provider.IsProxy,
			&provider.IsTor,
			&provider.IsVPN,
			&provider.IsServer,
			&provider.IsAbuser,
			&provider.IsRobot,
			&provider.ErrorCode,
			&provider.ErrorSummary,
			&extraJSON,
			&detailsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan ip quality product provider: %w", err)
		}
		if len(extraJSON) > 0 {
			provider.ExtraJSON = append([]byte(nil), extraJSON...)
		}
		details, err := decodeIPQualityProviderDetails(detailsJSON)
		if err != nil {
			return nil, err
		}
		provider.Details = details
		out[reportID] = append(out[reportID], provider)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality product providers: %w", err)
	}
	return out, nil
}

type storedIPQualityProduct struct {
	revision    int
	state       string
	summary     *agentapi.IPQualityProductSummary
	product     *agentapi.IPQualityAgentProduct
	snapshot    *ipquality.IntelligenceSnapshot
	completedAt *time.Time
}

func interpretStoredIPQualityProduct(productJSON, agentSummaryJSON []byte, intelligenceState *string, completedAt *time.Time, snapshotJSON, intelligenceSummaryJSON []byte, hasSnapshot bool) (storedIPQualityProduct, error) {
	if isJSONNull(productJSON) {
		return storedIPQualityProduct{}, nil
	}
	product, err := decodeIPQualityAgentProduct(productJSON)
	if err != nil {
		return storedIPQualityProduct{}, err
	}
	if product == nil || product.Revision != agentapi.IPQualityProductRevision {
		return storedIPQualityProduct{}, fmt.Errorf("invalid stored ip quality product revision")
	}
	state, err := canonicalIPQualityIntelligenceState(intelligenceState)
	if err != nil {
		return storedIPQualityProduct{}, err
	}
	summaryJSON := agentSummaryJSON
	var snapshot *ipquality.IntelligenceSnapshot
	// Complete and partial use the snapshot summary. Failed keeps the persisted
	// Agent-only summary, but a terminal snapshot still supplies diagnostic rows.
	// Swept failures have no snapshot, and skipped must not surface a cleared one.
	if hasSnapshot && (state == "complete" || state == "partial") && !isJSONNull(intelligenceSummaryJSON) {
		summaryJSON = intelligenceSummaryJSON
	}
	if hasSnapshot && (state == "complete" || state == "partial" || state == "failed") && !isJSONNull(snapshotJSON) {
		decoded, err := decodeIPQualitySnapshot(snapshotJSON)
		if err != nil {
			return storedIPQualityProduct{}, err
		}
		if decoded != nil {
			decoded.State = state
			decoded.CompletedAt = completedAt
			snapshot = decoded
		}
	}
	if isJSONNull(summaryJSON) {
		return storedIPQualityProduct{}, fmt.Errorf("revision 1 ip quality report is missing its stored summary")
	}
	summary, err := decodeIPQualityProductSummary(summaryJSON)
	if err != nil {
		return storedIPQualityProduct{}, err
	}
	return storedIPQualityProduct{
		revision:    agentapi.IPQualityProductRevision,
		state:       state,
		summary:     summary,
		product:     product,
		snapshot:    snapshot,
		completedAt: completedAt,
	}, nil
}

func buildStoredIPQualityProduct(row ipQualityStoredProductRow, providers []ipquality.ProviderResultWrite) (ipquality.IPQualityProductView, bool, error) {
	facts, err := interpretStoredIPQualityProduct(
		row.productJSON,
		row.agentSummaryJSON,
		row.intelligenceState,
		row.completedAt,
		row.snapshotJSON,
		row.intelligenceSummaryJSON,
		ipQualitySnapshotObject(row.snapshotJSON),
	)
	if err != nil {
		return ipquality.IPQualityProductView{}, false, err
	}
	if facts.revision != agentapi.IPQualityProductRevision {
		return ipquality.IPQualityProductView{}, false, nil
	}
	report := ipquality.ReportWrite{
		ObservedAt:      row.observedAt,
		IPAddress:       row.ipAddress,
		IPVersion:       row.ipVersion,
		Product:         facts.product,
		ProviderResults: providers,
	}
	view := ipquality.BuildProductView(report, facts.snapshot)
	view.ReportID = row.reportID
	view.State = facts.state
	view.CompletedAt = facts.completedAt
	view.Summary = normalizeIPQualityProductSummary(*facts.summary)
	return view, true, nil
}

func applyIPQualityProductSummary(views map[string]ipquality.IPQualityProductView, summary *ipquality.Summary) {
	if summary == nil {
		return
	}
	view, ok := views[summary.ReportID]
	if !ok {
		summary.ProductRevision = 0
		summary.IntelligenceState = ""
		summary.ProductSummary = nil
		return
	}
	summary.ProductRevision = view.Revision
	summary.IntelligenceState = view.State
	copied := view.Summary
	summary.ProductSummary = &copied
}

func attachIPQualityProducts(ctx context.Context, db ipQualityDB, result *ipquality.VPSReport) error {
	if result == nil {
		return nil
	}
	views, err := loadIPQualityProducts(ctx, db, ipQualityReportIDs(result))
	if err != nil {
		return err
	}
	applyIPQualityProductSummary(views, result.Summary)
	for i := range result.Families {
		applyIPQualityProductSummary(views, result.Families[i].LatestReportSummary)
	}
	for i := range result.History {
		applyIPQualityProductSummary(views, &result.History[i])
	}
	if result.Summary != nil {
		if view, ok := views[result.Summary.ReportID]; ok {
			copied := view
			result.Product = &copied
		}
	}
	return nil
}

func applyIPQualityProductSummaries(ctx context.Context, db ipQualityDB, summaries map[string]ipquality.Summary) error {
	if len(summaries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if summary.ReportID != "" {
			ids = append(ids, summary.ReportID)
		}
	}
	views, err := loadIPQualityProducts(ctx, db, ids)
	if err != nil {
		return err
	}
	for vpsID, summary := range summaries {
		applyIPQualityProductSummary(views, &summary)
		summaries[vpsID] = summary
	}
	return nil
}

func ipQualityReportIDs(result *ipquality.VPSReport) []string {
	if result == nil {
		return nil
	}
	ids := make([]string, 0, 2+len(result.History))
	if result.Summary != nil {
		ids = append(ids, result.Summary.ReportID)
	}
	if result.LatestReport != nil {
		ids = append(ids, result.LatestReport.ReportID)
	}
	for _, family := range result.Families {
		if family.LatestReportSummary != nil {
			ids = append(ids, family.LatestReportSummary.ReportID)
		}
	}
	for _, summary := range result.History {
		ids = append(ids, summary.ReportID)
	}
	return ids
}

func uniqueIPQualityReportIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func prepareIPQualityProductWrite(report ipquality.ReportWrite) (ipQualityProductWrite, error) {
	prepared := normalizeIPQualityProductWrite(report)
	if err := validateIPQualityProductWrite(prepared); err != nil {
		return ipQualityProductWrite{}, err
	}
	if prepared.Product == nil || prepared.Product.Revision != agentapi.IPQualityProductRevision {
		return ipQualityProductWrite{}, nil
	}
	productJSON, err := json.Marshal(prepared.Product)
	if err != nil || len(productJSON) > ipQualityAgentProductMaxBytes {
		return ipQualityProductWrite{}, fmt.Errorf("%w: product size", ipquality.ErrInvalidIPQualityReport)
	}
	summary := normalizeIPQualityProductSummary(ipquality.BuildProductView(prepared, nil).Summary)
	summaryJSON, err := json.Marshal(summary)
	if err != nil || len(summaryJSON) > ipQualityAgentProductMaxBytes {
		return ipQualityProductWrite{}, fmt.Errorf("%w: product summary size", ipquality.ErrInvalidIPQualityReport)
	}
	details := make([][]byte, len(prepared.ProviderResults))
	for i, provider := range prepared.ProviderResults {
		if provider.Details == nil {
			continue
		}
		raw, err := json.Marshal(provider.Details)
		if err != nil || len(raw) > ipQualityProviderDetailsMaxBytes {
			return ipQualityProductWrite{}, fmt.Errorf("%w: provider details size", ipquality.ErrInvalidIPQualityReport)
		}
		details[i] = raw
	}
	checks := make([][]byte, len(prepared.ServiceUnlocks))
	for i, unlock := range prepared.ServiceUnlocks {
		raw, err := json.Marshal(unlock.Checks)
		if err != nil || len(raw) > ipQualityServiceChecksMaxBytes {
			return ipQualityProductWrite{}, fmt.Errorf("%w: service checks size", ipquality.ErrInvalidIPQualityReport)
		}
		checks[i] = raw
	}
	return ipQualityProductWrite{
		productJSON: productJSON,
		summaryJSON: summaryJSON,
		details:     details,
		checks:      checks,
		enqueue:     ipQualityIntelligenceEligible(prepared),
	}, nil
}

func validateIPQualityProductWrite(report ipquality.ReportWrite) error {
	if err := agentapi.ValidateIPQualityAgentProduct(report.Product, report.IPAddress, report.IPVersion, len(report.ProviderResults)); err != nil {
		return fmt.Errorf("%w: %v", ipquality.ErrInvalidIPQualityReport, err)
	}
	revision := 0
	if report.Product != nil {
		revision = report.Product.Revision
	}
	for _, provider := range report.ProviderResults {
		if err := agentapi.ValidateIPQualityProviderDetails(provider.Provider, provider.Details, revision, report.IPAddress); err != nil {
			return fmt.Errorf("%w: %v", ipquality.ErrInvalidIPQualityReport, err)
		}
	}
	for _, unlock := range report.ServiceUnlocks {
		if err := agentapi.ValidateIPQualityServiceChecks(unlock.Service, unlock.Checks, revision); err != nil {
			return fmt.Errorf("%w: %v", ipquality.ErrInvalidIPQualityReport, err)
		}
	}
	return nil
}

func normalizeIPQualityProductWrite(report ipquality.ReportWrite) ipquality.ReportWrite {
	if report.Product == nil || report.Product.Revision != agentapi.IPQualityProductRevision {
		return report
	}
	product := normalizeIPQualityAgentProduct(*report.Product)
	report.Product = &product
	if len(report.ProviderResults) > 0 {
		providers := append([]ipquality.ProviderResultWrite(nil), report.ProviderResults...)
		for i := range providers {
			if providers[i].Details == nil {
				continue
			}
			details := normalizeIPQualityProviderDetails(*providers[i].Details)
			providers[i].Details = &details
		}
		report.ProviderResults = providers
	}
	if len(report.ServiceUnlocks) > 0 {
		unlocks := append([]ipquality.ServiceUnlockWrite(nil), report.ServiceUnlocks...)
		for i := range unlocks {
			unlocks[i].Checks = normalizeIPQualityServiceChecks(unlocks[i].Checks)
		}
		report.ServiceUnlocks = unlocks
	}
	return report
}

// ipQualityIntelligenceEligible is the enqueue gate: revision 1, not backfilled,
// and a parsed non-placeholder exit of the reported family. Documentation ranges
// remain eligible; only unspecified addresses are placeholders.
func ipQualityIntelligenceEligible(report ipquality.ReportWrite) bool {
	if report.IsBackfilled || report.Product == nil || report.Product.Revision != agentapi.IPQualityProductRevision {
		return false
	}
	addr, ok := ipidentity.Parse(report.IPAddress)
	if !ok || !addr.IsValid() || addr.IsUnspecified() {
		return false
	}
	switch report.IPVersion {
	case 4:
		return addr.Is4()
	case 6:
		return addr.Is6() && !addr.Is4In6()
	default:
		return false
	}
}

func enqueueIPQualityIntelligence(ctx context.Context, tx ipQualityExecutor, reportID string, receivedAt time.Time) error {
	if _, err := tx.Exec(ctx, `
		insert into ip_quality_intelligence (
			report_id,
			state,
			deadline_at
		) values ($1, 'pending', $2)`,
		reportID,
		receivedAt.Add(ipQualityIntelligenceDeadline),
	); err != nil {
		return fmt.Errorf("enqueue ip quality intelligence for report %q: %w", reportID, err)
	}
	return nil
}

func decodeIPQualityAgentProduct(raw []byte) (*agentapi.IPQualityAgentProduct, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var product agentapi.IPQualityAgentProduct
	if err := json.Unmarshal(raw, &product); err != nil {
		return nil, fmt.Errorf("decode ip quality agent product: %w", err)
	}
	normalized := normalizeIPQualityAgentProduct(product)
	return &normalized, nil
}

func decodeIPQualityProviderDetails(raw []byte) (*agentapi.IPQualityProviderDetails, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var details agentapi.IPQualityProviderDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, fmt.Errorf("decode ip quality provider details: %w", err)
	}
	normalized := normalizeIPQualityProviderDetails(details)
	return &normalized, nil
}

func decodeIPQualityServiceChecks(raw []byte) ([]agentapi.IPQualityServiceCheck, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var checks []agentapi.IPQualityServiceCheck
	if err := json.Unmarshal(raw, &checks); err != nil {
		return nil, fmt.Errorf("decode ip quality service checks: %w", err)
	}
	return normalizeIPQualityServiceChecks(checks), nil
}

func decodeIPQualityProductSummary(raw []byte) (*agentapi.IPQualityProductSummary, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var summary agentapi.IPQualityProductSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		return nil, fmt.Errorf("decode ip quality product summary: %w", err)
	}
	normalized := normalizeIPQualityProductSummary(summary)
	return &normalized, nil
}

func decodeIPQualitySnapshot(raw []byte) (*ipquality.IntelligenceSnapshot, error) {
	if isJSONNull(raw) {
		return nil, nil
	}
	var snapshot ipquality.IntelligenceSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode ip quality intelligence snapshot: %w", err)
	}
	normalizeIPQualitySnapshot(&snapshot)
	return &snapshot, nil
}

func normalizeIPQualityAgentProduct(product agentapi.IPQualityAgentProduct) agentapi.IPQualityAgentProduct {
	if product.PTRNames == nil {
		product.PTRNames = []string{}
	}
	if product.Mail == nil {
		product.Mail = []agentapi.IPQualityMailResult{}
	}
	return product
}

func normalizeIPQualityProviderDetails(details agentapi.IPQualityProviderDetails) agentapi.IPQualityProviderDetails {
	if details.Scores == nil {
		details.Scores = []agentapi.IPQualityScore{}
	}
	if details.FieldStates == nil {
		details.FieldStates = map[string]string{}
	}
	return details
}

func normalizeIPQualityServiceChecks(checks []agentapi.IPQualityServiceCheck) []agentapi.IPQualityServiceCheck {
	if checks == nil {
		return []agentapi.IPQualityServiceCheck{}
	}
	return checks
}

func normalizeIPQualityProductSummary(summary agentapi.IPQualityProductSummary) agentapi.IPQualityProductSummary {
	if summary.RiskFlags == nil {
		summary.RiskFlags = []string{}
	}
	if summary.HighRiskSources == nil {
		summary.HighRiskSources = []string{}
	}
	if summary.RestrictedServices == nil {
		summary.RestrictedServices = []string{}
	}
	if summary.GeoStatus == "" {
		summary.GeoStatus = "unknown"
	}
	if summary.IPType != nil {
		copied := *summary.IPType
		summary.IPType = &copied
	}
	return summary
}

func normalizeIPQualitySnapshot(snapshot *ipquality.IntelligenceSnapshot) {
	if snapshot == nil {
		return
	}
	if snapshot.Providers == nil {
		snapshot.Providers = []ipquality.ProviderResultRead{}
	}
	if snapshot.Blacklists == nil {
		snapshot.Blacklists = []agentapi.IPQualityBlacklistResult{}
	}
	if snapshot.SourceOutcomes == nil {
		snapshot.SourceOutcomes = []ipquality.SourceOutcome{}
	}
	if snapshot.ASN.Origins == nil {
		snapshot.ASN.Origins = []string{}
	}
	for i := range snapshot.Blacklists {
		if snapshot.Blacklists[i].Codes == nil {
			snapshot.Blacklists[i].Codes = []string{}
		}
	}
	for i := range snapshot.Providers {
		if snapshot.Providers[i].Details == nil {
			continue
		}
		details := normalizeIPQualityProviderDetails(*snapshot.Providers[i].Details)
		snapshot.Providers[i].Details = &details
	}
}

func canonicalIPQualityIntelligenceState(state *string) (string, error) {
	if state == nil || *state == "" {
		return "skipped", nil
	}
	switch *state {
	case "pending", "running", "complete", "partial", "failed", "skipped":
		return *state, nil
	default:
		return "", fmt.Errorf("invalid ip quality intelligence state %q", *state)
	}
}

func ipQualitySnapshotObject(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func isJSONNull(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func ipQualityColumn(values [][]byte, index int) []byte {
	if index < 0 || index >= len(values) {
		return nil
	}
	return values[index]
}
