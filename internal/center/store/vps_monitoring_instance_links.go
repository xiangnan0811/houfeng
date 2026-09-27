package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
)

var _ assetlinks.Repository = (*PostgresVPSMonitoringInstanceLinkRepository)(nil)

type vpsMonitoringInstanceLinkDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type PostgresVPSMonitoringInstanceLinkRepository struct {
	db vpsMonitoringInstanceLinkDB
}

func NewPostgresVPSMonitoringInstanceLinkRepository(db *pgxpool.Pool) *PostgresVPSMonitoringInstanceLinkRepository {
	return &PostgresVPSMonitoringInstanceLinkRepository{db: db}
}

const vpsMonitoringInstanceLinkSelectColumns = `
	link_id,
	vps_id,
	monitoring_instance_id,
	linked_at,
	unlinked_at,
	note`

type vpsMonitoringInstanceLinkScanner interface {
	Scan(dest ...any) error
}

func scanVPSMonitoringInstanceLink(row vpsMonitoringInstanceLinkScanner) (assetlinks.Record, error) {
	var record assetlinks.Record
	if err := row.Scan(
		&record.LinkID,
		&record.VPSID,
		&record.MonitoringInstanceID,
		&record.LinkedAt,
		&record.UnlinkedAt,
		&record.Note,
	); err != nil {
		return assetlinks.Record{}, err
	}
	return record, nil
}

func lockVPSAndRejectActiveMonitoringLink(ctx context.Context, tx pgx.Tx, vpsID, monitoringInstanceID string) error {
	lifecycle, err := lockAssetVPSLifecycle(ctx, tx, vpsID)
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlinks.ErrVPSMonitoringInstanceLinkNotFound
	}
	if err != nil {
		return fmt.Errorf("lock vps %q before monitoring instance link write: %w", vpsID, err)
	}
	if isTerminalVPSLifecycle(lifecycle) {
		return fmt.Errorf("%w: terminal vps %q cannot accept a monitoring instance link", assetlinks.ErrVPSMonitoringInstanceLinkConflict, vpsID)
	}

	miLifecycle, archivedAt, err := lockMonitoringInstanceForAssetLink(ctx, tx, monitoringInstanceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlinks.ErrVPSMonitoringInstanceLinkNotFound
	}
	if err != nil {
		return fmt.Errorf("lock monitoring instance %q before vps link write: %w", monitoringInstanceID, err)
	}
	if err := ensureMonitoringInstanceCanBeLinked(monitoringInstanceID, miLifecycle, archivedAt); err != nil {
		return err
	}
	return rejectActiveMonitoringLink(ctx, tx, vpsID)
}

func rejectActiveMonitoringLink(ctx context.Context, tx pgx.Tx, vpsID string) error {
	var activeLinkCount int
	if err := tx.QueryRow(ctx, `
		select count(*)
		from monitoring_instances
		where vps_id = $1
		  and lifecycle_status <> '已退役'`,
		vpsID,
	).Scan(&activeLinkCount); err != nil {
		return fmt.Errorf("count active monitoring instance links for vps %q: %w", vpsID, err)
	}
	if activeLinkCount > 0 {
		return assetlinks.ErrVPSActiveMonitoringInstanceExists
	}
	return nil
}

func (r *PostgresVPSMonitoringInstanceLinkRepository) LinkMonitoringInstance(ctx context.Context, vpsID string, input assetlinks.LinkInput) (assetlinks.Record, error) {
	return assetlinks.Record{}, fmt.Errorf("%w: monitoring ownership is immutable", assetlinks.ErrVPSMonitoringInstanceLinkConflict)
}
func (r *PostgresVPSMonitoringInstanceLinkRepository) UnlinkMonitoringInstance(ctx context.Context, vpsID string, input assetlinks.UnlinkInput) (assetlinks.Record, error) {
	return assetlinks.Record{}, fmt.Errorf("%w: retire monitoring instead of unlinking", assetlinks.ErrVPSMonitoringInstanceLinkConflict)
}

func (r *PostgresVPSMonitoringInstanceLinkRepository) ListMonitoringInstancesForVPS(ctx context.Context, vpsID string) ([]assetlinks.MonitoringInstanceSummary, error) {
	rows, err := r.db.Query(ctx, `
		select
			n.monitoring_instance_id,
			n.display_name,
			n."group",
			n.region,
			n.city,
			n.provider,
			n.lifecycle_status,
			n.archived_at,
			n.monitoring_status,
			n.binding_status,
			n.current_health_status,
			n.last_heartbeat_at,
			n.last_sync_at,
			n.current_active_incident_count,
			n.current_primary_issue_summary,
			l.linked_at,
			l.note, n.vps_id, v.lifecycle_status, n.lifecycle_status <> '已退役',n.ever_connected,n.last_trusted_online_at
		from vps_monitoring_instance_links l
		join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
		join vps_assets v on v.vps_id=n.vps_id
		where n.vps_id = $1
		order by l.linked_at desc, n.display_name, n.monitoring_instance_id`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query active monitoring instances for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	summaries := make([]assetlinks.MonitoringInstanceSummary, 0)
	for rows.Next() {
		var summary assetlinks.MonitoringInstanceSummary
		if err := rows.Scan(
			&summary.MonitoringInstanceID,
			&summary.DisplayName,
			&summary.Group,
			&summary.Region,
			&summary.City,
			&summary.Provider,
			&summary.LifecycleStatus,
			&summary.ArchivedAt,
			&summary.MonitoringStatus,
			&summary.BindingStatus,
			&summary.CurrentHealthStatus,
			&summary.LastHeartbeatAt,
			&summary.LastSyncAt,
			&summary.CurrentActiveIncidentCount,
			&summary.CurrentPrimaryIssueSummary,
			&summary.LinkedAt,
			&summary.Note, &summary.VPSID, &summary.VPSLifecycleStatus, &summary.IsCurrent, &summary.EverConnected, &summary.LastTrustedOnlineAt,
		); err != nil {
			return nil, fmt.Errorf("scan active monitoring instance for vps %q: %w", vpsID, err)
		}
		projection := monitoringinstances.Record{VPSLifecycleStatus: summary.VPSLifecycleStatus, LifecycleStatus: summary.LifecycleStatus, MonitoringStatus: summary.MonitoringStatus, BindingStatus: summary.BindingStatus, LastTrustedOnlineAt: summary.LastTrustedOnlineAt, CurrentHealthStatus: summary.CurrentHealthStatus}
		projectMonitoringHealth(&projection)
		summary.CurrentHealthStatus = projection.CurrentHealthStatus
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active monitoring instances for vps %q: %w", vpsID, err)
	}
	return summaries, nil
}

func (r *PostgresVPSMonitoringInstanceLinkRepository) ListVPSForMonitoringInstance(ctx context.Context, monitoringInstanceID string) ([]assetlinks.VPSSummary, error) {
	rows, err := r.db.Query(ctx, `
		select
			v.vps_id,
			v.display_name,
			v.provider_id,
			v.provider_name,
			v.country,
			v.region,
			v.city,
			v.lifecycle_status,
			v.usage_status,
			v.renewal_decision,
			v.importance,
			v.labels,
			v.archived_at,
			l.linked_at,
			l.note
		from vps_monitoring_instance_links l
		join vps_assets v on v.vps_id = l.vps_id
		where l.monitoring_instance_id = $1
		order by l.linked_at desc, lower(v.display_name), v.vps_id`, monitoringInstanceID)
	if err != nil {
		return nil, fmt.Errorf("query active vps assets for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	defer rows.Close()

	summaries := make([]assetlinks.VPSSummary, 0)
	for rows.Next() {
		var summary assetlinks.VPSSummary
		if err := rows.Scan(
			&summary.VPSID,
			&summary.DisplayName,
			&summary.ProviderID,
			&summary.ProviderName,
			&summary.Country,
			&summary.Region,
			&summary.City,
			&summary.LifecycleStatus,
			&summary.UsageStatus,
			&summary.RenewalDecision,
			&summary.Importance,
			&summary.Labels,
			&summary.ArchivedAt,
			&summary.LinkedAt,
			&summary.Note,
		); err != nil {
			return nil, fmt.Errorf("scan active vps asset for monitoring instance %q: %w", monitoringInstanceID, err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active vps assets for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	return summaries, nil
}

func (r *PostgresVPSMonitoringInstanceLinkRepository) CountActiveLinksForVPS(ctx context.Context, vpsID string) (int, error) {
	var count int
	if err := r.db.QueryRow(ctx, `
		select count(*)
		from monitoring_instances
		where vps_id = $1
		  and lifecycle_status <> '已退役'`, vpsID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active links for vps %q: %w", vpsID, err)
	}
	return count, nil
}

func mapVPSMonitoringInstanceLinkWriteError(err error, format string, args ...any) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return assetlinks.ErrVPSMonitoringInstanceLinkConflict
		case "23503":
			return assetlinks.ErrVPSMonitoringInstanceLinkNotFound
		case "23514":
			return assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput
		}
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
