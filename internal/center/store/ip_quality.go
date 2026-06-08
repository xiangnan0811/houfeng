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
				raw_json
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb)`,
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
					error_summary
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
				resultID,
				reportID,
				provider.Provider,
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
					status,
					region,
					unlock_type,
					error_code,
					error_summary
				) values ($1,$2,$3,$4,$5,$6,$7,$8)`,
				unlockID,
				reportID,
				unlock.Service,
				unlock.Status,
				unlock.Region,
				unlock.UnlockType,
				unlock.ErrorCode,
				unlock.ErrorSummary,
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

func (r *PostgresIPQualityRepository) ListLatestSummariesForVPS(ctx context.Context, vpsIDs []string) (map[string]ipquality.Summary, error) {
	out := make(map[string]ipquality.Summary, len(vpsIDs))
	if len(vpsIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, `
		select latest.vps_id,
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
			latest.unlockable_count
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
			r.created_at
		from ip_quality_reports r
		join ip_quality_latest_vps_summaries latest on latest.report_id = r.report_id
		where latest.vps_id = $1
		order by r.observed_at desc, r.report_id desc
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

func (r *PostgresIPQualityRepository) providerResultsForReport(ctx context.Context, reportID string) ([]ipquality.ProviderResultRead, error) {
	rows, err := r.db.Query(ctx, `
		select provider,
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
			error_summary
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
		if err := rows.Scan(
			&result.Provider,
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
		); err != nil {
			return nil, err
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
			status,
			region,
			unlock_type,
			error_code,
			error_summary
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
		if err := rows.Scan(
			&result.Service,
			&result.Status,
			&result.Region,
			&result.UnlockType,
			&result.ErrorCode,
			&result.ErrorSummary,
		); err != nil {
			return nil, err
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
			assigned.unlockable_count
		from ip_quality_assigned_vps_reports assigned
		where assigned.vps_id = $1
		order by assigned.observed_at desc, assigned.report_id desc
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
		&report.CreatedAt,
	); err != nil {
		return ipquality.Report{}, err
	}
	if len(rawJSON) > 0 {
		report.RawJSON = json.RawMessage(append([]byte(nil), rawJSON...))
	}
	return report, nil
}

func scanIPQualitySummary(row ipQualityScanner) (ipquality.Summary, error) {
	var summary ipquality.Summary
	if err := row.Scan(
		&summary.VPSID,
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
	); err != nil {
		return ipquality.Summary{}, err
	}
	return summary, nil
}
