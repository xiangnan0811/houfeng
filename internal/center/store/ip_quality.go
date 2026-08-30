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
)

type ipQualityDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type ipQualityTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresIPQualityRepository struct {
	db          ipQualityDB
	beginTx     func(context.Context, pgx.TxOptions) (ipQualityTx, error)
	newReportID func() (string, error)
}

func NewPostgresIPQualityRepository(db *pgxpool.Pool) *PostgresIPQualityRepository {
	return &PostgresIPQualityRepository{
		db: db,
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
	for _, report := range reports {
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
		rawJSON := []byte(nil)
		if len(report.RawJSON) > 0 {
			rawJSON = ipquality.SanitizeRawJSON(report.RawJSON)
		}
		coverageJSON := []byte(nil)
		if len(report.CoverageJSON) > 0 {
			coverageJSON = ipquality.SanitizeExtraJSON(report.CoverageJSON)
		}
		diagnosticsJSON := []byte(nil)
		if len(report.DiagnosticsJSON) > 0 {
			diagnosticsJSON = ipquality.SanitizeExtraJSON(report.DiagnosticsJSON)
		}
		receivedAt := report.ReceivedAt
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}
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
				diagnostics_json
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb,$24::jsonb,$25::jsonb)`,
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
		); err != nil {
			return fmt.Errorf("insert ip quality report for monitoring instance %q: %w", report.MonitoringInstanceID, err)
		}
		for _, provider := range report.ProviderResults {
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
					extra_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb)`,
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
			); err != nil {
				return fmt.Errorf("insert ip quality provider result for report %q: %w", reportID, err)
			}
		}
		for _, unlock := range report.ServiceUnlocks {
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
					extra_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`,
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
			); err != nil {
				return fmt.Errorf("insert ip quality service unlock for report %q: %w", reportID, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit ip quality reports transaction: %w", err)
	}
	return nil
}

const overviewLatestIPQualitySummarySQL = `
		-- vps overview ip quality summary
		with valid_reports as (
			select r.report_id, r.observed_at, r.received_at, r.ip_address, r.status, r.risk_level, r.monitoring_instance_id, r.is_backfilled
			from ip_quality_reports r
			where r.status in ('success', 'partial')
				and r.ip_address <> '0.0.0.0'
				and r.ip_version in (4, 6)
		),
		ip_quality_stale_settings as (
			select
				case
					when coalesce(ip_quality_settings, '{}'::jsonb) ? 'stale_after_seconds'
						and coalesce(ip_quality_settings->>'stale_after_seconds', '') ~ '^[0-9]+$'
					then greatest((ip_quality_settings->>'stale_after_seconds')::integer, 60)
					else 604800
				end as stale_after_seconds
			from center_settings
			where settings_id = 'center'
			union all
			select 604800
			where not exists (
				select 1 from center_settings where settings_id = 'center'
			)
		),
		assigned as (
			select
				l.vps_id,
				r.report_id,
				r.observed_at,
				r.received_at,
				r.status,
				r.risk_level,
				r.is_backfilled
			from vps_monitoring_instance_links l
			join valid_reports r on r.monitoring_instance_id = l.monitoring_instance_id
			where l.unlinked_at is null
				and l.vps_id = $1
			union all
			select
				v.vps_id,
				r.report_id,
				r.observed_at,
				r.received_at,
				r.status,
				r.risk_level,
				r.is_backfilled
			from vps_assets v
			join valid_reports r on r.ip_address in (nullif(v.ipv4, ''), nullif(v.ipv6, ''))
			where v.vps_id = $1
				and not exists (
					select 1
					from vps_monitoring_instance_links l
					where l.vps_id = v.vps_id
						and l.unlinked_at is null
				)
		)
		select
			assigned.vps_id,
			assigned.status,
			assigned.risk_level,
			assigned.observed_at < now() - make_interval(secs => (
				select stale_after_seconds
				from ip_quality_stale_settings
				limit 1
			)) as stale,
			assigned.observed_at
		from assigned
		order by assigned.observed_at desc, assigned.is_backfilled asc, assigned.received_at desc, assigned.report_id desc
		limit 1`

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
	if err := rows.Scan(
		&summary.VPSID,
		&summary.Status,
		&summary.RiskLevel,
		&summary.Stale,
		&summary.ObservedAt,
	); err != nil {
		return nil, fmt.Errorf("scan overview ip quality summary: %w", err)
	}
	return &summary, nil
}

func (r *PostgresIPQualityRepository) GetVPSIPQuality(ctx context.Context, vpsID string) (ipquality.VPSReport, error) {
	summaries, err := r.ListLatestSummariesForVPS(ctx, []string{vpsID})
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	report, ok, err := r.latestReportForVPS(ctx, vpsID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		return ipquality.VPSReport{
			ProviderResults: []ipquality.ProviderResultRead{},
			ServiceUnlocks:  []ipquality.ServiceUnlockRead{},
			History:         []ipquality.Summary{},
		}, nil
	}
	providers, err := r.providerResultsForReport(ctx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	unlocks, err := r.serviceUnlocksForReport(ctx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	history, err := r.historyForVPS(ctx, vpsID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	var summary *ipquality.Summary
	if value, ok := summaries[vpsID]; ok {
		s := value
		summary = &s
	}
	return ipquality.VPSReport{
		Summary:         summary,
		LatestReport:    &report,
		ProviderResults: providers,
		ServiceUnlocks:  unlocks,
		History:         history,
	}, nil
}

func (r *PostgresIPQualityRepository) GetVPSIPQualityReportDetail(ctx context.Context, vpsID, reportID string) (ipquality.VPSReport, error) {
	report, ok, err := r.reportForAssignedVPS(ctx, vpsID, reportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		return ipquality.VPSReport{
			ProviderResults: []ipquality.ProviderResultRead{},
			ServiceUnlocks:  []ipquality.ServiceUnlockRead{},
			History:         []ipquality.Summary{},
		}, nil
	}
	summary, ok, err := r.summaryForAssignedVPSReport(ctx, vpsID, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	if !ok {
		summary = summaryFromReport(vpsID, report)
	}
	providers, err := r.providerResultsForReport(ctx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	unlocks, err := r.serviceUnlocksForReport(ctx, report.ReportID)
	if err != nil {
		return ipquality.VPSReport{}, err
	}
	return ipquality.VPSReport{
		Summary:         &summary,
		LatestReport:    &report,
		ProviderResults: providers,
		ServiceUnlocks:  unlocks,
		History:         []ipquality.Summary{},
	}, nil
}

func (r *PostgresIPQualityRepository) ListLatestSummariesForVPS(ctx context.Context, vpsIDs []string) (map[string]ipquality.Summary, error) {
	out := make(map[string]ipquality.Summary, len(vpsIDs))
	if len(vpsIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		with ranked as (
			select assigned.*,
				row_number() over (
					partition by assigned.vps_id
					order by assigned.observed_at desc, r.is_backfilled asc, r.received_at desc, assigned.report_id desc
				) as latest_rank
			from ip_quality_assigned_vps_reports assigned
			join ip_quality_reports r on r.report_id = assigned.report_id
			where assigned.vps_id = any($1)
		)
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
		from ranked latest
		where latest.latest_rank = 1`, vpsIDs)
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
	return out, nil
}

func (r *PostgresIPQualityRepository) latestReportForVPS(ctx context.Context, vpsID string) (ipquality.Report, bool, error) {
	rows, err := r.db.Query(ctx, `
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
			r.created_at
		from ip_quality_reports r
		join ip_quality_assigned_vps_reports assigned on assigned.report_id = r.report_id
		where assigned.vps_id = $1
		order by r.observed_at desc, r.is_backfilled asc, r.received_at desc, r.report_id desc
		limit 1`, vpsID)
	if err != nil {
		return ipquality.Report{}, false, fmt.Errorf("query latest ip quality report for vps %q: %w", vpsID, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return ipquality.Report{}, false, rows.Err()
	}
	report, err := scanIPQualityReport(rows)
	if err != nil {
		return ipquality.Report{}, false, fmt.Errorf("scan latest ip quality report for vps %q: %w", vpsID, err)
	}
	if rows.Next() {
		return report, true, nil
	}
	if err := rows.Err(); err != nil {
		return ipquality.Report{}, false, fmt.Errorf("iterate latest ip quality report for vps %q: %w", vpsID, err)
	}
	return report, true, nil
}

func (r *PostgresIPQualityRepository) reportForAssignedVPS(ctx context.Context, vpsID, reportID string) (ipquality.Report, bool, error) {
	rows, err := r.db.Query(ctx, `
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
			r.created_at
		from ip_quality_reports r
		join ip_quality_assigned_vps_reports assigned on assigned.report_id = r.report_id
		where assigned.vps_id = $1 and r.report_id = $2
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

func (r *PostgresIPQualityRepository) summaryForAssignedVPSReport(ctx context.Context, vpsID, reportID string) (ipquality.Summary, bool, error) {
	rows, err := r.db.Query(ctx, `
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

func (r *PostgresIPQualityRepository) providerResultsForReport(ctx context.Context, reportID string) ([]ipquality.ProviderResultRead, error) {
	rows, err := r.db.Query(ctx, `
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
			extra_json
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
		var rawExtra []byte
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
		); err != nil {
			return nil, err
		}
		if len(rawExtra) > 0 {
			result.ExtraJSON = json.RawMessage(append([]byte(nil), rawExtra...))
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality provider results for report %q: %w", reportID, err)
	}
	return results, nil
}

func (r *PostgresIPQualityRepository) serviceUnlocksForReport(ctx context.Context, reportID string) ([]ipquality.ServiceUnlockRead, error) {
	rows, err := r.db.Query(ctx, `
		select service,
			source,
			status,
			probe_status,
			latency_ms,
			region,
			unlock_type,
			error_code,
			error_summary,
			extra_json
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
		var rawExtra []byte
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
		); err != nil {
			return nil, err
		}
		if len(rawExtra) > 0 {
			result.ExtraJSON = json.RawMessage(append([]byte(nil), rawExtra...))
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip quality service unlocks for report %q: %w", reportID, err)
	}
	return results, nil
}

func (r *PostgresIPQualityRepository) historyForVPS(ctx context.Context, vpsID string) ([]ipquality.Summary, error) {
	rows, err := r.db.Query(ctx, `
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
		where assigned.vps_id = $1
		order by assigned.observed_at desc, r.is_backfilled asc, r.received_at desc, assigned.report_id desc
		limit 30`, vpsID)
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

func summaryFromReport(vpsID string, report ipquality.Report) ipquality.Summary {
	return ipquality.Summary{
		ReportID:      report.ReportID,
		VPSID:         vpsID,
		ObservedAt:    report.ObservedAt,
		IPAddress:     report.IPAddress,
		IPVersion:     report.IPVersion,
		Status:        report.Status,
		RiskLevel:     report.RiskLevel,
		UseRegionCode: report.UseRegionCode,
		UseRegionName: report.UseRegionName,
		ASN:           report.ASN,
		Organization:  report.Organization,
		ErrorCode:     report.ErrorCode,
		ErrorSummary:  report.ErrorSummary,
		Coverage:      report.Coverage,
	}
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
