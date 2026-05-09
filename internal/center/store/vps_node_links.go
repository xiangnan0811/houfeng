package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/ids"
)

var _ assetlinks.Repository = (*PostgresVPSNodeLinkRepository)(nil)

type vpsNodeLinkDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresVPSNodeLinkRepository struct {
	db vpsNodeLinkDB
}

func NewPostgresVPSNodeLinkRepository(db *pgxpool.Pool) *PostgresVPSNodeLinkRepository {
	return &PostgresVPSNodeLinkRepository{db: db}
}

const vpsNodeLinkSelectColumns = `
	link_id,
	vps_id,
	node_id,
	linked_at,
	unlinked_at,
	note`

type vpsNodeLinkScanner interface {
	Scan(dest ...any) error
}

func scanVPSNodeLink(row vpsNodeLinkScanner) (assetlinks.Record, error) {
	var record assetlinks.Record
	if err := row.Scan(
		&record.LinkID,
		&record.VPSID,
		&record.NodeID,
		&record.LinkedAt,
		&record.UnlinkedAt,
		&record.Note,
	); err != nil {
		return assetlinks.Record{}, err
	}
	return record, nil
}

func (r *PostgresVPSNodeLinkRepository) LinkNode(ctx context.Context, vpsID string, input assetlinks.LinkInput) (assetlinks.Record, error) {
	input = assetlinks.NormalizeLinkInput(input)
	if err := assetlinks.ValidateLinkInput(input); err != nil {
		return assetlinks.Record{}, err
	}

	linkID, err := ids.New("vnl")
	if err != nil {
		return assetlinks.Record{}, fmt.Errorf("generate vps node link id: %w", err)
	}

	record, err := scanVPSNodeLink(r.db.QueryRow(ctx, `
		insert into vps_node_links (
			link_id,
			vps_id,
			node_id,
			note
		) values (
			$1,
			$2,
			$3,
			$4
		)
		returning `+vpsNodeLinkSelectColumns,
		linkID,
		vpsID,
		input.NodeID,
		input.Note,
	))
	if err != nil {
		return assetlinks.Record{}, mapVPSNodeLinkWriteError(err, "link vps %q to node %q", vpsID, input.NodeID)
	}
	return record, nil
}

func (r *PostgresVPSNodeLinkRepository) UnlinkNode(ctx context.Context, vpsID string, input assetlinks.UnlinkInput) (assetlinks.Record, error) {
	input = assetlinks.NormalizeUnlinkInput(input)
	if err := assetlinks.ValidateUnlinkInput(input); err != nil {
		return assetlinks.Record{}, err
	}

	record, err := scanVPSNodeLink(r.db.QueryRow(ctx, `
		update vps_node_links
		set unlinked_at = now(),
		    note = case when $3 <> '' then $3 else note end
		where vps_id = $1
		  and node_id = $2
		  and unlinked_at is null
		returning `+vpsNodeLinkSelectColumns,
		vpsID,
		input.NodeID,
		input.Note,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlinks.Record{}, assetlinks.ErrVPSNodeLinkNotFound
	}
	if err != nil {
		return assetlinks.Record{}, fmt.Errorf("unlink vps %q from node %q: %w", vpsID, input.NodeID, err)
	}
	return record, nil
}

func (r *PostgresVPSNodeLinkRepository) ListNodesForVPS(ctx context.Context, vpsID string) ([]assetlinks.NodeSummary, error) {
	rows, err := r.db.Query(ctx, `
		select
			n.node_id,
			n.display_name,
			n."group",
			n.region,
			n.city,
			n.provider,
			n.lifecycle_status,
			n.monitoring_status,
			n.binding_status,
			n.current_health_status,
			n.last_heartbeat_at,
			n.last_sync_at,
			n.current_active_incident_count,
			n.current_primary_issue_summary,
			l.linked_at,
			l.note
		from vps_node_links l
		join nodes n on n.node_id = l.node_id
		where l.vps_id = $1
		  and l.unlinked_at is null
		order by l.linked_at desc, n.display_name, n.node_id`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query active nodes for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	summaries := make([]assetlinks.NodeSummary, 0)
	for rows.Next() {
		var summary assetlinks.NodeSummary
		if err := rows.Scan(
			&summary.NodeID,
			&summary.DisplayName,
			&summary.Group,
			&summary.Region,
			&summary.City,
			&summary.Provider,
			&summary.LifecycleStatus,
			&summary.MonitoringStatus,
			&summary.BindingStatus,
			&summary.CurrentHealthStatus,
			&summary.LastHeartbeatAt,
			&summary.LastSyncAt,
			&summary.CurrentActiveIncidentCount,
			&summary.CurrentPrimaryIssueSummary,
			&summary.LinkedAt,
			&summary.Note,
		); err != nil {
			return nil, fmt.Errorf("scan active node for vps %q: %w", vpsID, err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active nodes for vps %q: %w", vpsID, err)
	}
	return summaries, nil
}

func (r *PostgresVPSNodeLinkRepository) ListVPSForNode(ctx context.Context, nodeID string) ([]assetlinks.VPSSummary, error) {
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
		from vps_node_links l
		join vps_assets v on v.vps_id = l.vps_id
		where l.node_id = $1
		  and l.unlinked_at is null
		order by l.linked_at desc, lower(v.display_name), v.vps_id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query active vps assets for node %q: %w", nodeID, err)
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
			return nil, fmt.Errorf("scan active vps asset for node %q: %w", nodeID, err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active vps assets for node %q: %w", nodeID, err)
	}
	return summaries, nil
}

func (r *PostgresVPSNodeLinkRepository) CountActiveLinksForVPS(ctx context.Context, vpsID string) (int, error) {
	var count int
	if err := r.db.QueryRow(ctx, `
		select count(*)
		from vps_node_links
		where vps_id = $1
		  and unlinked_at is null`, vpsID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active links for vps %q: %w", vpsID, err)
	}
	return count, nil
}

func mapVPSNodeLinkWriteError(err error, format string, args ...any) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return assetlinks.ErrVPSNodeLinkConflict
		case "23503":
			return assetlinks.ErrVPSNodeLinkNotFound
		case "23514":
			return assetlinks.ErrInvalidVPSNodeLinkInput
		}
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
