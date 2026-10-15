package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/contracts/agentapi"
)

type ipQualityDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type ipQualityExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type ipQualityTx interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresIPQualityRepository struct {
	db          ipQualityDB
	pool        *pgxpool.Pool
	beginTx     func(context.Context, pgx.TxOptions) (ipQualityTx, error)
	newReportID func() (string, error)
}

func NewPostgresIPQualityRepository(db *pgxpool.Pool) *PostgresIPQualityRepository {
	return &PostgresIPQualityRepository{
		db:   db,
		pool: db,
		beginTx: func(ctx context.Context, opts pgx.TxOptions) (ipQualityTx, error) {
			return db.BeginTx(ctx, opts)
		},
		newReportID: func() (string, error) {
			return ids.New("ipq")
		},
	}
}

var _ ipquality.Repository = (*PostgresIPQualityRepository)(nil)

func (r *PostgresIPQualityRepository) SaveReports(ctx context.Context, reports []ipquality.ReportWrite) error {
	if len(reports) == 0 {
		return nil
	}
	for i := range reports {
		reports[i].CollectionMetadata = agentapi.ParseCollectionMetadata(reports[i].DiagnosticsJSON)
		report := reports[i]
		if err := ipquality.ValidateReportWrite(report); err != nil {
			return err
		}
	}
	if r.beginTx == nil {
		return fmt.Errorf("ip quality repository cannot save reports without transaction support")
	}
	newReportID := r.newReportID
	if newReportID == nil {
		newReportID = func() (string, error) { return ids.New("ipq") }
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin ip quality reports transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, report := range reports {
		reportID, err := newReportID()
		if err != nil {
			return fmt.Errorf("generate ip quality report id: %w", err)
		}
		if report.ReceivedAt.IsZero() {
			report.ReceivedAt = time.Now().UTC()
		}
		if err := writeIPQualityReport(ctx, tx, reportID, report, nil); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ip quality reports transaction: %w", err)
	}
	return nil
}

func writeIPQualityReport(ctx context.Context, tx ipQualityExecutor, reportID string, report ipquality.ReportWrite, emptyRawJSON json.RawMessage) error {
	rawJSON := []byte(emptyRawJSON)
	if len(report.RawJSON) > 0 {
		rawJSON = ipquality.SanitizeRawJSON(report.RawJSON)
	}
	coverageJSON := []byte(nil)
	if len(report.CoverageJSON) > 0 {
		coverageJSON = ipquality.SanitizeExtraJSON(report.CoverageJSON)
	}
	diagnosticsJSON := []byte(nil)
	if len(report.DiagnosticsJSON) > 0 {
		diagnosticsJSON = ipquality.SanitizeCollectionDiagnostics(report.DiagnosticsJSON)
	}
	productWrite, err := prepareIPQualityProductWrite(report)
	if err != nil {
		return err
	}
	receivedAt := report.ReceivedAt
	if _, err := tx.Exec(ctx, `
			insert into ip_quality_reports (
				report_id,
				monitoring_instance_id,
				observed_at,
				received_at,
				agent_version,
				fingerprint,
				sync_batch_id,
				ip_address,
				ip_version,
				status,
				asn,
				organization,
				latitude,
				longitude,
				use_region_code,
				use_region_name,
				registered_region_code,
				registered_region_name,
				risk_level,
				error_code,
				error_summary,
				is_backfilled,
				raw_json,
				coverage_json,
				diagnostics_json,
				product_json,
				product_summary_json
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb,$24::jsonb,$25::jsonb,$26::jsonb,$27::jsonb)`,
		reportID,
		report.MonitoringInstanceID,
		report.ObservedAt,
		receivedAt,
		report.AgentVersion,
		report.Fingerprint,
		report.SyncBatchID,
		report.IPAddress,
		report.IPVersion,
		report.Status,
		report.ASN,
		report.Organization,
		report.Latitude,
		report.Longitude,
		report.UseRegionCode,
		report.UseRegionName,
		report.RegisteredRegionCode,
		report.RegisteredRegionName,
		report.RiskLevel,
		report.ErrorCode,
		report.ErrorSummary,
		report.IsBackfilled,
		rawJSON,
		coverageJSON,
		diagnosticsJSON,
		productWrite.productJSON,
		productWrite.summaryJSON,
	); err != nil {
		return fmt.Errorf("insert ip quality report for monitoring instance %q: %w", report.MonitoringInstanceID, err)
	}
	for i, provider := range report.ProviderResults {
		resultID, err := ids.New("ipqp")
		if err != nil {
			return fmt.Errorf("generate ip quality provider result id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
				insert into ip_quality_provider_results (
					result_id,
					report_id,
					provider,
					status,
					source_type,
					latency_ms,
					usage_type,
					company_type,
					risk_level,
					risk_score,
					region_code,
					region_name,
					is_proxy,
					is_tor,
					is_vpn,
					is_server,
					is_abuser,
					is_robot,
					error_code,
					error_summary,
					extra_json,
					details_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22::jsonb)`,
			resultID,
			reportID,
			provider.Provider,
			defaultString(provider.Status, "success"),
			defaultString(provider.SourceType, "default"),
			provider.LatencyMS,
			provider.UsageType,
			provider.CompanyType,
			provider.RiskLevel,
			provider.RiskScore,
			provider.RegionCode,
			provider.RegionName,
			provider.IsProxy,
			provider.IsTor,
			provider.IsVPN,
			provider.IsServer,
			provider.IsAbuser,
			provider.IsRobot,
			provider.ErrorCode,
			provider.ErrorSummary,
			ipquality.SanitizeExtraJSON(provider.ExtraJSON),
			ipQualityColumn(productWrite.details, i),
		); err != nil {
			return fmt.Errorf("insert ip quality provider result for report %q: %w", reportID, err)
		}
	}
	for i, unlock := range report.ServiceUnlocks {
		unlockID, err := ids.New("ipqu")
		if err != nil {
			return fmt.Errorf("generate ip quality service unlock id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
				insert into ip_quality_service_unlocks (
					unlock_id,
					report_id,
					service,
					source,
					status,
					probe_status,
					latency_ms,
					region,
					unlock_type,
					error_code,
					error_summary,
					extra_json,
					checks_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13::jsonb)`,
			unlockID,
			reportID,
			unlock.Service,
			unlock.Source,
			unlock.Status,
			defaultString(unlock.ProbeStatus, "success"),
			unlock.LatencyMS,
			unlock.Region,
			unlock.UnlockType,
			unlock.ErrorCode,
			unlock.ErrorSummary,
			ipquality.SanitizeExtraJSON(unlock.ExtraJSON),
			ipQualityColumn(productWrite.checks, i),
		); err != nil {
			return fmt.Errorf("insert ip quality service unlock for report %q: %w", reportID, err)
		}
	}
	if productWrite.enqueue {
		if err := enqueueIPQualityIntelligence(ctx, tx, reportID, receivedAt); err != nil {
			return err
		}
	}
	return nil
}

const overviewLatestIPQualitySummarySQL = `
		-- vps overview ip quality summary
		with selected_report as materialized (
			select state.vps_id, r.report_id, r.status, r.risk_level, r.observed_at, r.ip_version,
				r.product_json, r.product_summary_json,
				public.houfeng_parse_host_address(r.ip_address) as ip_identity
			from ip_quality_vps_family_state state
			join ip_quality_reports r on r.report_id = state.latest_report_id
			where state.vps_id = $1 and state.is_default
		), fallback_asset_identities as materialized (
			select v.vps_id,
				public.houfeng_parse_host_address(nullif(v.ipv4, '')) as ipv4_identity,
				public.houfeng_parse_host_address(nullif(v.ipv6, '')) as ipv6_identity
			from vps_assets v
			where not exists (select 1 from vps_monitoring_instance_links l
				where l.vps_id = v.vps_id and l.unlinked_at is null)
		)
		select selected.vps_id, selected.status, selected.risk_level,
			selected.observed_at < now() - make_interval(secs => coalesce((
				select case when coalesce(ip_quality_settings->>'stale_after_seconds', '') ~ '^[0-9]+$'
					then greatest((ip_quality_settings->>'stale_after_seconds')::integer, 60)
					else 604800 end
				from center_settings where settings_id = 'center'
			), 604800)) as stale,
			selected.observed_at, selected.ip_version,
			selected.product_json,
			selected.product_summary_json,
			intelligence.state,
			(intelligence.data_json is not null and jsonb_typeof(intelligence.data_json) = 'object') as has_snapshot,
			intelligence.summary_json
		from selected_report selected
		left join public.ip_quality_intelligence as intelligence
			on intelligence.report_id = selected.report_id
		where exists (select 1 from vps_monitoring_instance_links l
			where l.vps_id = selected.vps_id and l.unlinked_at is null)
			or not exists (
				select 1 from fallback_asset_identities other
				where other.vps_id <> selected.vps_id
					and (selected.ip_identity = other.ipv4_identity or selected.ip_identity = other.ipv6_identity)
			)`

func (r *PostgresIPQualityRepository) GetLatestVPSIPQualitySummary(ctx context.Context, vpsID string) (*ipquality.Summary, error) {
	rows, err := r.db.Query(ctx, overviewLatestIPQualitySummarySQL, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query overview ip quality summary: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate overview ip quality summary: %w", err)
		}
		return nil, nil
	}
	var summary ipquality.Summary
	var productJSON, agentSummaryJSON, intelligenceSummaryJSON []byte
	var intelligenceState *string
	var hasSnapshot bool
	if err := rows.Scan(
		&summary.VPSID,
		&summary.Status,
		&summary.RiskLevel,
		&summary.Stale,
		&summary.ObservedAt,
		&summary.IPVersion,
		&productJSON,
		&agentSummaryJSON,
		&intelligenceState,
		&hasSnapshot,
		&intelligenceSummaryJSON,
	); err != nil {
		return nil, fmt.Errorf("scan overview ip quality summary: %w", err)
	}
	facts, err := interpretStoredIPQualityProduct(productJSON, agentSummaryJSON, intelligenceState, nil, nil, intelligenceSummaryJSON, hasSnapshot)
	if err != nil {
		return nil, err
	}
	summary.ProductRevision = facts.revision
	summary.IntelligenceState = facts.state
	summary.ProductSummary = facts.summary
	return &summary, nil
}
func emptyVPSIPQualityReport() ipquality.VPSReport {
	return ipquality.VPSReport{
		SelectedIPVersion: 4,
		DefaultIPVersion:  4,
		Families: []ipquality.FamilyStatus{
			{IPVersion: 4, Availability: "unknown"},
			{IPVersion: 6, Availability: "unknown"},
		},
		ProviderResults: []ipquality.ProviderResultRead{},
		ServiceUnlocks:  []ipquality.ServiceUnlockRead{},
		History:         []ipquality.Summary{},
	}
}

// Read navigation and facts through the same snapshot. Close rows before issuing
// summary queries: pgx transactions share one connection.
func familyStateForVPS(ctx context.Context, db ipQualityDB, vpsID string) (ipquality.VPSReport, error) {
	result := emptyVPSIPQualityReport()
	rows, err := db.Query(ctx, `
		select state.ip_version, state.latest_report_id, state.availability,
			state.attempt_stale, state.is_default,
			attempt.observed_at, attempt.status, attempt.error_code, attempt.diagnostics_json
		from ip_quality_vps_family_state state
		left join ip_quality_reports attempt on attempt.report_id = state.latest_attempt_id
		where state.vps_id = $1
		order by state.ip_version`, vpsID)
	if err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("query ip quality family state: %w", err)
	}
	var reportIDs [2]string
	for rows.Next() {
		var version int
		var reportID *string
		var availability string
		var stale, isDefault bool
		var observedAt *time.Time
		var status, errorCode *string
		var diagnostics []byte
		if err := rows.Scan(&version, &reportID, &availability, &stale, &isDefault,
			&observedAt, &status, &errorCode, &diagnostics); err != nil {
			rows.Close()
			return ipquality.VPSReport{}, fmt.Errorf("scan ip quality family state: %w", err)
		}
		index := 0
		if version == 6 {
			index = 1
		} else if version != 4 {
			rows.Close()
			return ipquality.VPSReport{}, fmt.Errorf("invalid ip quality family state")
		}
		family := &result.Families[index]
		family.Availability = availability
		if isDefault {
			result.DefaultIPVersion = version
		}
		if reportID != nil {
			reportIDs[index] = *reportID
		}
		if observedAt != nil && status != nil {
			metadata := agentapi.ParseCollectionMetadata(diagnostics)
			attempt := &ipquality.FamilyAttempt{
				ObservedAt: *observedAt, Stale: stale, ReportStatus: *status,
				CollectionScope: "legacy_single_family",
			}
			if errorCode != nil {
				attempt.ErrorCode = ipquality.SafeCollectErrorCode(*errorCode)
			}
			// A malformed/unsupported paired marker must never claim legacy
			// semantics. Only validated metadata can expose a collection ID.
			if metadata.Paired {
				attempt.CollectionScope = "dual_stack"
				if metadata.Valid && metadata.IPVersion == version {
					if len(metadata.ExpectedIPVersions) == 1 {
						attempt.CollectionScope = "single_family"
					}
					attempt.CollectionID = metadata.CollectionID
				}
			}
			family.LatestAttempt = attempt
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("iterate ip quality family state: %w", err)
	}
	for index, reportID := range reportIDs {
		if reportID == "" {
			continue
		}
		summary, ok, err := summaryForAssignedVPSReport(ctx, db, vpsID, reportID)
		if err != nil {
			return ipquality.VPSReport{}, err
		}
		if !ok {
			return ipquality.VPSReport{}, fmt.Errorf("ip quality assigned summary missing from read snapshot")
		}
		result.Families[index].LatestReportSummary = &summary
	}
	result.SelectedIPVersion = result.DefaultIPVersion
	return result, nil
}

func (r *PostgresIPQualityRepository) GetVPSIPQuality(ctx context.Context, vpsID string, ipVersion int) (ipquality.VPSReport, error) {
	if ipVersion != 0 && ipVersion != 4 && ipVersion != 6 {
		return ipquality.VPSReport{}, ipquality.ErrInvalidIPQualityReport
	}
	if r.beginTx == nil {
		return ipquality.VPSReport{}, fmt.Errorf("ip quality repository cannot read reports without transaction support")
	}
	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("begin ip quality read transaction: %w", err)
	}
	if tx == nil {
		return ipquality.VPSReport{}, fmt.Errorf("begin ip quality read transaction: nil transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := familyStateForVPS(ctx, tx, vpsID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	result.SelectedIPVersion = result.DefaultIPVersion
	if ipVersion != 0 {
		result.SelectedIPVersion = ipVersion
	}
	var selected *ipquality.Summary
	for _, family := range result.Families {
		if family.IPVersion == result.SelectedIPVersion {
			selected = family.LatestReportSummary
		}
	}
	if selected == nil {
		if err := attachIPQualityProducts(ctx, tx, &result); err != nil {
			return ipquality.VPSReport{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ipquality.VPSReport{}, fmt.Errorf("commit ip quality read transaction: %w", err)
		}
		return result, nil
	}
	summary := *selected

	report, ok, err := reportForAssignedVPS(ctx, tx, vpsID, summary.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		return ipquality.VPSReport{}, fmt.Errorf("ip quality assigned report missing from read snapshot")
	}
	providers, err := providerResultsForReport(ctx, tx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	unlocks, err := serviceUnlocksForReport(ctx, tx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	history, err := historyForVPS(ctx, tx, vpsID, result.SelectedIPVersion)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	result.Summary = &summary
	result.LatestReport = &report
	result.ProviderResults = providers
	result.ServiceUnlocks = unlocks
	result.History = history
	if err := attachIPQualityProducts(ctx, tx, &result); err != nil {
		return ipquality.VPSReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("commit ip quality read transaction: %w", err)
	}
	return result, nil
}

func (r *PostgresIPQualityRepository) GetVPSIPQualityReportDetail(ctx context.Context, vpsID, reportID string) (ipquality.VPSReport, error) {
	if r.beginTx == nil {
		return ipquality.VPSReport{}, fmt.Errorf("ip quality repository cannot read reports without transaction support")
	}
	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("begin ip quality read transaction: %w", err)
	}
	if tx == nil {
		return ipquality.VPSReport{}, fmt.Errorf("begin ip quality read transaction: nil transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := familyStateForVPS(ctx, tx, vpsID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	report, ok, err := reportForAssignedVPS(ctx, tx, vpsID, reportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		if err := attachIPQualityProducts(ctx, tx, &result); err != nil {
			return ipquality.VPSReport{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return ipquality.VPSReport{}, fmt.Errorf("commit ip quality read transaction: %w", err)
		}
		return result, nil
	}
	summary, ok, err := summaryForAssignedVPSReport(ctx, tx, vpsID, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		return ipquality.VPSReport{}, fmt.Errorf("ip quality assigned summary missing from read snapshot")
	}
	providers, err := providerResultsForReport(ctx, tx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	unlocks, err := serviceUnlocksForReport(ctx, tx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	history, err := historyForVPS(ctx, tx, vpsID, report.IPVersion)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	result.SelectedIPVersion = report.IPVersion
	result.Summary = &summary
	result.LatestReport = &report
	result.ProviderResults = providers
	result.ServiceUnlocks = unlocks
	result.History = history
	if err := attachIPQualityProducts(ctx, tx, &result); err != nil {
		return ipquality.VPSReport{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ipquality.VPSReport{}, fmt.Errorf("commit ip quality read transaction: %w", err)
	}
	return result, nil
}

func (r *PostgresIPQualityRepository) ListLatestSummariesForVPS(ctx context.Context, vpsIDs []string) (map[string]ipquality.Summary, error) {
	return listLatestSummariesForVPS(ctx, r.db, vpsIDs)
}

func listLatestSummariesForVPS(ctx context.Context, db ipQualityDB, vpsIDs []string) (map[string]ipquality.Summary, error) {
	out := make(map[string]ipquality.Summary, len(vpsIDs))
	if len(vpsIDs) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		select latest.vps_id,
			latest.report_id,
			latest.observed_at,
			latest.ip_address,
			latest.ip_version,
			latest.status,
			latest.risk_level,
			latest.use_region_code,
			latest.use_region_name,
			latest.asn,
			latest.organization,
			latest.stale,
			latest.ambiguous,
			latest.assignment_mode,
			latest.error_code,
			latest.error_summary,
			latest.provider_count,
			latest.unlockable_count,
			latest.coverage_json
		from ip_quality_latest_vps_summaries latest
		where latest.vps_id = any($1)`, vpsIDs)
	if err != nil {
		return nil, fmt.Errorf("query ip quality latest summaries: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		summary, err := scanIPQualitySummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ip quality latest summary: %w", err)
		}
		out[summary.VPSID] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality latest summaries: %w", err)
	}
	rows.Close()
	if err := applyIPQualityProductSummaries(ctx, db, out); err != nil {
		return nil, err
	}
	return out, nil
}

func reportForAssignedVPS(ctx context.Context, db ipQualityDB, vpsID, reportID string) (ipquality.Report, bool, error) {
	rows, err := db.Query(ctx, `
		select r.report_id,
			r.monitoring_instance_id,
			r.observed_at,
			r.received_at,
			r.agent_version,
			r.fingerprint,
			r.sync_batch_id,
			r.ip_address,
			r.ip_version,
			r.status,
			r.asn,
			r.organization,
			r.latitude,
			r.longitude,
			r.use_region_code,
			r.use_region_name,
			r.registered_region_code,
			r.registered_region_name,
			r.risk_level,
			r.error_code,
			r.error_summary,
			r.is_backfilled,
			r.raw_json,
			r.coverage_json,
			r.diagnostics_json,
			r.created_at,
			r.product_json
		from ip_quality_reports r
		join ip_quality_assigned_vps_reports assigned on assigned.report_id = r.report_id
		where assigned.vps_id = $1 and r.report_id = $2
			and public.houfeng_parse_host_address(r.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
			and family(public.houfeng_parse_host_address(r.ip_address)) = r.ip_version
		limit 1`, vpsID, reportID)
	if err != nil {
		return ipquality.Report{}, false, fmt.Errorf("query assigned ip quality report %q for vps %q: %w", reportID, vpsID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return ipquality.Report{}, false, rows.Err()
	}
	report, err := scanIPQualityReport(rows)
	if err != nil {
		return ipquality.Report{}, false, fmt.Errorf("scan assigned ip quality report %q for vps %q: %w", reportID, vpsID, err)
	}
	if err := rows.Err(); err != nil {
		return ipquality.Report{}, false, fmt.Errorf("iterate assigned ip quality report %q for vps %q: %w", reportID, vpsID, err)
	}
	return report, true, nil
}

func summaryForAssignedVPSReport(ctx context.Context, db ipQualityDB, vpsID, reportID string) (ipquality.Summary, bool, error) {
	rows, err := db.Query(ctx, `
		select assigned.vps_id,
			assigned.report_id,
			assigned.observed_at,
			assigned.ip_address,
			assigned.ip_version,
			assigned.status,
			assigned.risk_level,
			assigned.use_region_code,
			assigned.use_region_name,
			assigned.asn,
			assigned.organization,
			assigned.stale,
			assigned.ambiguous,
			assigned.assignment_mode,
			assigned.error_code,
			assigned.error_summary,
			assigned.provider_count,
			assigned.unlockable_count,
			assigned.coverage_json
		from ip_quality_assigned_vps_reports assigned
		where assigned.vps_id = $1 and assigned.report_id = $2
			and public.houfeng_parse_host_address(assigned.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
			and family(public.houfeng_parse_host_address(assigned.ip_address)) = assigned.ip_version
		limit 1`, vpsID, reportID)
	if err != nil {
		return ipquality.Summary{}, false, fmt.Errorf("query assigned ip quality summary %q for vps %q: %w", reportID, vpsID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return ipquality.Summary{}, false, rows.Err()
	}
	summary, err := scanIPQualitySummary(rows)
	if err != nil {
		return ipquality.Summary{}, false, fmt.Errorf("scan assigned ip quality summary %q for vps %q: %w", reportID, vpsID, err)
	}
	if err := rows.Err(); err != nil {
		return ipquality.Summary{}, false, fmt.Errorf("iterate assigned ip quality summary %q for vps %q: %w", reportID, vpsID, err)
	}
	return summary, true, nil
}

func providerResultsForReport(ctx context.Context, db ipQualityDB, reportID string) ([]ipquality.ProviderResultRead, error) {
	rows, err := db.Query(ctx, `
		select provider,
			status,
			source_type,
			latency_ms,
			usage_type,
			company_type,
			risk_level,
			risk_score,
			region_code,
			region_name,
			is_proxy,
			is_tor,
			is_vpn,
			is_server,
			is_abuser,
			is_robot,
			error_code,
			error_summary,
			extra_json,
			details_json
		from ip_quality_provider_results
		where report_id = $1
		order by provider`, reportID)
	if err != nil {
		return nil, fmt.Errorf("query ip quality provider results for report %q: %w", reportID, err)
	}
	defer rows.Close()
	results := []ipquality.ProviderResultRead{}
	for rows.Next() {
		var result ipquality.ProviderResultRead
		var rawExtra, rawDetails []byte
		if err := rows.Scan(
			&result.Provider,
			&result.Status,
			&result.SourceType,
			&result.LatencyMS,
			&result.UsageType,
			&result.CompanyType,
			&result.RiskLevel,
			&result.RiskScore,
			&result.RegionCode,
			&result.RegionName,
			&result.IsProxy,
			&result.IsTor,
			&result.IsVPN,
			&result.IsServer,
			&result.IsAbuser,
			&result.IsRobot,
			&result.ErrorCode,
			&result.ErrorSummary,
			&rawExtra,
			&rawDetails,
		); err != nil {
			return nil, err
		}
		if len(rawExtra) > 0 {
			result.ExtraJSON = json.RawMessage(append([]byte(nil), rawExtra...))
		}
		details, err := decodeIPQualityProviderDetails(rawDetails)
		if err != nil {
			return nil, err
		}
		result.Details = details
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality provider results for report %q: %w", reportID, err)
	}
	return results, nil
}

func serviceUnlocksForReport(ctx context.Context, db ipQualityDB, reportID string) ([]ipquality.ServiceUnlockRead, error) {
	rows, err := db.Query(ctx, `
		select service,
			source,
			status,
			probe_status,
			latency_ms,
			region,
			unlock_type,
			error_code,
			error_summary,
			extra_json,
			checks_json
		from ip_quality_service_unlocks
		where report_id = $1
		order by service`, reportID)
	if err != nil {
		return nil, fmt.Errorf("query ip quality service unlocks for report %q: %w", reportID, err)
	}
	defer rows.Close()
	results := []ipquality.ServiceUnlockRead{}
	for rows.Next() {
		var result ipquality.ServiceUnlockRead
		var rawExtra, rawChecks []byte
		if err := rows.Scan(
			&result.Service,
			&result.Source,
			&result.Status,
			&result.ProbeStatus,
			&result.LatencyMS,
			&result.Region,
			&result.UnlockType,
			&result.ErrorCode,
			&result.ErrorSummary,
			&rawExtra,
			&rawChecks,
		); err != nil {
			return nil, err
		}
		if len(rawExtra) > 0 {
			result.ExtraJSON = json.RawMessage(append([]byte(nil), rawExtra...))
		}
		checks, err := decodeIPQualityServiceChecks(rawChecks)
		if err != nil {
			return nil, err
		}
		result.Checks = checks
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality service unlocks for report %q: %w", reportID, err)
	}
	return results, nil
}

func historyForVPS(ctx context.Context, db ipQualityDB, vpsID string, ipVersion int) ([]ipquality.Summary, error) {
	rows, err := db.Query(ctx, `
		select assigned.vps_id,
			assigned.report_id,
			assigned.observed_at,
			assigned.ip_address,
			assigned.ip_version,
			assigned.status,
			assigned.risk_level,
			assigned.use_region_code,
			assigned.use_region_name,
			assigned.asn,
			assigned.organization,
			assigned.stale,
			assigned.ambiguous,
			assigned.assignment_mode,
			assigned.error_code,
			assigned.error_summary,
			assigned.provider_count,
			assigned.unlockable_count,
			assigned.coverage_json
		from ip_quality_assigned_vps_reports assigned
		join ip_quality_reports r on r.report_id = assigned.report_id
		where assigned.vps_id = $1 and assigned.ip_version = $2
			and public.houfeng_parse_host_address(assigned.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
			and family(public.houfeng_parse_host_address(assigned.ip_address)) = assigned.ip_version
		order by assigned.observed_at desc, r.is_backfilled asc, r.received_at desc, assigned.report_id desc
		limit 30`, vpsID, ipVersion)
	if err != nil {
		return nil, fmt.Errorf("query ip quality history for vps %q: %w", vpsID, err)
	}
	defer rows.Close()
	history := []ipquality.Summary{}
	for rows.Next() {
		summary, err := scanIPQualitySummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ip quality history for vps %q: %w", vpsID, err)
		}
		history = append(history, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality history for vps %q: %w", vpsID, err)
	}
	return history, nil
}

type ipQualityScanner interface {
	Scan(dest ...any) error
}

func scanIPQualityReport(row ipQualityScanner) (ipquality.Report, error) {
	var report ipquality.Report
	var rawJSON []byte
	var coverageJSON []byte
	var diagnosticsJSON []byte
	var productJSON []byte
	if err := row.Scan(
		&report.ReportID,
		&report.MonitoringInstanceID,
		&report.ObservedAt,
		&report.ReceivedAt,
		&report.AgentVersion,
		&report.Fingerprint,
		&report.SyncBatchID,
		&report.IPAddress,
		&report.IPVersion,
		&report.Status,
		&report.ASN,
		&report.Organization,
		&report.Latitude,
		&report.Longitude,
		&report.UseRegionCode,
		&report.UseRegionName,
		&report.RegisteredRegionCode,
		&report.RegisteredRegionName,
		&report.RiskLevel,
		&report.ErrorCode,
		&report.ErrorSummary,
		&report.IsBackfilled,
		&rawJSON,
		&coverageJSON,
		&diagnosticsJSON,
		&report.CreatedAt,
		&productJSON,
	); err != nil {
		return ipquality.Report{}, err
	}
	if len(rawJSON) > 0 {
		report.RawJSON = json.RawMessage(append([]byte(nil), rawJSON...))
	}
	report.Coverage = coverageFromJSON(coverageJSON)
	if len(diagnosticsJSON) > 0 {
		report.DiagnosticsJSON = json.RawMessage(append([]byte(nil), diagnosticsJSON...))
	}
	product, err := decodeIPQualityAgentProduct(productJSON)
	if err != nil {
		return ipquality.Report{}, err
	}
	report.Product = product
	return report, nil
}

func scanIPQualitySummary(row ipQualityScanner) (ipquality.Summary, error) {
	var summary ipquality.Summary
	var coverageJSON []byte
	if err := row.Scan(
		&summary.VPSID,
		&summary.ReportID,
		&summary.ObservedAt,
		&summary.IPAddress,
		&summary.IPVersion,
		&summary.Status,
		&summary.RiskLevel,
		&summary.UseRegionCode,
		&summary.UseRegionName,
		&summary.ASN,
		&summary.Organization,
		&summary.Stale,
		&summary.Ambiguous,
		&summary.AssignmentMode,
		&summary.ErrorCode,
		&summary.ErrorSummary,
		&summary.ProviderCount,
		&summary.UnlockableCount,
		&coverageJSON,
	); err != nil {
		return ipquality.Summary{}, err
	}
	summary.Coverage = coverageFromJSON(coverageJSON)
	return summary, nil
}

func coverageFromJSON(raw []byte) *ipquality.Coverage {
	if len(raw) == 0 {
		return nil
	}
	var coverage ipquality.Coverage
	if err := json.Unmarshal(raw, &coverage); err != nil {
		return nil
	}
	return &coverage
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
