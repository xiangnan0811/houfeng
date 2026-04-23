package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/nodes"
)

type PostgresNodeRepository struct {
	db *pgxpool.Pool
}

func NewPostgresNodeRepository(db *pgxpool.Pool) *PostgresNodeRepository {
	return &PostgresNodeRepository{db: db}
}

const nodeSelectColumns = `
	node_id,
	display_name,
	region,
	city,
	provider,
	lifecycle_status,
	monitoring_status,
	binding_status,
	labels,
	note,
	current_health_status,
	last_heartbeat_at,
	last_sync_at,
	current_active_incident_count,
	current_primary_issue_summary,
	created_at,
	updated_at`

type nodeScanner interface {
	Scan(dest ...any) error
}

var _ nodes.Repository = (*PostgresNodeRepository)(nil)

func scanNode(row nodeScanner) (nodes.Record, error) {
	var record nodes.Record
	if err := row.Scan(
		&record.NodeID,
		&record.DisplayName,
		&record.Region,
		&record.City,
		&record.Provider,
		&record.LifecycleStatus,
		&record.MonitoringStatus,
		&record.BindingStatus,
		&record.Labels,
		&record.Note,
		&record.CurrentHealthStatus,
		&record.LastHeartbeatAt,
		&record.LastSyncAt,
		&record.CurrentActiveIncidentCount,
		&record.CurrentPrimaryIssueSummary,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nodes.Record{}, err
	}
	return record, nil
}

func (r *PostgresNodeRepository) ListNodes(ctx context.Context) ([]nodes.Record, error) {
	rows, err := r.db.Query(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		order by created_at desc`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	records := make([]nodes.Record, 0)
	for rows.Next() {
		record, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}

	return records, nil
}

func (r *PostgresNodeRepository) GetNode(ctx context.Context, nodeID string) (nodes.Record, error) {
	record, err := scanNode(r.db.QueryRow(ctx, `
		select `+nodeSelectColumns+`
		from nodes
		where node_id = $1`, nodeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nodes.Record{}, nodes.ErrNodeNotFound
	}
	if err != nil {
		return nodes.Record{}, fmt.Errorf("query node %q: %w", nodeID, err)
	}
	return record, nil
}

func (r *PostgresNodeRepository) CreateNode(ctx context.Context, input nodes.CreateInput) (nodes.Record, error) {
	nodeID, err := ids.New("nd")
	if err != nil {
		return nodes.Record{}, fmt.Errorf("generate node id: %w", err)
	}

	record, err := scanNode(r.db.QueryRow(ctx, `
		insert into nodes (
			node_id,
			display_name,
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			binding_status,
			labels,
			note,
			current_health_status,
			current_active_incident_count,
			current_primary_issue_summary
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			0,
			''
		)
		returning `+nodeSelectColumns,
		nodeID,
		input.DisplayName,
		input.Region,
		input.City,
		input.Provider,
		input.LifecycleStatus,
		nodes.MonitoringEnabled,
		nodes.BindingUnbound,
		input.Labels,
		input.Note,
		nodes.HealthNormal,
	))
	if err != nil {
		return nodes.Record{}, fmt.Errorf("create node: %w", err)
	}
	return record, nil
}
